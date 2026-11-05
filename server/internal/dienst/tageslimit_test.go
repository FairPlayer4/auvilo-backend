package dienst

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
)

// Tageslimits für Sicherungen (E-B2b-12) und Diagnosen (E-D-5), soweit es um
// das Zusammenspiel mit dem Zähler geht. Die Grenzen selbst prüfen
// TestSicherungSpeichernHatEinTageslimitJeKonto und
// TestDiagnoseSendenHatEinTageslimitJeKonto.

// zaehlerAttrappe zählt die Aufrufe und gibt einen festen Fehler zurück (nil:
// zählt wie der echte Zähler im Arbeitsspeicher).
type zaehlerAttrappe struct {
	mu      sync.Mutex
	aufrufe int
	fehler  error
	echt    kontingent.Zaehler
}

func (z *zaehlerAttrappe) Verbrauche(ctx context.Context, art, kontoID string, jetzt time.Time, max int) error {
	z.mu.Lock()
	z.aufrufe++
	z.mu.Unlock()
	if z.fehler != nil {
		return z.fehler
	}
	return z.echt.Verbrauche(ctx, art, kontoID, jetzt, max)
}

func (z *zaehlerAttrappe) anzahl() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.aufrufe
}

func mitZaehler(z kontingent.Zaehler) func(*Konfiguration) {
	return func(k *Konfiguration) { k.Kontingente = z }
}

// KRITISCH: Ist der Tageszähler nicht erreichbar (Firestore weg), lässt der
// Server nichts durch. Sonst wäre das Limit genau dann aus, wenn es gebraucht
// wird (eine App in einer Schleife bei gestörtem Firestore).
func TestOhneTageszaehlerWirdNichtsAbgelegt(t *testing.T) {
	ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0,
		mitZaehler(&zaehlerAttrappe{fehler: errors.New("Firestore nicht erreichbar")}))
	token, _ := ts.diagnoseFreischalten(t, "google-kevin")

	t.Run("Sicherung speichern", func(t *testing.T) {
		_, err := ts.sicherungSpeichern(token, []byte("sicherung"), 0)
		erwarteCode(t, err, connect.CodeUnavailable)
		_, err = ts.sicherungHolen(token)
		erwarteCode(t, err, connect.CodeNotFound)
	})
	t.Run("Diagnose senden", func(t *testing.T) {
		erwarteCode(t, ts.diagnoseSenden(token, gzipJSON(t, "{}")), connect.CodeUnavailable)
		if n := len(ts.diagnosen.Objekte()); n != 0 {
			t.Errorf("%d Diagnosen abgelegt, erwartet keine", n)
		}
	})
	// Gegenprobe: Holen und Löschen zählen nicht und gehen weiter.
	t.Run("Holen und Löschen gehen weiter", func(t *testing.T) {
		_, err := ts.sicherungHolen(token)
		erwarteCode(t, err, connect.CodeNotFound)
		if err := ts.sicherungLoeschen(token); err != nil {
			t.Fatal(err)
		}
	})
}

// KRITISCH: Was der Server ablehnt, bevor er etwas ablegt, verbraucht nichts
// vom Tageslimit – sonst könnte ein nicht freigeschaltetes Gerät oder ein Konto
// ohne Diagnose-Freigabe das Limit eines Kontos leeren.
func TestAbgelehnteAnfragenVerbrauchenNichtsVomTageslimit(t *testing.T) {
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher()}
	ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitZaehler(z))
	k := ts.kontoMitNeuemGeraet(t) // Handy hat schon eine Sicherung gespeichert
	ohneDiagnose := k.handySitzung // Konto nicht für Diagnosen freigeschaltet
	mitDiagnose, _ := ts.diagnoseFreischalten(t, "google-anna")

	tests := []struct {
		name    string
		aufruf  func() error
		wantErr connect.Code
	}{
		{"Sicherung vom nicht freigeschalteten Gerät", func() error {
			_, err := ts.sicherungSpeichern(k.laptopSitzung, []byte("vom laptop"), 1)
			return err
		}, connect.CodePermissionDenied},
		{"leere Sicherung", func() error {
			_, err := ts.sicherungSpeichern(k.handySitzung, nil, 1)
			return err
		}, connect.CodeInvalidArgument},
		{"Sicherung mit negativer Version", func() error {
			_, err := ts.sicherungSpeichern(k.handySitzung, []byte("x"), -1)
			return err
		}, connect.CodeInvalidArgument},
		{"zu große Sicherung", func() error {
			_, err := ts.sicherungSpeichern(k.handySitzung, make([]byte, achtMiB+1), 1)
			return err
		}, connect.CodeResourceExhausted},
		{"Diagnose von Konto ohne Freigabe", func() error {
			return ts.diagnoseSenden(ohneDiagnose, gzipJSON(t, "{}"))
		}, connect.CodePermissionDenied},
		{"zu große Diagnose", func() error {
			return ts.diagnoseSenden(mitDiagnose, mitGzipKopf(kiB256+1))
		}, connect.CodeResourceExhausted},
		{"Diagnose ohne gzip", func() error {
			return ts.diagnoseSenden(mitDiagnose, []byte("kein gzip"))
		}, connect.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vorher := z.anzahl()
			erwarteCode(t, tt.aufruf(), tt.wantErr)
			if n := z.anzahl() - vorher; n != 0 {
				t.Errorf("Tageszähler %d-mal aufgerufen, erwartet nie", n)
			}
		})
	}
	// Gegenprobe: Erlaubte Anfragen zählen.
	t.Run("erlaubte Sicherung und Diagnose zählen", func(t *testing.T) {
		alt, err := ts.sicherungHolen(k.handySitzung)
		if err != nil {
			t.Fatal(err)
		}
		vorher := z.anzahl()
		if _, err := ts.sicherungSpeichern(k.handySitzung, []byte("neu"), alt.Version); err != nil {
			t.Fatal(err)
		}
		if err := ts.diagnoseSenden(mitDiagnose, gzipJSON(t, "{}")); err != nil {
			t.Fatal(err)
		}
		if n := z.anzahl() - vorher; n != 2 {
			t.Errorf("Tageszähler %d-mal aufgerufen, erwartet 2", n)
		}
	})
}

// kaputteAblage scheitert beim Speichern, als wäre Cloud Storage gestört.
type kaputteAblage struct {
	datensicherung.Ablage
}

func (kaputteAblage) Speichern(context.Context, string, []byte, int64) (int64, error) {
	return 0, errors.New("Cloud Storage gestört")
}

// Scheitert das Ablegen nach dem Zählen, ist der Versuch trotzdem verbraucht
// (E-D-5, gleich für Sicherungen): Eine App, die bei gestörter Ablage in einer
// Schleife wiederholt, bleibt so auch begrenzt.
func TestGescheitertesAblegenZaehltTrotzdem(t *testing.T) {
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher()}
	ts := neuerTestServerMit(t, kaputteAblage{datensicherung.NeuerArbeitsspeicher()}, 0, mitZaehler(z))
	token, _ := ts.diagnoseFreischalten(t, "google-kevin")

	for i := range sicherungenAmTag {
		_, err := ts.sicherungSpeichern(token, []byte("sicherung"), 0)
		erwarteCode(t, err, connect.CodeUnavailable)
		if n := z.anzahl(); n != i+1 {
			t.Fatalf("nach %d Versuchen %d-mal gezählt", i+1, n)
		}
	}
	_, err := ts.sicherungSpeichern(token, []byte("sicherung"), 0)
	erwarteCode(t, err, connect.CodeResourceExhausted)
}
