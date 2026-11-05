package dienst

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

func (ts *testServer) sicherungSpeichern(token string, chiffrat []byte, erwartet int64) (*v1.SicherungSpeichernResponse, error) {
	res, err := ts.sicherer.SicherungSpeichern(context.Background(), mitSitzung(&v1.SicherungSpeichernRequest{Chiffrat: chiffrat, ErwarteteVersion: erwartet}, token))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (ts *testServer) sicherungHolen(token string) (*v1.SicherungHolenResponse, error) {
	res, err := ts.sicherer.SicherungHolen(context.Background(), mitSitzung(&v1.SicherungHolenRequest{}, token))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (ts *testServer) sicherungLoeschen(token string) error {
	_, err := ts.sicherer.SicherungLoeschen(context.Background(), mitSitzung(&v1.SicherungLoeschenRequest{}, token))
	return err
}

func (ts *testServer) erwarteSicherung(t *testing.T, token, inhalt string, version int64) {
	t.Helper()
	geholt, err := ts.sicherungHolen(token)
	if err != nil {
		t.Fatalf("Holen: %v", err)
	}
	if string(geholt.Chiffrat) != inhalt || geholt.Version != version {
		t.Errorf("geholt = %q Version %d, erwartet %q Version %d", geholt.Chiffrat, geholt.Version, inhalt, version)
	}
}

// Ablauf aus docs/b2b-entwurf.md: Ein Gerät legt die Sicherung an, ein anderes
// Gerät desselben Kontos holt sie und ersetzt sie mit der gesehenen Version.
func TestZweitesGeraetHoltUndErsetztDieSicherung(t *testing.T) {
	ts := neuerTestServer(t)
	handy := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	laptopAntwort := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
	laptop := laptopAntwort.Sitzung.Token
	ts.geraetFreischaltenOK(t, handy, laptopAntwort.GeraetId) // ohne: freischaltung_test.go

	erste, err := ts.sicherungSpeichern(handy, []byte("sicherung-1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	ts.erwarteSicherung(t, laptop, "sicherung-1", erste.Version)

	zweite, err := ts.sicherungSpeichern(laptop, []byte("sicherung-2"), erste.Version)
	if err != nil {
		t.Fatal(err)
	}
	if zweite.Version <= erste.Version {
		t.Errorf("neue Version %d nicht größer als %d", zweite.Version, erste.Version)
	}
	ts.erwarteSicherung(t, handy, "sicherung-2", zweite.Version)
}

// Die Grenze aus datensicherung.proto als eigene Zahl, nicht als MaxChiffrat:
// Sonst bliebe der Test grün, wenn jemand die Konstante ändert.
const achtMiB = 8 << 20

func TestSicherungSpeichernPrueftDieAnfrage(t *testing.T) {
	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token

	var version int64
	t.Run("nimmt eine Sicherung von genau 8 MiB an", func(t *testing.T) {
		res, err := ts.sicherungSpeichern(token, make([]byte, achtMiB), 0)
		if err != nil {
			t.Fatal(err)
		}
		version = res.Version
	})

	faelle := []struct {
		name     string
		chiffrat []byte
		erwartet func() int64
		code     connect.Code
	}{
		{"lehnt leere Sicherung ab", nil, func() int64 { return version }, connect.CodeInvalidArgument},
		{"lehnt negative Version ab", []byte("chiffrat"), func() int64 { return -1 }, connect.CodeInvalidArgument},
		// Zu groß hat einen eigenen Code, damit die App "Sicherung zu groß" melden
		// kann – knapp darüber (prüft der Dienst) wie weit darüber (prüft Connect).
		{"lehnt Sicherung knapp über 8 MiB als zu groß ab", make([]byte, achtMiB+1), func() int64 { return version }, connect.CodeResourceExhausted},
		{"lehnt Sicherung weit über 8 MiB als zu groß ab", make([]byte, achtMiB+64<<10), func() int64 { return version }, connect.CodeResourceExhausted},
		// KRITISCH: Ein Gerät mit veraltetem Stand überschreibt nichts.
		{"lehnt veraltete Version ab", []byte("veraltet"), func() int64 { return 0 }, connect.CodeFailedPrecondition},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			_, err := ts.sicherungSpeichern(token, fall.chiffrat, fall.erwartet())
			erwarteCode(t, err, fall.code)
			geholt, err := ts.sicherungHolen(token)
			if err != nil {
				t.Fatal(err)
			}
			if len(geholt.Chiffrat) != achtMiB || geholt.Version != version {
				t.Errorf("Sicherung verändert: %d Bytes, Version %d", len(geholt.Chiffrat), geholt.Version)
			}
		})
	}
}

func TestSicherungHolenOhneSicherungMeldetNichtGefunden(t *testing.T) {
	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	_, err := ts.sicherungHolen(token)
	erwarteCode(t, err, connect.CodeNotFound)
}

func TestSicherungLoeschen(t *testing.T) {
	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token

	t.Run("ohne Sicherung ist kein Fehler", func(t *testing.T) {
		if err := ts.sicherungLoeschen(token); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("danach ist sie weg, und eine neue beginnt wieder mit Version 0", func(t *testing.T) {
		if _, err := ts.sicherungSpeichern(token, []byte("alt"), 0); err != nil {
			t.Fatal(err)
		}
		if err := ts.sicherungLoeschen(token); err != nil {
			t.Fatal(err)
		}
		_, err := ts.sicherungHolen(token)
		erwarteCode(t, err, connect.CodeNotFound)
		neu, err := ts.sicherungSpeichern(token, []byte("neu"), 0)
		if err != nil {
			t.Fatal(err)
		}
		ts.erwarteSicherung(t, token, "neu", neu.Version)
	})
}

// Die Grenze aus datensicherung.proto als eigene Zahl (nicht SicherungenJeTag).
const sicherungenAmTag = 48

// KRITISCH: Eine App, die in einer Schleife sichert, kann je Konto und Tag nur
// begrenzt hochladen (Kosten, Rückfallebene).
func TestSicherungSpeichernHatEinTageslimitJeKonto(t *testing.T) {
	ts := neuerTestServer(t)
	kevinGeraet := testhilfe.NeuesGeraet(t)
	kevinAntwort := ts.anmelden(t, "google-kevin", kevinGeraet)
	ts.diagnoseKonten[kevinAntwort.KontoId] = true // auch für Diagnosen, siehe unten
	kevin := kevinAntwort.Sitzung.Token
	anna := ts.anmelden(t, "google-anna", testhilfe.NeuesGeraet(t)).Sitzung.Token

	var version int64
	speichern := func(token string, inhalt string) error {
		res, err := ts.sicherungSpeichern(token, []byte(inhalt), version)
		if err == nil {
			version = res.Version
		}
		return err
	}
	for i := range sicherungenAmTag - 2 {
		if err := speichern(kevin, "sicherung"); err != nil {
			t.Fatalf("Sicherung %d: %v", i+1, err)
		}
	}
	// Abgelehnte Anfragen zählen nicht mit: leer oder zu groß.
	for range 3 {
		_, err := ts.sicherungSpeichern(kevin, nil, version)
		erwarteCode(t, err, connect.CodeInvalidArgument)
		_, err = ts.sicherungSpeichern(kevin, make([]byte, achtMiB+1), version)
		erwarteCode(t, err, connect.CodeResourceExhausted)
	}
	// Ein Konflikt zählt: Ob die Version passt, zeigt erst das Speichern.
	_, err := ts.sicherungSpeichern(kevin, []byte("veraltet"), 0)
	erwarteCode(t, err, connect.CodeFailedPrecondition)
	if err := speichern(kevin, "letzte"); err != nil {
		t.Fatalf("letzte erlaubte Sicherung: %v", err)
	}
	t.Run("danach lehnt der Server ab und lässt die Sicherung stehen", func(t *testing.T) {
		_, err := ts.sicherungSpeichern(kevin, []byte("zu viel"), version)
		erwarteCode(t, err, connect.CodeResourceExhausted)
		if err != nil && !strings.Contains(err.Error(), "Tageslimit") {
			t.Errorf("Meldung nennt das Tageslimit nicht: %v", err)
		}
		ts.erwarteSicherung(t, kevin, "letzte", version)
	})
	t.Run("Holen bleibt möglich", func(t *testing.T) {
		ts.erwarteSicherung(t, kevin, "letzte", version)
	})
	t.Run("Diagnosen haben ein eigenes Tageslimit", func(t *testing.T) {
		if err := ts.diagnoseSenden(kevin, gzipJSON(t, "{}")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ein anderes Konto hat sein eigenes Limit", func(t *testing.T) {
		if _, err := ts.sicherungSpeichern(anna, []byte("annas"), 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("am nächsten Tag (UTC) geht es wieder", func(t *testing.T) {
		ts.jetzt = time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 1, 0, time.UTC)
		neu := ts.anmelden(t, "google-kevin", kevinGeraet).Sitzung.Token // die alte Sitzung ist abgelaufen
		if err := speichern(neu, "morgen"); err != nil {
			t.Fatal(err)
		}
	})
}

// KRITISCH: Jedes Konto sieht, schreibt und löscht nur seine eigene Sicherung.
func TestSicherungGehoertNurZumEigenenKonto(t *testing.T) {
	ts := neuerTestServer(t)
	kevin := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	andere := ts.anmelden(t, "google-andere", testhilfe.NeuesGeraet(t)).Sitzung.Token
	res, err := ts.sicherungSpeichern(kevin, []byte("kevins-sicherung"), 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("anderes Konto bekommt Kevins Sicherung nicht", func(t *testing.T) {
		_, err := ts.sicherungHolen(andere)
		erwarteCode(t, err, connect.CodeNotFound)
	})
	t.Run("anderes Konto überschreibt Kevins Sicherung nicht, auch nicht mit ihrer Version", func(t *testing.T) {
		_, err := ts.sicherungSpeichern(andere, []byte("fremd"), res.Version)
		erwarteCode(t, err, connect.CodeFailedPrecondition)
		ts.erwarteSicherung(t, kevin, "kevins-sicherung", res.Version)
	})
	t.Run("Löschen im anderen Konto lässt Kevins Sicherung stehen", func(t *testing.T) {
		if _, err := ts.sicherungSpeichern(andere, []byte("andere-sicherung"), 0); err != nil {
			t.Fatal(err)
		}
		if err := ts.sicherungLoeschen(andere); err != nil {
			t.Fatal(err)
		}
		ts.erwarteSicherung(t, kevin, "kevins-sicherung", res.Version)
	})
}

func TestSicherungVerlangtGueltigeSitzung(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	handySitzung := ts.anmelden(t, "google-kevin", handy).Sitzung.Token
	laptopSitzung := ts.anmelden(t, "google-kevin", laptop).Sitzung.Token
	ts.geraetFreischaltenOK(t, handySitzung, laptop.ID())
	res, err := ts.sicherungSpeichern(laptopSitzung, []byte("sicherung"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.geraete.GeraetSperren(context.Background(), mitSitzung(&v1.GeraetSperrenRequest{GeraetId: handy.ID()}, laptopSitzung)); err != nil {
		t.Fatal(err)
	}

	faelle := []struct {
		name  string
		token string
	}{
		{"ohne Sitzung", ""},
		{"mit erfundener Sitzung", "erfunden"},
		{"mit Sitzung eines gesperrten Geräts", handySitzung},
	}
	for _, fall := range faelle {
		t.Run("Speichern lehnt ab "+fall.name, func(t *testing.T) {
			_, err := ts.sicherungSpeichern(fall.token, []byte("fremd"), res.Version)
			erwarteCode(t, err, connect.CodeUnauthenticated)
		})
		t.Run("Holen lehnt ab "+fall.name, func(t *testing.T) {
			_, err := ts.sicherungHolen(fall.token)
			erwarteCode(t, err, connect.CodeUnauthenticated)
		})
		t.Run("Löschen lehnt ab "+fall.name, func(t *testing.T) {
			erwarteCode(t, ts.sicherungLoeschen(fall.token), connect.CodeUnauthenticated)
		})
	}
	// Abgelehnte Aufrufe haben nichts verändert.
	ts.erwarteSicherung(t, laptopSitzung, "sicherung", res.Version)
}

// wartendeAblage hält jedes Speichern an, bis der Test es freigibt. So lässt
// sich prüfen, was passiert, während Sicherungen laufen.
type wartendeAblage struct {
	*datensicherung.Arbeitsspeicher
	angekommen chan struct{}
	frei       chan struct{}
}

func (w *wartendeAblage) Speichern(ctx context.Context, kontoID string, chiffrat []byte, erwartet int64) (int64, error) {
	w.angekommen <- struct{}{}
	<-w.frei
	return w.Arbeitsspeicher.Speichern(ctx, kontoID, chiffrat, erwartet)
}

// KRITISCH: Nur wenige Sicherungen gleichzeitig, sonst reicht der Arbeitsspeicher
// des Servers nicht (256 MiB auf Cloud Run). Weitere bekommen sofort
// "unavailable", die übrigen Dienste laufen weiter.
func TestZuVieleSicherungenGleichzeitigWerdenAbgewiesen(t *testing.T) {
	const grenze = 2
	ablage := &wartendeAblage{Arbeitsspeicher: datensicherung.NeuerArbeitsspeicher(), angekommen: make(chan struct{}), frei: make(chan struct{})}
	ts := neuerTestServerMit(t, ablage, grenze)
	var tokens []string
	for _, sub := range []string{"google-a", "google-b", "google-c"} {
		tokens = append(tokens, ts.anmelden(t, sub, testhilfe.NeuesGeraet(t)).Sitzung.Token)
	}

	// Zwei Sicherungen laufen und warten in der Ablage.
	ergebnisse := make(chan error, grenze)
	for i := range grenze {
		go func() {
			_, err := ts.sicherungSpeichern(tokens[i], []byte("sicherung"), 0)
			ergebnisse <- err
		}()
		<-ablage.angekommen
	}

	ok := t.Run("weitere Sicherung bekommt sofort unavailable", func(t *testing.T) {
		fertig := make(chan error, 1)
		go func() {
			_, err := ts.sicherungSpeichern(tokens[2], []byte("sicherung"), 0)
			fertig <- err
		}()
		// Greift die Grenze nicht, käme die Anfrage in der Ablage an und bliebe dort
		// hängen; der select macht daraus einen roten Test statt eines hängenden.
		// close gibt dann alle wartenden Sicherungen frei.
		select {
		case err := <-fertig:
			erwarteCode(t, err, connect.CodeUnavailable)
		case <-ablage.angekommen:
			close(ablage.frei)
			t.Fatal("die weitere Sicherung kam bis zur Ablage durch")
		case <-time.After(10 * time.Second):
			close(ablage.frei)
			t.Fatal("keine Antwort nach 10 s")
		}
	})
	if !ok {
		return // die Ablage ist schon freigegeben, der Rest des Tests sagt nichts mehr
	}
	// KRITISCH: Die Sitzung wird vor den Plätzen geprüft. Sonst könnte jeder ohne
	// Anmeldung die Plätze belegen und echte Sicherungen blockieren.
	t.Run("ohne Sitzung kommt unauthenticated statt unavailable", func(t *testing.T) {
		_, err := ts.sicherungSpeichern("erfunden", []byte("sicherung"), 0)
		erwarteCode(t, err, connect.CodeUnauthenticated)
	})
	t.Run("andere Dienste laufen weiter", func(t *testing.T) {
		if _, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, tokens[2])); err != nil {
			t.Fatal(err)
		}
	})

	for range grenze {
		ablage.frei <- struct{}{}
		if err := <-ergebnisse; err != nil {
			t.Fatalf("laufende Sicherung: %v", err)
		}
	}
	t.Run("danach ist wieder Platz", func(t *testing.T) {
		go func() { <-ablage.angekommen; ablage.frei <- struct{}{} }()
		if _, err := ts.sicherungSpeichern(tokens[2], []byte("sicherung"), 0); err != nil {
			t.Fatal(err)
		}
	})
}

// KRITISCH: Ins Log kommen Größe und Version, nie der Inhalt der Sicherung.
func TestSicherungsinhaltLandetNieImLog(t *testing.T) {
	var log bytes.Buffer
	vorher := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(vorher) })

	ts := neuerTestServer(t)
	token := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	inhalt := []byte("UNVERWECHSELBARER-INHALT")
	res, err := ts.sicherungSpeichern(token, inhalt, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.sicherungSpeichern(token, inhalt, 0); err == nil { // abgelehnt
		t.Fatal("veraltete Version angenommen")
	}
	ts.erwarteSicherung(t, token, string(inhalt), res.Version)

	if !bytes.Contains(log.Bytes(), []byte("Sicherung gespeichert")) {
		t.Fatalf("Log ohne Eintrag zum Speichern – prüft der Test das richtige Log?\n%s", log.String())
	}
	if bytes.Contains(log.Bytes(), inhalt) {
		t.Errorf("Inhalt der Sicherung im Log:\n%s", log.String())
	}
}
