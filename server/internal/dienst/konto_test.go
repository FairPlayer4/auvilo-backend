package dienst

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Konto löschen (B1, konto.proto): sofort mit freigeschaltetem Gerät, sonst
// Antrag mit Wartezeit; Abbrechen; der regelmäßige Lauf.

// loeschAblage ist eine Ablage für Sicherungen, deren Loeschen der Test
// scheitern lassen kann.
type loeschAblage struct {
	*datensicherung.Arbeitsspeicher
	kaputt bool
}

func (a *loeschAblage) Loeschen(ctx context.Context, kontoID string) error {
	if a.kaputt {
		return errors.New("Cloud Storage nicht erreichbar")
	}
	return a.Arbeitsspeicher.Loeschen(ctx, kontoID)
}

// loeschTest: Testserver mit Zugriff auf die Ablage der Sicherungen und einem
// Konto (Handy freigeschaltet, Laptop nicht) samt Sicherung, Backup und
// Diagnose. Dazu Anna als zweites Konto mit denselben Daten, das nie
// mitgelöscht werden darf.
type loeschTest struct {
	*testServer
	ablage   *loeschAblage
	loescher *Loescher
	k        konto
	anna     string // Sitzung von Anna
	annaID   string
}

func neuerLoeschTest(t *testing.T) *loeschTest {
	t.Helper()
	ablage := &loeschAblage{Arbeitsspeicher: datensicherung.NeuerArbeitsspeicher()}
	ts := neuerTestServerMit(t, ablage, 0)
	lt := &loeschTest{testServer: ts, ablage: ablage, loescher: NeuerLoescher(ts.speicher, ablage, ts.diagnosen)}
	lt.k = ts.kontoMitNeuemGeraet(t)
	ts.diagnoseKonten[lt.k.kontoID] = true
	if err := ts.diagnoseSenden(lt.k.handySitzung, gzipJSON(t, `{"ereignis":"kevin"}`)); err != nil {
		t.Fatal(err)
	}
	lt.anna, lt.annaID = ts.diagnoseFreischalten(t, "google-anna")
	if _, err := ts.backupSpeichern(lt.anna, []byte("backup-anna"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.sicherungSpeichern(lt.anna, []byte("sicherung-anna"), 0); err != nil {
		t.Fatal(err)
	}
	if err := ts.diagnoseSenden(lt.anna, gzipJSON(t, `{"ereignis":"anna"}`)); err != nil {
		t.Fatal(err)
	}
	return lt
}

func (ts *testServer) kontoLoeschen(token string) (*v1.KontoLoeschenResponse, error) {
	res, err := ts.konto.KontoLoeschen(context.Background(), mitSitzung(&v1.KontoLoeschenRequest{}, token))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (ts *testServer) loeschenAbbrechen(token string) error {
	_, err := ts.konto.KontoLoeschenAbbrechen(context.Background(), mitSitzung(&v1.KontoLoeschenAbbrechenRequest{}, token))
	return err
}

// loeschenAbInListe liest loeschen_ab aus GeraeteAuflisten (Nullwert = keiner).
func (ts *testServer) loeschenAbInListe(t *testing.T, token string) time.Time {
	t.Helper()
	res, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, token))
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg.LoeschenAb == nil {
		return time.Time{}
	}
	return res.Msg.LoeschenAb.AsTime()
}

// erwarteKevinGeloescht prüft, dass vom Konto nichts übrig ist: keine Sitzung,
// keine Geräte, kein Backup, keine Sicherung, keine Diagnose. Ein neues
// Anmelden legt ein neues, leeres Konto an.
func (lt *loeschTest) erwarteKevinGeloescht(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, token := range []string{lt.k.handySitzung, lt.k.laptopSitzung} {
		_, err := lt.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, token))
		erwarteCode(t, err, connect.CodeUnauthenticated)
	}
	for _, g := range []*testhilfe.Geraet{lt.k.handy, lt.k.laptop} {
		if _, err := lt.speicher.Geraet(ctx, g.ID()); !errors.Is(err, speicher.ErrNichtGefunden) {
			t.Errorf("Gerät noch da: %v", err)
		}
	}
	if _, err := lt.speicher.BackupHolen(ctx, lt.k.kontoID); !errors.Is(err, speicher.ErrNichtGefunden) {
		t.Errorf("Backup noch da: %v", err)
	}
	if f, err := lt.ablage.Fassungen(ctx, lt.k.kontoID); err != nil || len(f) != 0 {
		t.Errorf("Sicherung noch da: %v, Fehler %v", f, err)
	}
	for name := range lt.diagnosen.Objekte() {
		if strings.HasPrefix(name, "diagnose/"+lt.k.kontoID+"/") {
			t.Errorf("Diagnose noch da: %s", name)
		}
	}
	neu := lt.anmelden(t, "google-kevin", lt.k.handy)
	if !neu.NeuesKonto || neu.KontoId == lt.k.kontoID || neu.LoeschenAb != nil {
		t.Errorf("Anmelden danach = %+v, erwartet neues Konto ohne Termin", neu)
	}
	_, err := lt.sicherungHolen(neu.Sitzung.Token)
	erwarteCode(t, err, connect.CodeNotFound)
}

