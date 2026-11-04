// Der Server: liest die Konfiguration aus Umgebungsvariablen, verbindet sich
// mit Firestore und beantwortet Connect-Anfragen. Auf Cloud Run setzt OpenTofu
// die Variablen (infra/server.tf).
//
// Lokal mit Firestore-Emulator und fake-gcs-server (darin vorher den Bucket
// "lokal" anlegen, siehe docs/entwicklungsumgebung.md):
//
//	FIRESTORE_EMULATOR_HOST=localhost:8086 STORAGE_EMULATOR_HOST=localhost:4443 \
//	PROJEKT=lokal SICHERUNG_BUCKET=lokal DIAGNOSE_BUCKET=lokal \
//	GOOGLE_CLIENT_IDS=<client-id> PFEFFER=<base64, ≥ 32 Bytes> go run ./cmd/server
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/anmeldung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/diagnose"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/dienst"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// kontoIDMuster beschreibt eine Konto-ID: 128 Bit Zufall als Hex in
// Kleinbuchstaben (wie in infra/diagnose.tf geprüft).
var kontoIDMuster = regexp.MustCompile(`^[0-9a-f]{32}$`)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: cloudLoggingFelder})))
	if err := starte(); err != nil {
		slog.Error("Server beendet mit Fehler", "fehler", err.Error())
		os.Exit(1)
	}
}

// cloudLoggingFelder benennt die slog-Felder so um, wie Cloud Logging sie
// erwartet ("severity", "message"); wie im Kill-Switch.
func cloudLoggingFelder(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// konfiguration aus den Umgebungsvariablen.
type konfiguration struct {
	projekt         string   // PROJEKT
	sicherungBucket string   // SICHERUNG_BUCKET: Bucket für die Datensicherungen
	diagnoseBucket  string   // DIAGNOSE_BUCKET: Bucket für Diagnosen aus der App
	diagnoseKonten  []string // DIAGNOSE_KONTEN: freigeschaltete Konto-IDs, durch Komma getrennt; leer = niemand
	clientIDs       []string // GOOGLE_CLIENT_IDS, durch Komma getrennt
	pfefferSecret   string   // PFEFFER_SECRET: Secret-Manager-Version (Betrieb)
	pfeffer         []byte   // PFEFFER: base64 (nur lokal)
	port            string   // PORT, setzt Cloud Run; sonst 8080
}

// leseKonfiguration ist von os.Getenv getrennt, damit sie testbar ist.
func leseKonfiguration(getenv func(string) string) (konfiguration, error) {
	k := konfiguration{
		projekt:         getenv("PROJEKT"),
		sicherungBucket: getenv("SICHERUNG_BUCKET"),
		diagnoseBucket:  getenv("DIAGNOSE_BUCKET"),
		pfefferSecret:   getenv("PFEFFER_SECRET"),
		port:            getenv("PORT"),
	}
	if k.port == "" {
		k.port = "8080"
	}
	if k.projekt == "" {
		return k, errors.New("PROJEKT fehlt")
	}
	if k.sicherungBucket == "" {
		return k, errors.New("SICHERUNG_BUCKET fehlt")
	}
	if k.diagnoseBucket == "" {
		return k, errors.New("DIAGNOSE_BUCKET fehlt")
	}
	// DIAGNOSE_KONTEN darf leer sein: Dann nimmt der Server keine Diagnosen an.
	k.diagnoseKonten = liste(getenv("DIAGNOSE_KONTEN"))
	// Ein Tippfehler (Großbuchstaben, ein Zeichen zu wenig) würde sonst still
	// niemanden freischalten; lieber startet der Server nicht.
	for _, id := range k.diagnoseKonten {
		if !kontoIDMuster.MatchString(id) {
			return k, fmt.Errorf("DIAGNOSE_KONTEN: %q ist keine Konto-ID (32 Zeichen 0-9a-f)", id)
		}
	}
	k.clientIDs = liste(getenv("GOOGLE_CLIENT_IDS"))
	if len(k.clientIDs) == 0 {
		return k, errors.New("GOOGLE_CLIENT_IDS fehlt")
	}
	lokal := getenv("PFEFFER")
	switch {
	case k.pfefferSecret != "" && lokal != "":
		return k, errors.New("PFEFFER und PFEFFER_SECRET schließen sich aus")
	case k.pfefferSecret != "":
	case lokal != "":
		p, err := base64.StdEncoding.DecodeString(lokal)
		if err != nil {
			return k, fmt.Errorf("PFEFFER ist kein base64: %w", err)
		}
		k.pfeffer = p
	default:
		return k, errors.New("PFEFFER_SECRET (oder lokal PFEFFER) fehlt")
	}
	return k, nil
}

// liste zerlegt eine durch Komma getrennte Liste und lässt leere Einträge weg.
func liste(s string) []string {
	var l []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			l = append(l, e)
		}
	}
	return l
}

