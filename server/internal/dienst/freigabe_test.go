package dienst

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/anmeldung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Freigabeliste (ID7, docs/anforderungen.md): Nur freigegebene Google-Konten
// melden sich an und erneuern ihre Sitzung.

// kennungVon rechnet die Kennung so aus wie der Testserver.
func kennungVon(sub string) string { return anmeldung.Kennung(testPfeffer, sub) }

// mitFreigabe stellt die Freigabeliste des Testservers ein.
func mitFreigabe(f Freigabe) func(*Konfiguration) {
	return func(k *Konfiguration) { k.Freigabe = f }
}

// erwarteNichtFreigegeben prüft Code und Fehlerdetail mit der Kennung.
func erwarteNichtFreigegeben(t *testing.T, err error, kennung string) {
	t.Helper()
	erwarteCode(t, err, connect.CodeFailedPrecondition)
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("kein Connect-Fehler: %v", err)
	}
	for _, d := range ce.Details() {
		if v, e := d.Value(); e == nil {
			if nf, ok := v.(*v1.NichtFreigegeben); ok {
				if nf.GetKennung() != kennung {
					t.Errorf("Kennung im Detail = %q, erwartet %q", nf.GetKennung(), kennung)
				}
				return
			}
		}
	}
	t.Errorf("Fehlerdetail NichtFreigegeben fehlt: %v", err)
}

func TestAnmeldenNurMitFreigegebenerKennung(t *testing.T) {
	faelle := []struct {
		name     string
		freigabe Freigabe
		sub      string
		erlaubt  bool
	}{
		{"freigegebene Kennung meldet sich an", FreigabeFuer([]string{kennungVon("google-kevin")}), "google-kevin", true},
		{"alle freigegeben: jede Kennung meldet sich an", FreigabeAlle(), "google-fremd", true},
		{"lehnt Kennung ab, die nicht auf der Liste steht", FreigabeFuer([]string{kennungVon("google-kevin")}), "google-fremd", false},
		{"lehnt bei leerer Liste jede Kennung ab", FreigabeFuer(nil), "google-kevin", false},
		// KRITISCH: Eine vergessene Einstellung öffnet den Server nicht.
		{"lehnt ohne Einstellung (Nullwert) jede Kennung ab", Freigabe{}, "google-kevin", false},
		{"lehnt Teil einer freigegebenen Kennung ab", FreigabeFuer([]string{kennungVon("google-kevin") + "00"}), "google-kevin", false},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitFreigabe(fall.freigabe))
			g := testhilfe.NeuesGeraet(t)
			_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(ts.anmeldeAnfrage(t, fall.sub, g)))
			if fall.erlaubt {
				if err != nil {
					t.Fatalf("Anmelden: %v", err)
				}
				return
			}
			erwarteNichtFreigegeben(t, err, kennungVon(fall.sub))
			if _, err := ts.speicher.Geraet(context.Background(), g.ID()); !errors.Is(err, speicher.ErrNichtGefunden) {
				t.Errorf("Gerät angelegt (Fehler %v)", err)
			}
		})
	}
}

// KRITISCH: Ein nicht freigegebenes Konto verbraucht nichts vom Tageszähler
// (der Fall prüft, dass die Freigabe vor dem Zählen kommt).
func TestNichtFreigegebenesKontoZaehltNicht(t *testing.T) {
	z := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher(), mitSitzungen: true}
	ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitFreigabe(FreigabeFuer(nil)),
		func(k *Konfiguration) { k.Kontingente = z })
	_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(ts.anmeldeAnfrage(t, "google-fremd", testhilfe.NeuesGeraet(t))))
	erwarteNichtFreigegeben(t, err, kennungVon("google-fremd"))
	if z.anzahl() != 0 {
		t.Errorf("Tageszähler %d-mal aufgerufen, erwartet 0", z.anzahl())
	}
}

// kennungSpeicher ersetzt KennungVon, sonst wie der Arbeitsspeicher.
type kennungSpeicher struct {
	*speicher.Arbeitsspeicher
	fehler error
}