// erwarteAnnaUnberuehrt prüft, dass das andere Konto alles behalten hat.
func (lt *loeschTest) erwarteAnnaUnberuehrt(t *testing.T) {
	t.Helper()
	if b, err := lt.backupHolen(lt.anna); err != nil || string(b.Chiffrat) != "backup-anna" {
		t.Errorf("Annas Backup = %v, Fehler %v", b, err)
	}
	if s, err := lt.sicherungHolen(lt.anna); err != nil || string(s.Chiffrat) != "sicherung-anna" {
		t.Errorf("Annas Sicherung = %v, Fehler %v", s, err)
	}
	n := 0
	for name := range lt.diagnosen.Objekte() {
		if strings.HasPrefix(name, "diagnose/"+lt.annaID+"/") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Annas Diagnosen: %d, erwartet 1", n)
	}
}

// KRITISCH: Ein freigeschaltetes Gerät löscht sofort alles, was der Server zum
// Konto hat, und nur das.
func TestFreigeschaltetesGeraetLoeschtDasKontoSofort(t *testing.T) {
	lt := neuerLoeschTest(t)
	res, err := lt.kontoLoeschen(lt.k.handySitzung)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Geloescht || res.LoeschenAb != nil {
		t.Errorf("Antwort = %+v, erwartet gelöscht ohne Termin", res)
	}
	lt.erwarteKevinGeloescht(t)
	lt.erwarteAnnaUnberuehrt(t)
}

// KRITISCH: Wer nur den Google-Login hat, löscht nichts sofort. Er bekommt
// einen Termin in sieben Tagen, und die echten Geräte sehen ihn.
func TestNichtFreigeschaltetesGeraetBeantragtNurDieLoeschung(t *testing.T) {
	lt := neuerLoeschTest(t)
	res, err := lt.kontoLoeschen(lt.k.laptopSitzung)
	if err != nil {
		t.Fatal(err)
	}
	termin := start.Add(7 * 24 * time.Hour)
	if res.Geloescht || res.LoeschenAb == nil || !res.LoeschenAb.AsTime().Equal(termin) {
		t.Fatalf("Antwort = %+v, erwartet Termin %v", res, termin)
	}
	lt.erwarteUnveraendert(t, lt.k)

	// Die echten Geräte sehen den Termin in allen drei Antworten.
	if ab := lt.loeschenAbInListe(t, lt.k.handySitzung); !ab.Equal(termin) {
		t.Errorf("GeraeteAuflisten: Termin = %v, erwartet %v", ab, termin)
	}
	if ab := lt.anmelden(t, "google-kevin", lt.k.handy).LoeschenAb; ab == nil || !ab.AsTime().Equal(termin) {
		t.Errorf("Anmelden: Termin = %v, erwartet %v", ab, termin)
	}
	erneuert, err := lt.anmelde.SitzungErneuern(context.Background(), connect.NewRequest(lt.erneuerAnfrage(t, lt.k.handy, lt.jetzt)))
	if err != nil {
		t.Fatal(err)
	}
	if ab := erneuert.Msg.LoeschenAb; ab == nil || !ab.AsTime().Equal(termin) {
		t.Errorf("SitzungErneuern: Termin = %v, erwartet %v", ab, termin)
	}
	// Annas Konto zeigt keinen Termin.
	if ab := lt.loeschenAbInListe(t, lt.anna); !ab.IsZero() {
		t.Errorf("Annas Termin = %v, erwartet keinen", ab)
	}

	// Ein weiterer Antrag später schiebt den Termin nicht hinaus.
	lt.jetzt = start.Add(30 * time.Minute)
	res, err = lt.kontoLoeschen(lt.k.laptopSitzung)
	if err != nil {
		t.Fatal(err)
	}
	if res.Geloescht || !res.LoeschenAb.AsTime().Equal(termin) {
		t.Errorf("zweiter Antrag = %+v, erwartet unveränderten Termin %v", res, termin)
	}
}

