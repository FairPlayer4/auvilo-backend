package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

// Anmeldung bei Google wie in einer Desktop-App ("Loopback-Flow"):
//  1. Das Werkzeug lauscht auf 127.0.0.1 an einem freien Port.
//  2. Der Browser öffnet die Google-Anmeldung; danach leitet Google mit einem
//     Code auf 127.0.0.1 zurück.
//  3. Das Werkzeug tauscht den Code gegen Tokens und nimmt das ID-Token.
//
// PKCE bindet den Code an dieses Werkzeug; state schützt vor fremden
// Rückleitungen; nonce bindet das ID-Token an den Geräteschlüssel.

// googleEndpunkt: fest eingetragen statt golang.org/x/oauth2/google, das viele
// weitere Abhängigkeiten mitbringt.
var googleEndpunkt = oauth2.Endpoint{ //nolint:gosec // öffentliche Adressen, keine Zugangsdaten
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// findeClientDatei sucht die von Google heruntergeladene JSON-Datei des
// OAuth-Clients. Sie bleibt außerhalb des Repos.
func findeClientDatei(ordner string) (string, error) {
	treffer, err := filepath.Glob(filepath.Join(ordner, "client_secret_*.json"))
	if err != nil {
		return "", err
	}
	switch len(treffer) {
	case 0:
		return "", fmt.Errorf("keine client_secret_*.json in %s (siehe docs/gcp-einrichtung.md)", ordner)
	case 1:
		return treffer[0], nil
	default:
		return "", fmt.Errorf("mehrere client_secret_*.json in %s – bitte nur eine", ordner)
	}
}

// leseClient liest Client-ID und -Secret aus der JSON-Datei. Bei Desktop-Clients
// ist das "Secret" laut Google nicht geheim; trotzdem gibt das Werkzeug es nie aus.
func leseClient(daten []byte) (*oauth2.Config, error) {
	var datei struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(daten, &datei); err != nil {
		return nil, fmt.Errorf("Client-Datei: %w", err)
	}
	if datei.Installed.ClientID == "" || datei.Installed.ClientSecret == "" {
		return nil, errors.New(`Client-Datei: Abschnitt "installed" fehlt – ist der Client vom Typ Desktop-App?`)
	}
	return &oauth2.Config{
		ClientID:     datei.Installed.ClientID,
		ClientSecret: datei.Installed.ClientSecret,
		Endpoint:     googleEndpunkt,
		Scopes:       []string{"openid"}, // nur die Konto-ID, keine E-Mail, kein Name
	}, nil
}

// rueckruf ist das Ergebnis der Rückleitung von Google.
type rueckruf struct {
	code string
	err  error
}

// rueckrufHandler nimmt die Rückleitung entgegen und schickt das Ergebnis
// einmal in ergebnis.
//
// KRITISCH: Nur eine Rückleitung mit unserem state zählt. Sonst könnte eine
// fremde Webseite dem Werkzeug einen Code unterschieben.
func rueckrufHandler(state string, ergebnis chan<- rueckruf) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var e rueckruf
		switch {
		case q.Get("state") != state:
			http.Error(w, "state passt nicht", http.StatusBadRequest)
			return // zählt nicht als Ergebnis: weiter warten
		case q.Get("error") != "":
			e.err = fmt.Errorf("Google meldet: %s", q.Get("error"))
		case q.Get("code") == "":
			e.err = errors.New("Rückleitung ohne Code")
		default:
			e.code = q.Get("code")
		}
		if e.err != nil {
			http.Error(w, "Anmeldung fehlgeschlagen. Das Fenster kann geschlossen werden.", http.StatusBadRequest)
		} else {
			_, _ = fmt.Fprintln(w, "Anmeldung abgeschlossen. Das Fenster kann geschlossen werden.")
		}
		select {
		case ergebnis <- e: // nur das erste Ergebnis zählt
		default:
		}
	})
}

// googleIDToken führt den Loopback-Flow aus und liefert das ID-Token.
// oeffne öffnet die Adresse im Browser.
func googleIDToken(ctx context.Context, cfg *oauth2.Config, nonce string, oeffne func(string) error) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0") // Port 0: das System wählt einen freien
	if err != nil {
		return "", err
	}
	kopie := *cfg // cfg nicht verändern
	kopie.RedirectURL = "http://" + ln.Addr().String() + "/"

	state := zufall()
	verifier := oauth2.GenerateVerifier()
	adresse := kopie.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)

	ergebnis := make(chan rueckruf, 1)
	srv := &http.Server{Handler: rueckrufHandler(state, ergebnis), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	fmt.Println("Anmeldung im Browser. Falls er sich nicht öffnet, diese Adresse aufrufen:")
	fmt.Println(adresse)
	if err := oeffne(adresse); err != nil {
		fmt.Println("Browser ließ sich nicht öffnen:", err)
	}

	ctx, abbrechen := context.WithTimeout(ctx, 5*time.Minute)
	defer abbrechen()
	var e rueckruf
	select {
	case e = <-ergebnis:
	case <-ctx.Done():
		return "", errors.New("keine Rückleitung von Google innerhalb von 5 Minuten")
	}
	if e.err != nil {
		return "", e.err
	}

	tok, err := kopie.Exchange(ctx, e.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("Code eintauschen: %w", err)
	}
	idToken, _ := tok.Extra("id_token").(string)
	if idToken == "" {
		return "", errors.New("Google hat kein ID-Token geliefert")
	}
	return idToken, nil
}

func zufall() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // liefert seit Go 1.24 nie einen Fehler
	return base64.RawURLEncoding.EncodeToString(b)
}