func (s kennungSpeicher) KennungVon(context.Context, string) (string, error) {
	return "", s.fehler
}

// sitzungsSpion zählt, wie oft eine Sitzung angelegt wird.
type sitzungsSpion struct {
	speicher.Speicher
	angelegt int
}

func (s *sitzungsSpion) SitzungAnlegen(ctx context.Context, geraetID string, neu speicher.NeueSitzung, jetzt time.Time) (string, error) {
	s.angelegt++
	return s.Speicher.SitzungAnlegen(ctx, geraetID, neu, jetzt)
}

// Zu jeder Erlaubnis die Ablehnung. Abgelehnt heißt auch: keine neue Sitzung
// und kein verbrauchter Tageszähler (Review-Lauf 11), sonst könnte ein
// entzogenes Gerät die Sitzungen seines Tages aufbrauchen.
func TestErneuernPrueftDieFreigabe(t *testing.T) {
	kevin := kennungVon("google-kevin")
	faelle := []struct {
		name     string
		freigabe Freigabe
		speicher func(*speicher.Arbeitsspeicher) speicher.Speicher
		pruefe   func(t *testing.T, err error)
		erneuert bool
	}{
		{"erneuert, solange die Kennung freigegeben ist", FreigabeFuer([]string{kevin}), nil,
			func(t *testing.T, err error) {
				if err != nil {
					t.Fatal(err)
				}
			}, true},
		{"erneuert ohne Lesen der Kennung, wenn alle freigegeben sind", FreigabeAlle(),
			func(a *speicher.Arbeitsspeicher) speicher.Speicher {
				return kennungSpeicher{a, errors.New("darf nicht gelesen werden")}
			},
			func(t *testing.T, err error) {
				if err != nil {
					t.Fatal(err)
				}
			}, true},
		// KRITISCH: Entzug wirkt spätestens mit dem Ablauf der Sitzung.
		{"lehnt ab, wenn die Kennung von der Liste genommen wurde", FreigabeFuer(nil), nil,
			func(t *testing.T, err error) { erwarteNichtFreigegeben(t, err, kevin) }, false},
		{"lehnt ab, wenn das Konto keine Kennung mehr hat", FreigabeFuer([]string{kevin}),
			func(a *speicher.Arbeitsspeicher) speicher.Speicher {
				return kennungSpeicher{a, speicher.ErrNichtGefunden}
			},
			func(t *testing.T, err error) { erwarteCode(t, err, connect.CodeUnauthenticated) }, false},
		{"meldet Speicherfehler beim Lesen der Kennung als vorübergehend", FreigabeFuer([]string{kevin}),
			func(a *speicher.Arbeitsspeicher) speicher.Speicher {
				return kennungSpeicher{a, errors.New("Firestore gestört")}
			},
			func(t *testing.T, err error) { erwarteCode(t, err, connect.CodeUnavailable) }, false},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			// Angemeldet wird bei einem Server, der alle zulässt; erneuert bei
			// einem zweiten mit derselben Datenbank und der Freigabe des Falls.
			vorher := neuerTestServer(t)
			handy := testhilfe.NeuesGeraet(t)
			vorher.anmelden(t, "google-kevin", handy)
			spion := &sitzungsSpion{Speicher: vorher.speicher}
			if fall.speicher != nil {
				spion.Speicher = fall.speicher(vorher.speicher)
			}
			zaehler := &zaehlerAttrappe{echt: kontingent.NeuerArbeitsspeicher(), mitSitzungen: true}
			nachher := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitFreigabe(fall.freigabe), mitZaehler(zaehler),
				func(k *Konfiguration) { k.Speicher = spion })
			_, err := nachher.anmelde.SitzungErneuern(context.Background(), connect.NewRequest(nachher.erneuerAnfrage(t, handy, nachher.jetzt)))
			fall.pruefe(t, err)
			// Erneuert: genau eine Sitzung und ein Zählerschritt; abgelehnt: keins von beiden.
			erwartet := 0
			if fall.erneuert {
				erwartet = 1
			}
			if spion.angelegt != erwartet || zaehler.anzahl() != erwartet {
				t.Errorf("Sitzungen angelegt: %d, Zähler verbraucht: %d, erwartet je %d", spion.angelegt, zaehler.anzahl(), erwartet)
			}
		})
	}
}