func TestOhneAntragZeigenDieAntwortenKeinenTermin(t *testing.T) {
	ts := neuerTestServer(t)
	handy := testhilfe.NeuesGeraet(t)
	angemeldet := ts.anmelden(t, "google-kevin", handy)
	if angemeldet.LoeschenAb != nil {
		t.Errorf("Anmelden: Termin = %v", angemeldet.LoeschenAb)
	}
	if ab := ts.loeschenAbInListe(t, angemeldet.Sitzung.Token); !ab.IsZero() {
		t.Errorf("GeraeteAuflisten: Termin = %v", ab)
	}
	erneuert, err := ts.anmelde.SitzungErneuern(context.Background(), connect.NewRequest(ts.erneuerAnfrage(t, handy, ts.jetzt)))
	if err != nil {
		t.Fatal(err)
	}
	if erneuert.Msg.LoeschenAb != nil {
		t.Errorf("SitzungErneuern: Termin = %v", erneuert.Msg.LoeschenAb)
	}
}

// KRITISCH: Abbrechen darf nur ein freigeschaltetes Gerät. Sonst könnte, wer
// nur den Google-Login hat, einen Antrag des Besitzers zurücknehmen.
func TestAntragAbbrechen(t *testing.T) {
	t.Run("lehnt ab: nicht freigeschaltetes Gerät", func(t *testing.T) {
		lt := neuerLoeschTest(t)
		if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
		erwarteCode(t, lt.loeschenAbbrechen(lt.k.laptopSitzung), connect.CodePermissionDenied)
		if ab := lt.loeschenAbInListe(t, lt.k.handySitzung); ab.IsZero() {
			t.Error("Termin nach abgelehntem Abbrechen verschwunden")
		}
	})
	t.Run("erlaubt: freigeschaltetes Gerät, danach löscht der Lauf nichts", func(t *testing.T) {
		lt := neuerLoeschTest(t)
		if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
		if err := lt.loeschenAbbrechen(lt.k.handySitzung); err != nil {
			t.Fatal(err)
		}
		if ab := lt.loeschenAbInListe(t, lt.k.handySitzung); !ab.IsZero() {
			t.Errorf("Termin nach dem Abbrechen = %v", ab)
		}
		n, err := lt.loescher.Aufraeumen(context.Background(), start.Add(30*24*time.Hour))
		if err != nil || n != 0 {
			t.Fatalf("Lauf: %d gelöscht, Fehler %v", n, err)
		}
		lt.erwarteUnveraendert(t, lt.k)
	})
	t.Run("ohne Antrag ist Abbrechen kein Fehler", func(t *testing.T) {
		lt := neuerLoeschTest(t)
		if err := lt.loeschenAbbrechen(lt.k.handySitzung); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("lehnt ab: Termin schon erreicht", func(t *testing.T) {
		lt := neuerLoeschTest(t)
		if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
		// Sieben Tage später: neu anmelden (die Sitzung gilt eine Stunde).
		lt.jetzt = start.Add(7 * 24 * time.Hour)
		handy := lt.anmelden(t, "google-kevin", lt.k.handy)
		erwarteCode(t, lt.loeschenAbbrechen(handy.Sitzung.Token), connect.CodeFailedPrecondition)
	})
}

// KRITISCH: Der Lauf löscht ein beantragtes Konto erst am Termin, dann aber
// vollständig; Anna bleibt unberührt.
func TestLaufLoeschtBeantragteKontenAmTermin(t *testing.T) {
	lt := neuerLoeschTest(t)
	if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
		t.Fatal(err)
	}
	termin := start.Add(7 * 24 * time.Hour)
	ctx := context.Background()

	n, err := lt.loescher.Aufraeumen(ctx, termin.Add(-time.Millisecond))
	if err != nil || n != 0 {
		t.Fatalf("vor dem Termin: %d gelöscht, Fehler %v", n, err)
	}
	lt.erwarteUnveraendert(t, lt.k)

	n, err = lt.loescher.Aufraeumen(ctx, termin)
	if err != nil || n != 1 {
		t.Fatalf("am Termin: %d gelöscht, Fehler %v", n, err)
	}
	lt.erwarteKevinGeloescht(t)
	lt.erwarteAnnaUnberuehrt(t)
}

// KRITISCH: Scheitert das Löschen mittendrin, ist der Zugang schon weg, das
// Konto bleibt fällig, und der nächste Lauf löscht den Rest.
func TestAbgebrochenesLoeschenHoltDerLaufNach(t *testing.T) {
	lt := neuerLoeschTest(t)
	lt.ablage.kaputt = true
	_, err := lt.kontoLoeschen(lt.k.handySitzung)
	erwarteCode(t, err, connect.CodeUnavailable)
	_, err = lt.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, lt.k.handySitzung))
	erwarteCode(t, err, connect.CodeUnauthenticated)

	// Der Lauf scheitert ebenso, meldet den Fehler und lässt das Konto fällig.
	n, err := lt.loescher.Aufraeumen(context.Background(), start)
	if err == nil || n != 0 {
		t.Fatalf("Lauf mit kaputter Ablage: %d gelöscht, Fehler %v", n, err)
	}
	ids, err := lt.speicher.FaelligeLoeschungen(context.Background(), start, 10)
	if err != nil || len(ids) != 1 || ids[0] != lt.k.kontoID {
		t.Fatalf("fällig = %v, Fehler %v", ids, err)
	}

	lt.ablage.kaputt = false
	n, err = lt.loescher.Aufraeumen(context.Background(), start)
	if err != nil || n != 1 {
		t.Fatalf("Lauf danach: %d gelöscht, Fehler %v", n, err)
	}
	lt.erwarteKevinGeloescht(t)
	lt.erwarteAnnaUnberuehrt(t)
}

