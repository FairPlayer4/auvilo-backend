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
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Tageslimits für Sicherungen (E-B2b-12), Diagnosen (E-D-5) und Sitzungen,
// soweit es um das Zusammenspiel mit dem Zähler geht. Die Grenzen selbst prüfen
// die Tests *HatEinTageslimitJe* und *HabenEinTageslimitJe*.

// zaehlerAttrappe zählt die Aufrufe und gibt einen festen Fehler zurück (nil:
// zählt wie der echte Zähler im Arbeitsspeicher).
type zaehlerAttrappe struct {
	mu      sync.Mutex
	aufrufe int
	fehler  error
	echt    kontingent.Zaehler
	// mitSitzungen: auch Sitzungen und neue Geräte (Anmelden,
	// SitzungErneuern) zählen und scheitern lassen. Sonst lässt die Attrappe
	// sie ungezählt durch, damit sich die Tests anmelden können.
	mitSitzungen bool
}

func (z *zaehlerAttrappe) Verbrauche(ctx context.Context, art, id string, jetzt time.Time, max int) error {
	z.mu.Lock()
	if (art == kontingentSitzung || art == kontingentNeuesGeraet) && !z.mitSitzungen {
		z.mu.Unlock()
		return nil
	}
	z.aufrufe++
	fehler := z.fehler
	z.mu.Unlock()
	if fehler != nil {
		return fehler
	}
	return z.echt.Verbrauche(ctx, art, id, jetzt, max)
}

// setzeFehler lässt ab jetzt jeden Aufruf mit err scheitern.
func (z *zaehlerAttrappe) setzeFehler(err error) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.fehler = err
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
// Server nichts durch, auch kein Holen oder Löschen. Sonst wäre das Limit genau
// dann aus, wenn es gebraucht wird (eine App in einer Schleife bei gestörtem
// Firestore).
func TestOhneTageszaehlerGehtNichts(t *testing.T) {
	ablage := datensicherung.NeuerArbeitsspeicher()
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher()}
	ts := neuerTestServerMit(t, ablage, 0, mitZaehler(z))
	token, kontoID := ts.diagnoseFreischalten(t, "google-kevin")
	// Gegenprobe: Mit Zähler geht das Speichern.
	if _, err := ts.sicherungSpeichern(token, []byte("vorher"), 0); err != nil {
		t.Fatal(err)
	}
	z.setzeFehler(errors.New("Firestore nicht erreichbar"))
	// erwarteVorher prüft direkt in der Ablage, dass die Sicherung unverändert ist.
	erwarteVorher := func(t *testing.T) {
		t.Helper()
		f, err := ablage.Holen(context.Background(), kontoID, 0)
		if err != nil || string(f.Chiffrat) != "vorher" {
			t.Errorf("Sicherung = %q, Fehler %v", f.Chiffrat, err)
		}
	}

	t.Run("Sicherung speichern", func(t *testing.T) {
		_, err := ts.sicherungSpeichern(token, []byte("sicherung"), 1)
		erwarteCode(t, err, connect.CodeUnavailable)
		erwarteVorher(t)
	})
	t.Run("Sicherung holen", func(t *testing.T) {
		_, err := ts.sicherungHolen(token)
		erwarteCode(t, err, connect.CodeUnavailable)
	})
	t.Run("Sicherung löschen", func(t *testing.T) {
		erwarteCode(t, ts.sicherungLoeschen(token), connect.CodeUnavailable)
		erwarteVorher(t)
	})
	t.Run("Diagnose senden", func(t *testing.T) {
		erwarteCode(t, ts.diagnoseSenden(token, gzipJSON(t, "{}")), connect.CodeUnavailable)
		if n := len(ts.diagnosen.Objekte()); n != 0 {
			t.Errorf("%d Diagnosen abgelegt, erwartet keine", n)
		}
	})
}

// KRITISCH: Ohne Tageszähler gibt es auch keine neue Sitzung und kein neues
// Gerät: Das Sitzungslimit wäre sonst genau bei gestörtem Firestore aus.
func TestOhneTageszaehlerGibtEsKeineSitzung(t *testing.T) {
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher(), mitSitzungen: true}
	ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitZaehler(z))
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	ts.anmelden(t, "google-kevin", handy) // Gegenprobe: mit Zähler geht es
	z.setzeFehler(errors.New("Firestore nicht erreichbar"))
	ctx := context.Background()

	_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, handy, ts.jetzt)))
	erwarteCode(t, err, connect.CodeUnavailable)
	_, err = ts.anmelde.Anmelden(ctx, connect.NewRequest(ts.anmeldeAnfrage(t, "google-kevin", laptop)))
	erwarteCode(t, err, connect.CodeUnavailable)
	if _, err := ts.speicher.Geraet(ctx, laptop.ID()); !errors.Is(err, speicher.ErrNichtGefunden) {
		t.Errorf("Laptop ist trotzdem angelegt (Fehler %v)", err)
	}
}

// KRITISCH: Was der Server ablehnt, bevor er etwas ablegt, verbraucht nichts
// vom Tageslimit – sonst könnte ein nicht freigeschaltetes Gerät oder ein Konto
// ohne Diagnose-Freigabe das Limit eines Kontos leeren.
func TestAbgelehnteAnfragenVerbrauchenNichtsVomTageslimit(t *testing.T) {
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher()}
	ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitZaehler(z))
	k := ts.kontoMitNeuemGeraet(t) // Handy hat schon eine Sicherung gespeichert
	ohneDiagnose := k.handySitzung // Konto ohne Diagnose-Erlaubnis
	mitDiagnose, _ := ts.diagnoseFreischalten(t, "google-anna")
	annasLaptop := ts.anmelden(t, "google-anna", testhilfe.NeuesGeraet(t)).Sitzung.Token // nicht freigeschaltet

	tests := []struct {
		name    string
		aufruf  func() error
		wantErr connect.Code
	}{
		{"Sicherung vom nicht freigeschalteten Gerät", func() error {
			_, err := ts.sicherungSpeichern(k.laptopSitzung, []byte("vom laptop"), 1)
			return err
		}, connect.CodePermissionDenied},
		{"Sicherung löschen vom nicht freigeschalteten Gerät", func() error {
			return ts.sicherungLoeschen(k.laptopSitzung)
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
		{"Diagnose vom nicht freigeschalteten Gerät", func() error {
			return ts.diagnoseSenden(annasLaptop, gzipJSON(t, "{}"))
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
