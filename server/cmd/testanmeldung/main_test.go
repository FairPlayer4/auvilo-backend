package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/anmeldung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/dienst"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

func TestPruefeName(t *testing.T) {
	tests := []struct {
		name    string
		geraet  string
		wantErr bool
	}{
		{"nimmt einfachen Namen an", "pc", false},
		{"nimmt Ziffern und Bindestrich an", "handy-2", false},
		{"lehnt leeren Namen ab", "", true},
		{"lehnt Pfad nach oben ab", "../x", true},
		{"lehnt Schrägstrich ab", "a/b", true},
		{"lehnt Backslash ab", `a\b`, true},
		{"lehnt Großbuchstaben ab", "PC", true},
		{"lehnt mehr als 32 Zeichen ab", strings.Repeat("a", 33), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := pruefeName(tt.geraet); (err != nil) != tt.wantErr {
				t.Errorf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
		})
	}
}

func TestAblageGeraet(t *testing.T) {
	a := ablage{ordner: t.TempDir()}

	erstes, err := a.geraet("pc")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("lädt beim zweiten Mal denselben Schlüssel", func(t *testing.T) {
		wieder, err := a.geraet("pc")
		if err != nil {
			t.Fatal(err)
		}
		if wieder.id() != erstes.id() {
			t.Errorf("Geräte-ID %s, erwartet %s", wieder.id(), erstes.id())
		}
	})
	t.Run("anderer Name ist anderes Gerät", func(t *testing.T) {
		anderes, err := a.geraet("handy")
		if err != nil {
			t.Fatal(err)
		}
		if anderes.id() == erstes.id() {
			t.Error("gleiche Geräte-ID für verschiedene Namen")
		}
	})
	t.Run("Signatur passt zum öffentlichen Schlüssel", func(t *testing.T) {
		sig, err := erstes.signiere([]byte("nachricht"))
		if err != nil {
			t.Fatal(err)
		}
		pub, err := geraeteschluessel.Lese(erstes.der)
		if err != nil {
			t.Fatal(err)
		}
		if !geraeteschluessel.PruefeSignatur(pub, []byte("nachricht"), sig) {
			t.Error("Signatur ungültig")
		}
	})
	t.Run("lehnt beschädigte Schlüsseldatei ab", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(a.ordner, "geraet-kaputt.pem"), []byte("müll"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := a.geraet("kaputt"); err == nil {
			t.Error("kein Fehler, erwartet Fehler")
		}
	})
	t.Run("lehnt ungültigen Namen ab", func(t *testing.T) {
		if _, err := a.geraet("../pc"); err == nil {
			t.Error("kein Fehler, erwartet Fehler")
		}
	})
}

func TestAblageSitzung(t *testing.T) {
	a := ablage{ordner: t.TempDir()}
	if _, err := a.sitzung("pc"); err == nil {
		t.Error("Sitzung ohne Anmeldung gefunden, erwartet Fehler")
	}
	s := gespeicherteSitzung{GeraetID: "g", Token: "t", LaeuftAb: time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC)}
	if err := a.speichereSitzung("pc", s); err != nil {
		t.Fatal(err)
	}
	got, err := a.sitzung("pc")
	if err != nil {
		t.Fatal(err)
	}
	if got != s {
		t.Errorf("gelesen %+v, erwartet %+v", got, s)
	}
}

func TestLeseClient(t *testing.T) {
	tests := []struct {
		name    string
		daten   string
		wantErr bool
	}{
		{"nimmt Desktop-Client an", `{"installed":{"client_id":"id.apps","client_secret":"s"}}`, false},
		{"lehnt Web-Client ab", `{"web":{"client_id":"id.apps","client_secret":"s"}}`, true},
		{"lehnt fehlendes Secret ab", `{"installed":{"client_id":"id.apps"}}`, true},
		{"lehnt Müll ab", `kein json`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := leseClient([]byte(tt.daten))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if err == nil && (cfg.ClientID != "id.apps" || len(cfg.Scopes) != 1 || cfg.Scopes[0] != "openid") {
				t.Errorf("Konfiguration = %+v", cfg)
			}
		})
	}
}

func TestFindeClientDatei(t *testing.T) {
	ordner := t.TempDir()
	if _, err := findeClientDatei(ordner); err == nil {
		t.Error("ohne Datei: kein Fehler")
	}
	erste := filepath.Join(ordner, "client_secret_1.json")
	if err := os.WriteFile(erste, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := findeClientDatei(ordner); err != nil || got != erste {
		t.Errorf("= %q, %v; erwartet %q", got, err, erste)
	}
	if err := os.WriteFile(filepath.Join(ordner, "client_secret_2.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := findeClientDatei(ordner); err == nil {
		t.Error("bei zwei Dateien: kein Fehler")
	}
}

func TestRueckrufHandler(t *testing.T) {
	tests := []struct {
		name       string
		ziel       string
		wantStatus int
		wantCode   string // "" = kein Code
		wantErr    bool   // Ergebnis mit Fehler
		wantNichts bool   // gar kein Ergebnis: weiter warten
	}{
		{"nimmt Code mit unserem state an", "/?state=s1&code=abc", http.StatusOK, "abc", false, false},
		{"ignoriert fremden state", "/?state=fremd&code=abc", http.StatusBadRequest, "", false, true},
		{"ignoriert fehlenden state", "/?code=abc", http.StatusBadRequest, "", false, true},
		{"ignoriert anderen Pfad", "/anders?state=s1&code=abc", http.StatusNotFound, "", false, true},
		{"meldet Abbruch durch den Nutzer", "/?state=s1&error=access_denied", http.StatusBadRequest, "", true, false},
		{"meldet Rückleitung ohne Code", "/?state=s1", http.StatusBadRequest, "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ergebnis := make(chan rueckruf, 1)
			rec := httptest.NewRecorder()
			rueckrufHandler("s1", ergebnis).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.ziel, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("Status %d, erwartet %d", rec.Code, tt.wantStatus)
			}
			select {
			case e := <-ergebnis:
				if tt.wantNichts {
					t.Fatalf("Ergebnis %+v, erwartet keins", e)
				}
				if e.code != tt.wantCode || (e.err != nil) != tt.wantErr {
					t.Errorf("Ergebnis %+v, erwartet Code %q, Fehler %v", e, tt.wantCode, tt.wantErr)
				}
			default:
				if !tt.wantNichts {
					t.Error("kein Ergebnis, erwartet eins")
				}
			}
		})
	}
}