// Scheitert ein Konto, löscht der Lauf die übrigen trotzdem.
func TestLaufMachtNachEinemFehlerWeiter(t *testing.T) {
	lt := neuerLoeschTest(t)
	ctx := context.Background()
	// Kevin und Anna beantragen; Annas Konto lässt sich nicht löschen.
	if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.speicher.LoeschungBeantragen(ctx, lt.annaID, start); err != nil {
		t.Fatal(err)
	}
	kaputt := &kaputteAblageFuer{lt.ablage, lt.annaID}
	loescher := NeuerLoescher(lt.speicher, kaputt, lt.diagnosen)
	n, err := loescher.Aufraeumen(ctx, start.Add(7*24*time.Hour))
	if err == nil || n != 1 {
		t.Fatalf("%d gelöscht, Fehler %v; erwartet 1 und einen Fehler", n, err)
	}
	lt.erwarteKevinGeloescht(t)
	ids, err := lt.speicher.FaelligeLoeschungen(ctx, start.Add(7*24*time.Hour), 10)
	if err != nil || len(ids) != 1 || ids[0] != lt.annaID {
		t.Errorf("fällig = %v, Fehler %v; erwartet Annas Konto", ids, err)
	}
}

// kaputteAblageFuer scheitert beim Löschen eines bestimmten Kontos.
type kaputteAblageFuer struct {
	*loeschAblage
	kontoID string
}

func (a *kaputteAblageFuer) Loeschen(ctx context.Context, kontoID string) error {
	if kontoID == a.kontoID {
		return errors.New("Cloud Storage nicht erreichbar")
	}
	return a.loeschAblage.Loeschen(ctx, kontoID)
}
