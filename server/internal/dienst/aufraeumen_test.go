package dienst

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Die Fristen aus P5 im regelmäßigen Lauf (docs/anforderungen.md Kap. 4.4,
// docs/datenschutz/README.md K2): gesperrte Geräte nach 90 Tagen begraben,
// Konten nach 24 Monaten ohne Anmeldung oder Erneuerung löschen.

const neunzigTage = 90 * 24 * time.Hour

// KRITISCH: Nach 90 Tagen verschwinden Schlüssel und Zeitpunkte eines
// gesperrten Geräts, die Sperre bleibt: Das Gerät kommt weder über Anmelden
// noch über Erneuern wieder herein, und seine Versuche verbrauchen nicht das
// Tageslimit des Kontos für neue Geräte.
func TestLaufBegraebtGesperrteGeraeteNachNeunzigTagen(t *testing.T) {
	lt := neuerLoeschTest(t)
	ctx := context.Background()
	if err := lt.sperren(lt.k.handySitzung, lt.k.laptop.ID()); err != nil {
		t.Fatal(err)
	}

	e, err := lt.loescher.Aufraeumen(ctx, start.Add(neunzigTage))
	if err != nil || e.Begraben != 0 {
		t.Fatalf("nach genau 90 Tagen: %+v, Fehler %v; erwartet nichts begraben", e, err)
	}
	if g, err := lt.speicher.Geraet(ctx, lt.k.laptop.ID()); err != nil || len(g.Schluessel) == 0 {
		t.Fatalf("Laptop vor der Frist = %+v, Fehler %v", g, err)
	}

	lt.jetzt = start.Add(neunzigTage + time.Millisecond)
	e, err = lt.loescher.Aufraeumen(ctx, lt.jetzt)
	if err != nil || e != (AufraeumErgebnis{Begraben: 1}) {
		t.Fatalf("nach 90 Tagen: %+v, Fehler %v; erwartet ein Gerät begraben", e, err)
	}
	g, err := lt.speicher.Geraet(ctx, lt.k.laptop.ID())
	if err != nil || !g.Gesperrt || len(g.Schluessel) != 0 {
		t.Fatalf("Laptop danach = %+v, Fehler %v; erwartet Grabstein", g, err)
	}

	// Das Handy sieht den Laptop nicht mehr in der Liste.
	handy := lt.anmelden(t, "google-kevin", lt.k.handy)
	liste, err := lt.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, handy.Sitzung.Token))
	if err != nil {
		t.Fatal(err)
	}
	if len(liste.Msg.Geraete) != 1 || liste.Msg.Geraete[0].GeraetId != lt.k.handy.ID() {
		t.Errorf("Geräteliste = %v, erwartet nur das Handy", liste.Msg.Geraete)
	}

	// Der Laptop bleibt draußen, so oft er es versucht.
	for range neueGeraeteAmTag + 1 {
		_, err := lt.anmelde.Anmelden(ctx, connect.NewRequest(lt.anmeldeAnfrage(t, "google-kevin", lt.k.laptop)))
		erwarteCode(t, err, connect.CodePermissionDenied)
	}
	_, err = lt.anmelde.SitzungErneuern(ctx, connect.NewRequest(lt.erneuerAnfrage(t, lt.k.laptop, lt.jetzt)))
	erwarteCode(t, err, connect.CodeUnauthenticated)
	// Das Tageslimit für neue Geräte ist unberührt.
	lt.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
}

// KRITISCH: Ein Konto, das sich 24 Monate weder angemeldet noch eine Sitzung
// erneuert hat, bekommt einen Termin in sieben Tagen und wird dann auf dem
// üblichen Weg vollständig gelöscht. Ein aktives Konto bleibt.
func TestLaufLoeschtKontenNachVierundzwanzigMonatenOhneAnmeldung(t *testing.T) {
	lt := neuerLoeschTest(t)
	ctx := context.Background()
	zweiJahre := start.AddDate(0, 24, 0)
	// Anna meldet sich nach zwei Jahren mit einem neuen Handy an.
	lt.jetzt = zweiJahre
	lt.anmelden(t, "google-anna", testhilfe.NeuesGeraet(t))

	e, err := lt.loescher.Aufraeumen(ctx, zweiJahre)
	if err != nil || e != (AufraeumErgebnis{}) {
		t.Fatalf("nach genau 24 Monaten: %+v, Fehler %v; erwartet nichts", e, err)
	}

	tagDanach := zweiJahre.Add(24 * time.Hour)
	e, err = lt.loescher.Aufraeumen(ctx, tagDanach)
	if err != nil || e != (AufraeumErgebnis{Vorgemerkt: 1}) {
		t.Fatalf("am Tag danach: %+v, Fehler %v; erwartet Kevins Konto vorgemerkt", e, err)
	}
	termin := tagDanach.Add(7 * 24 * time.Hour)
	if ab, err := lt.speicher.LoeschenAb(ctx, lt.k.kontoID); err != nil || !ab.Equal(termin) {
		t.Fatalf("Kevins Termin = %v, Fehler %v; erwartet %v", ab, err, termin)
	}
	if ab, err := lt.speicher.LoeschenAb(ctx, lt.annaID); err != nil || !ab.IsZero() {
		t.Fatalf("Annas Termin = %v, Fehler %v; erwartet keinen", ab, err)
	}

	// Weitere Läufe schieben den Termin nicht hinaus und löschen erst an ihm.
	e, err = lt.loescher.Aufraeumen(ctx, termin.Add(-time.Millisecond))
	if err != nil || e != (AufraeumErgebnis{}) {
		t.Fatalf("vor dem Termin: %+v, Fehler %v; erwartet nichts", e, err)
	}
	e, err = lt.loescher.Aufraeumen(ctx, termin)
	if err != nil || e != (AufraeumErgebnis{Geloescht: 1}) {
		t.Fatalf("am Termin: %+v, Fehler %v; erwartet Kevins Konto gelöscht", e, err)
	}
	lt.jetzt = termin
	lt.erwarteKevinGeloescht(t)
	lt.anna = lt.anmelden(t, "google-anna", testhilfe.NeuesGeraet(t)).Sitzung.Token
	lt.erwarteAnnaUnberuehrt(t)
}