// erlaubtFuer liefert die Prüfung, ob ein Konto Diagnosen senden darf.
//
// KRITISCH: Nur genau die genannten Konten (E-D-3), keine Teilstrings.
func erlaubtFuer(konten []string) func(kontoID string) bool {
	menge := make(map[string]bool, len(konten))
	for _, k := range konten {
		menge[k] = true
	}
	return func(kontoID string) bool { return menge[kontoID] }
}

func starte() error {
	// ctx wird beim Signal SIGTERM (Cloud Run beendet die Instanz) oder Strg+C
	// abgebrochen.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	k, err := leseKonfiguration(os.Getenv)
	if err != nil {
		return err
	}
	if k.pfefferSecret != "" {
		if k.pfeffer, err = leseSecret(ctx, k.pfefferSecret); err != nil {
			return err
		}
	}

	fs, err := firestore.NewClient(ctx, k.projekt)
	if err != nil {
		return fmt.Errorf("Firestore: %w", err)
	}
	defer func() { _ = fs.Close() }() // Fehler beim Schließen am Ende ist egal

	gcs, err := datensicherung.NeuerClient(ctx)
	if err != nil {
		return fmt.Errorf("Cloud Storage: %w", err)
	}
	defer func() { _ = gcs.Close() }()

	// Die Schlüssel von Google werden bei Bedarf geladen und zwischengespeichert.
	// Eigener HTTP-Client mit Zeitlimit: Ein hängender Abruf würde sonst alle
	// folgenden Anmeldungen blockieren (http.DefaultClient wartet unbegrenzt).
	googleCtx := oidc.ClientContext(ctx, &http.Client{Timeout: 10 * time.Second})
	pruefer, err := anmeldung.NeuerPruefer(oidc.NewRemoteKeySet(googleCtx, anmeldung.GoogleSchluessel), k.clientIDs, nil)
	if err != nil {
		return err
	}
	handler, err := dienst.NeuerHandler(dienst.Konfiguration{
		Pruefer:     pruefer,
		Speicher:    speicher.NeuerFirestore(fs),
		Sicherungen: datensicherung.NeuesCloudStorage(gcs, k.sicherungBucket),
		Diagnosen:   diagnose.NeuesCloudStorage(gcs, k.diagnoseBucket),
		Kontingente: kontingent.NeuerFirestore(fs),
		Pfeffer:     k.pfeffer,
		// Freigeschaltet werden Konten, nicht Geräte (E-D-3).
		DiagnoseErlaubt: erlaubtFuer(k.diagnoseKonten),
	})
	if err != nil {
		return err
	}

	// HTTP/1.1 und HTTP/2 ohne TLS (h2c): Cloud Run beendet TLS vorher.
	protokolle := new(http.Protocols)
	protokolle.SetHTTP1(true)
	protokolle.SetUnencryptedHTTP2(true)
	srv := &http.Server{
		Addr:              ":" + k.port,
		Handler:           handler,
		Protocols:         protokolle,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	fehler := make(chan error, 1)
	go func() { fehler <- srv.ListenAndServe() }() // "go": läuft parallel weiter
	slog.Info("Server gestartet", "port", k.port, "projekt", k.projekt, "diagnoseKonten", len(k.diagnoseKonten))

	// select wartet auf das, was zuerst passiert: Fehler beim Start oder Signal.
	select {
	case err := <-fehler:
		return err
	case <-ctx.Done():
	}
	slog.Info("Server fährt herunter")
	ende, abbrechen := context.WithTimeout(context.Background(), 8*time.Second)
	defer abbrechen()
	return srv.Shutdown(ende)
}

// leseSecret holt den Pfeffer aus dem Secret Manager. name ist die volle
// Version, z. B. projects/<p>/secrets/pfeffer/versions/1 (fest, siehe infra/server.tf).
func leseSecret(ctx context.Context, name string) ([]byte, error) {
	c, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Secret Manager: %w", err)
	}
	defer func() { _ = c.Close() }()
	res, err := c.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{Name: name})
	if err != nil {
		return nil, fmt.Errorf("Pfeffer lesen: %w", err)
	}
	return res.GetPayload().GetData(), nil
}