// Eine leere Kennung ist nie zugelassen, auch nicht durch einen leeren Eintrag
// in der Liste; nur FreigabeAlle lässt alles zu (KRITISCH in erlaubt).
func TestLeereKennungNurBeiFreigabeAlle(t *testing.T) {
	kevin := kennungVon("google-kevin")
	faelle := []struct {
		name     string
		freigabe Freigabe
		kennung  string
		erlaubt  bool
	}{
		{"Kennung auf der Liste ist zugelassen", FreigabeFuer([]string{kevin}), kevin, true},
		{"alle freigegeben: auch die leere Kennung", FreigabeAlle(), "", true},
		{"lehnt die leere Kennung ab", FreigabeFuer([]string{kevin}), "", false},
		{"lehnt die leere Kennung trotz leerem Eintrag ab", FreigabeFuer([]string{kevin, ""}), "", false},
		{"lehnt die leere Kennung bei leerer Liste ab", FreigabeFuer(nil), "", false},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			if got := fall.freigabe.erlaubt(fall.kennung); got != fall.erlaubt {
				t.Errorf("erlaubt(%q) = %v, erwartet %v", fall.kennung, got, fall.erlaubt)
			}
		})
	}
}

// zugriffsSpion zählt die Aufrufe, die ein Anmelden braucht (Anmelden und
// LoeschenAb). Jeder andere Aufruf geht an Speicher; ist der nil, scheitert er
// mit einer Panik, die net/http als abgebrochene Verbindung meldet.
type zugriffsSpion struct {
	speicher.Speicher
	zugriffe int
}

func (s *zugriffsSpion) Anmelden(ctx context.Context, kennung string, g speicher.Geraet, neu speicher.NeueSitzung) (speicher.AnmeldeErgebnis, error) {
	s.zugriffe++
	if s.Speicher == nil {
		return speicher.AnmeldeErgebnis{}, errors.New("kein Speicher")
	}
	return s.Speicher.Anmelden(ctx, kennung, g, neu)
}

func (s *zugriffsSpion) LoeschenAb(ctx context.Context, kontoID string) (time.Time, error) {
	s.zugriffe++
	if s.Speicher == nil {
		return time.Time{}, errors.New("kein Speicher")
	}
	return s.Speicher.LoeschenAb(ctx, kontoID)
}

// KRITISCH (pruefeFreigabe, Review-Lauf 11): Ein nicht freigegebenes Konto wird
// vor jedem Zugriff auf den Speicher abgelehnt. Die Gegenprobe zeigt, dass der
// Spion ein Anmelden sieht.
func TestNichtFreigegebenesKontoErreichtDenSpeicherNicht(t *testing.T) {
	faelle := []struct {
		name     string
		freigabe Freigabe
		echt     bool // false: Speicher ohne Umsetzung
	}{
		{"freigegeben: Anmelden erreicht den Speicher", FreigabeAlle(), true},
		{"nicht freigegeben: abgelehnt ohne einen Zugriff", FreigabeFuer(nil), false},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			spion := &zugriffsSpion{}
			if fall.echt {
				spion.Speicher = speicher.NeuerArbeitsspeicher()
			}
			ts := neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0, mitFreigabe(fall.freigabe),
				func(k *Konfiguration) { k.Speicher = spion })
			_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(ts.anmeldeAnfrage(t, "google-fremd", testhilfe.NeuesGeraet(t))))
			if fall.echt {
				if err != nil || spion.zugriffe == 0 {
					t.Fatalf("Anmelden: Fehler %v, %d Zugriffe; erwartet Erfolg mit Zugriffen", err, spion.zugriffe)
				}
				return
			}
			erwarteNichtFreigegeben(t, err, kennungVon("google-fremd"))
			if spion.zugriffe != 0 {
				t.Errorf("%d Zugriffe auf den Speicher, erwartet 0", spion.zugriffe)
			}
		})
	}
}