// TestBefehle spielt alle Befehle gegen den echten Handler durch. Ersetzt sind
// nur Google (testhilfe) und Firestore (Arbeitsspeicher).
func TestBefehle(t *testing.T) {
	const clientID = "test-client.apps.googleusercontent.com"
	jetzt := time.Now()
	google := testhilfe.NeuesGoogle(t, clientID)
	pruefer, err := anmeldung.NeuerPruefer(google.Schluessel(), []string{clientID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := dienst.NeuerHandler(dienst.Konfiguration{
		Pruefer:  pruefer,
		Speicher: speicher.NeuerArbeitsspeicher(),
		Pfeffer:  []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	var aus bytes.Buffer
	w := neuesWerkzeug(srv.URL, ablage{ordner: t.TempDir()}, &aus)
	ctx := context.Background()
	// Statt Browser und Google: ein Token für das Gerät, dessen Nonce kommt.
	angemeldetAls := func(name string) {
		t.Helper()
		g, err := w.ablage.geraet(name)
		if err != nil {
			t.Fatal(err)
		}
		w.idToken = func(_ context.Context, nonce string) (string, error) {
			if nonce != geraeteschluessel.Nonce(g.der) {
				t.Errorf("Nonce %q passt nicht zum Gerät", nonce)
			}
			return google.IDToken(t, "google-konto-1", &testhilfe.Geraet{DER: g.der}, jetzt), nil
		}
		if err := w.anmelden(ctx, name); err != nil {
			t.Fatalf("anmelden %s: %v", name, err)
		}
	}

	angemeldetAls("pc")
	if !strings.Contains(aus.String(), "neues Konto: true") {
		t.Errorf("Ausgabe: %s", aus.String())
	}
	angemeldetAls("handy")
	if !strings.Contains(aus.String(), "neues Konto: false") {
		t.Errorf("zweites Gerät landet nicht im selben Konto: %s", aus.String())
	}

	aus.Reset()
	if err := w.geraeteAuflisten(ctx, "pc"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(aus.String(), "angemeldet"); n != 2 {
		t.Errorf("%d Geräte gelistet, erwartet 2:\n%s", n, aus.String())
	}

	alteSitzung, _ := w.ablage.sitzung("pc")
	if err := w.erneuern(ctx, "pc"); err != nil {
		t.Fatalf("erneuern: %v", err)
	}
	neueSitzung, _ := w.ablage.sitzung("pc")
	if neueSitzung.Token == alteSitzung.Token {
		t.Error("Erneuern hat kein neues Token gespeichert")
	}
	if err := w.geraeteAuflisten(ctx, "pc"); err != nil {
		t.Errorf("neue Sitzung gilt nicht: %v", err)
	}

	handy, _ := w.ablage.geraet("handy")
	if err := w.sperren(ctx, "pc", handy.id()); err != nil {
		t.Fatalf("sperren: %v", err)
	}
	t.Run("gesperrtes Gerät kommt nicht mehr rein", func(t *testing.T) {
		if err := w.geraeteAuflisten(ctx, "handy"); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("Fehler = %v, erwartet Unauthenticated", err)
		}
		// PermissionDenied: Die App weiß dann, dass Erneuern nie mehr klappt.
		if err := w.erneuern(ctx, "handy"); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("Erneuern: Fehler = %v, erwartet PermissionDenied", err)
		}
	})
	t.Run("ohne gespeicherte Sitzung kein Aufruf", func(t *testing.T) {
		if err := w.geraeteAuflisten(ctx, "tablet"); err == nil || connect.CodeOf(err) != connect.CodeUnknown {
			t.Errorf("Fehler = %v, erwartet lokalen Fehler", err)
		}
	})
}

func TestAusfuehrenLehntFalscheAufrufeAb(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ohne Befehl", nil},
		{"unbekannter Befehl", []string{"loeschen"}},
		{"sperren ohne ID", []string{"sperren"}},
		{"anmelden mit Zusatz", []string{"anmelden", "x"}},
		{"unbekannter Schalter", []string{"-gibtsnicht", "geraete"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"-ordner", t.TempDir()}, tt.args...)
			if err := ausfuehren(context.Background(), args, &bytes.Buffer{}); err == nil {
				t.Error("kein Fehler, erwartet Fehler")
			}
		})
	}
}