// Wer in den sieben Tagen zurückkommt, sieht den Termin auf seinen Geräten
// und bricht ihn mit einem freigeschalteten Gerät ab, wie einen Antrag. Die
// Anmeldung allein hebt ihn nicht auf: Der Speicher unterscheidet einen
// Termin wegen Inaktivität nicht von einem Antrag, und einen Antrag darf nur
// ein freigeschaltetes Gerät zurücknehmen.
func TestVorgemerktesKontoSiehtDenTerminUndKannAbbrechen(t *testing.T) {
	lt := neuerLoeschTest(t)
	ctx := context.Background()
	lt.jetzt = start.AddDate(0, 24, 0)
	lt.anmelden(t, "google-anna", testhilfe.NeuesGeraet(t)) // Anna bleibt aktiv
	tagDanach := start.AddDate(0, 24, 1)
	if e, err := lt.loescher.Aufraeumen(ctx, tagDanach); err != nil || e != (AufraeumErgebnis{Vorgemerkt: 1}) {
		t.Fatalf("%+v, Fehler %v; erwartet Kevins Konto vorgemerkt", e, err)
	}
	termin := tagDanach.Add(7 * 24 * time.Hour)

	lt.jetzt = tagDanach.Add(time.Hour)
	handy := lt.anmelden(t, "google-kevin", lt.k.handy)
	if handy.LoeschenAb == nil || !handy.LoeschenAb.AsTime().Equal(termin) {
		t.Fatalf("Anmelden: Termin = %v, erwartet %v", handy.LoeschenAb, termin)
	}
	laptop := lt.anmelden(t, "google-kevin", lt.k.laptop)
	erwarteCode(t, lt.loeschenAbbrechen(laptop.Sitzung.Token), connect.CodePermissionDenied)
	if err := lt.loeschenAbbrechen(handy.Sitzung.Token); err != nil {
		t.Fatal(err)
	}

	e, err := lt.loescher.Aufraeumen(ctx, termin)
	if err != nil || e != (AufraeumErgebnis{}) {
		t.Fatalf("am Termin: %+v, Fehler %v; erwartet nichts", e, err)
	}
	lt.erwarteUnveraendert(t, konto{handySitzung: handy.Sitzung.Token})
}

// kaputtesBegraben scheitert bei jedem Begraben.
type kaputtesBegraben struct {
	*speicher.Arbeitsspeicher
}

func (kaputtesBegraben) GeraetBegraben(context.Context, string, time.Time) error {
	return errors.New("Firestore nicht erreichbar")
}

// Scheitert ein Schritt, laufen die anderen trotzdem; der Fehler kommt zurück.
func TestLaufMachtNachEinemFehlerBeimBegrabenWeiter(t *testing.T) {
	lt := neuerLoeschTest(t)
	ctx := context.Background()
	// Kevins Laptop beantragt die Löschung; Otto hat ein gesperrtes Gerät.
	if _, err := lt.kontoLoeschen(lt.k.laptopSitzung); err != nil {
		t.Fatal(err)
	}
	ottoHandy, ottoAlt := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	otto := lt.anmelden(t, "google-otto", ottoHandy).Sitzung.Token
	lt.anmelden(t, "google-otto", ottoAlt)
	if err := lt.sperren(otto, ottoAlt.ID()); err != nil {
		t.Fatal(err)
	}

	loescher := NeuerLoescher(kaputtesBegraben{lt.speicher}, lt.ablage, lt.diagnosen)
	e, err := loescher.Aufraeumen(ctx, start.AddDate(0, 24, 1))
	if err == nil || e != (AufraeumErgebnis{Vorgemerkt: 2, Geloescht: 1}) {
		t.Fatalf("%+v, Fehler %v; erwartet Anna und Otto vorgemerkt, Kevin gelöscht und einen Fehler", e, err)
	}
	if g, err := lt.speicher.Geraet(ctx, ottoAlt.ID()); err != nil || len(g.Schluessel) == 0 {
		t.Errorf("Ottos altes Gerät = %+v, Fehler %v; erwartet unverändert", g, err)
	}
}
