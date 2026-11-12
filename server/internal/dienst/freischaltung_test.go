package dienst

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/freischaltung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/sitzung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Freischaltung neuer Geräte (E-B2b-13, geraete.proto). Die Ableitung aus dem
// Wiederherstellungsschlüssel prüfen die Testvektoren (Paket freischaltung,
// Kotlin); hier steht ein fester Schlüssel für den Wiederherstellungsschlüssel.

var fsPrivat = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))

// fsOeffentlich ist der Freischaltschlüssel, den die Tests mit dem Backup hochladen.
func fsOeffentlich() []byte { return fsPrivat.Public().(ed25519.PublicKey) }

func signiereFreischaltung(schluessel ed25519.PrivateKey, geraetID string) []byte {
	return ed25519.Sign(schluessel, freischaltung.Nachricht(geraetID))
}

func (ts *testServer) geraetFreischalten(token, geraetID string) error {
	_, err := ts.geraete.GeraetFreischalten(context.Background(), mitSitzung(&v1.GeraetFreischaltenRequest{GeraetId: geraetID}, token))
	return err
}

func (ts *testServer) geraetFreischaltenOK(t *testing.T, token, geraetID string) {
	t.Helper()
	if err := ts.geraetFreischalten(token, geraetID); err != nil {
		t.Fatalf("Freischalten: %v", err)
	}
}

func (ts *testServer) mitWiederherstellungFreischalten(token string, signatur []byte) error {
	_, err := ts.geraete.MitWiederherstellungFreischalten(context.Background(), mitSitzung(&v1.MitWiederherstellungFreischaltenRequest{Signatur: signatur}, token))
	return err
}

func (ts *testServer) sperren(token, geraetID string) error {
	_, err := ts.geraete.GeraetSperren(context.Background(), mitSitzung(&v1.GeraetSperrenRequest{GeraetId: geraetID}, token))
	return err
}

// konto ist ein Konto mit einem vertrauten Handy (hat Backup und Sicherung
// angelegt) und einem neu angemeldeten Laptop.
type konto struct {
	handy, laptop               *testhilfe.Geraet
	handySitzung, laptopSitzung string
	laptopFreigeschaltet        bool // laut Antwort beim Anmelden
	kontoID                     string
}

func (ts *testServer) kontoMitNeuemGeraet(t *testing.T) konto {
	t.Helper()
	k := konto{handy: testhilfe.NeuesGeraet(t), laptop: testhilfe.NeuesGeraet(t)}
	handy := ts.anmelden(t, "google-kevin", k.handy)
	k.handySitzung, k.kontoID = handy.Sitzung.Token, handy.KontoId
	if _, err := ts.backupSpeichern(k.handySitzung, []byte("backup-handy"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.sicherungSpeichern(k.handySitzung, []byte("sicherung-handy"), 0); err != nil {
		t.Fatal(err)
	}
	antwort := ts.anmelden(t, "google-kevin", k.laptop)
	k.laptopSitzung, k.laptopFreigeschaltet = antwort.Sitzung.Token, antwort.Freigeschaltet
	return k
}

// erwarteUnveraendert prüft, dass das Handy noch arbeitet und Backup und
// Sicherung so sind, wie es sie angelegt hat.
func (ts *testServer) erwarteUnveraendert(t *testing.T, k konto) {
	t.Helper()
	b, err := ts.backupHolen(k.handySitzung)
	if err != nil || string(b.Chiffrat) != "backup-handy" || b.Version != 1 {
		t.Errorf("Backup = %v, Fehler %v", b, err)
	}
	s, err := ts.sicherungHolen(k.handySitzung)
	if err != nil || string(s.Chiffrat) != "sicherung-handy" {
		t.Errorf("Sicherung = %v, Fehler %v", s, err)
	}
}

func TestAnmeldenMeldetFreischaltung(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	if !ts.anmelden(t, "google-kevin", handy).Freigeschaltet {
		t.Error("erstes Gerät eines neuen Kontos ist nicht freigeschaltet")
	}
	laptopSitzung := ts.anmelden(t, "google-kevin", laptop)
	if laptopSitzung.Freigeschaltet {
		t.Error("zweites Gerät ist ohne Zustimmung freigeschaltet")
	}
	liste, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, laptopSitzung.Sitzung.Token))
	if err != nil {
		t.Fatal(err)
	}
	frei := map[string]bool{}
	for _, g := range liste.Msg.Geraete {
		frei[g.GeraetId] = g.Freigeschaltet
	}
	if !frei[handy.ID()] || frei[laptop.ID()] {
		t.Errorf("Liste: freigeschaltet = %v, erwartet nur das Handy", frei)
	}
}

// KRITISCH: Ein Gerät, das nur den Google-Login hat, ändert und löscht nichts.
// Jeder Fall prüft die Ablehnung und danach dieselbe Aktion als Erlaubnis,
// sobald das Handy den Laptop freigeschaltet hat.
func TestNichtFreigeschaltetesGeraetDarfNichtsAendern(t *testing.T) {
	faelle := []struct {
		name   string
		aktion func(t *testing.T, ts *testServer, k konto) error
	}{
		// Die Erlaubnisfälle prüfen auch die Wirkung: Ein Dienst, der nach der
		// Prüfung nichts tut, darf nicht als „erlaubt“ durchgehen.
		{"anderes Gerät sperren", func(_ *testing.T, ts *testServer, k konto) error {
			if err := ts.sperren(k.laptopSitzung, k.handy.ID()); err != nil {
				return err
			}
			_, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, k.handySitzung))
			if connect.CodeOf(err) != connect.CodeUnauthenticated {
				return fmt.Errorf("Handy nach dem Sperren: Code %v, erwartet unauthenticated", connect.CodeOf(err))
			}
			return nil
		}},
		// Ein drittes Gerät, damit die Erlaubnis eine Wirkung hat: Sich selbst
		// freizuschalten änderte für ein freigeschaltetes Gerät nichts.
		{"anderes Gerät freischalten", func(t *testing.T, ts *testServer, k konto) error {
			tablet := testhilfe.NeuesGeraet(t)
			ts.anmelden(t, "google-kevin", tablet)
			if err := ts.geraetFreischalten(k.laptopSitzung, tablet.ID()); err != nil {
				return err
			}
			if !ts.anmelden(t, "google-kevin", tablet).Freigeschaltet {
				return errors.New("Tablet ist danach nicht freigeschaltet")
			}
			return nil
		}},
		{"Backup ersetzen", func(_ *testing.T, ts *testServer, k konto) error {
			if _, err := ts.backupSpeichern(k.laptopSitzung, []byte("backup-laptop"), 1); err != nil {
				return err
			}
			if b, err := ts.backupHolen(k.handySitzung); err != nil || string(b.Chiffrat) != "backup-laptop" {
				return fmt.Errorf("Backup danach = %v, Fehler %w", b, err)
			}
			return nil
		}},
		{"Backup löschen", func(_ *testing.T, ts *testServer, k konto) error {
			if err := ts.backupLoeschen(k.laptopSitzung); err != nil {
				return err
			}
			if _, err := ts.backupHolen(k.handySitzung); connect.CodeOf(err) != connect.CodeNotFound {
				return fmt.Errorf("Backup nach dem Löschen: Code %v, erwartet not_found", connect.CodeOf(err))
			}
			return nil
		}},
		{"Sicherung speichern", func(_ *testing.T, ts *testServer, k konto) error {
			geholt, err := ts.sicherungHolen(k.laptopSitzung)
			if err != nil {
				return err
			}
			if _, err = ts.sicherungSpeichern(k.laptopSitzung, []byte("sicherung-laptop"), geholt.Version); err != nil {
				return err
			}
			if s, err := ts.sicherungHolen(k.handySitzung); err != nil || string(s.Chiffrat) != "sicherung-laptop" {
				return fmt.Errorf("Sicherung danach = %v, Fehler %w", s, err)
			}
			return nil
		}},
		// Diagnosen liest Claude; ein fremdes Gerät mit dem Google-Login soll
		// keinen Text dorthin bringen (Entscheidung nach Review-Lauf 8).
		{"Diagnose senden", func(t *testing.T, ts *testServer, k konto) error {
			ts.diagnoseKonten[k.kontoID] = true
			if err := ts.diagnoseSenden(k.laptopSitzung, gzipJSON(t, `{"ereignis":"test"}`)); err != nil {
				return err
			}
			if n := len(ts.diagnosen.Objekte()); n != 1 {
				return fmt.Errorf("%d Diagnosen abgelegt, erwartet 1", n)
			}
			return nil
		}},
		// Der Antrag kommt direkt in den Speicher; KontoLoeschen vom Laptop
		// löschte im Erlaubnisfall sofort.
		{"Löschung abbrechen", func(_ *testing.T, ts *testServer, k konto) error {
			if _, err := ts.speicher.LoeschungBeantragen(context.Background(), k.kontoID, ts.jetzt.Add(time.Hour)); err != nil {
				return err
			}
			if err := ts.loeschenAbbrechen(k.laptopSitzung); err != nil {
				return err
			}
			if ab, err := ts.speicher.LoeschenAb(context.Background(), k.kontoID); err != nil || !ab.IsZero() {
				return fmt.Errorf("Termin nach dem Abbrechen = %v, Fehler %w", ab, err)
			}
			return nil
		}},
		{"Sicherung löschen", func(_ *testing.T, ts *testServer, k konto) error {
			if err := ts.sicherungLoeschen(k.laptopSitzung); err != nil {
				return err
			}
			if _, err := ts.sicherungHolen(k.handySitzung); connect.CodeOf(err) != connect.CodeNotFound {
				return fmt.Errorf("Sicherung nach dem Löschen: Code %v, erwartet not_found", connect.CodeOf(err))
			}
			return nil
		}},
	}
	for _, fall := range faelle {
		t.Run("lehnt ab: "+fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			k := ts.kontoMitNeuemGeraet(t)
			erwarteCode(t, fall.aktion(t, ts, k), connect.CodePermissionDenied)
			ts.erwarteUnveraendert(t, k)
		})
		t.Run("erlaubt nach Freischaltung: "+fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			k := ts.kontoMitNeuemGeraet(t)
			ts.geraetFreischaltenOK(t, k.handySitzung, k.laptop.ID())
			if err := fall.aktion(t, ts, k); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Was ein nicht freigeschaltetes Gerät darf (geraete.proto): nur lesen, sich
// selbst abmelden und sich freischalten (und ein erstes Backup anlegen, siehe
// TestErstesBackupEinesNichtFreigeschaltetenGeraets).
func TestNichtFreigeschaltetesGeraetDarfLesenUndSichAbmelden(t *testing.T) {
	ts := neuerTestServer(t)
	k := ts.kontoMitNeuemGeraet(t)
	if k.laptopFreigeschaltet {
		t.Fatal("Laptop ist ohne Zustimmung freigeschaltet")
	}
	ctx := context.Background()

	t.Run("Geräte auflisten", func(t *testing.T) {
		if _, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, k.laptopSitzung)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Backup holen", func(t *testing.T) {
		if _, err := ts.backupHolen(k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Sicherung holen", func(t *testing.T) {
		if _, err := ts.sicherungHolen(k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Sitzung erneuern", func(t *testing.T) {
		if _, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, k.laptop, ts.jetzt))); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("sich selbst sperren", func(t *testing.T) {
		if err := ts.sperren(k.laptopSitzung, k.laptop.ID()); err != nil {
			t.Fatal(err)
		}
		ts.erwarteUnveraendert(t, k)
	})
}

// KRITISCH: Ein nicht freigeschaltetes Gerät legt das erste Backup nur an, wenn
// das Konto kein freigeschaltetes, ungesperrtes Gerät hat (Entscheidung nach
// Review-Lauf 8). Sonst brächte ein fremdes Gerät mit dem Google-Login seinen
// eigenen Freischaltschlüssel ins Konto, solange das Handy noch kein Backup
// hochgeladen hat, und schaltete sich damit selbst frei. Wird es angenommen,
// ist das Gerät danach freigeschaltet; eine veraltete Version meldet es vor
// dem Schutz (Review-Lauf 9).
func TestErstesBackupEinesNichtFreigeschaltetenGeraets(t *testing.T) {
	faelle := []struct {
		name    string
		vorher  func(t *testing.T, ts *testServer, handySitzung string, handy *testhilfe.Geraet)
		wantErr connect.Code // 0: erlaubt, das Backup liegt danach vor
	}{
		{"abgelehnt, solange das Konto ein freigeschaltetes Gerät hat",
			func(*testing.T, *testServer, string, *testhilfe.Geraet) {}, connect.CodePermissionDenied},
		{"erlaubt, wenn das einzige freigeschaltete Gerät gesperrt ist",
			func(t *testing.T, ts *testServer, handySitzung string, handy *testhilfe.Geraet) {
				if err := ts.sperren(handySitzung, handy.ID()); err != nil {
					t.Fatal(err)
				}
			}, 0},
		{"veraltete Version: meldet failed_precondition statt nicht freigeschaltet",
			func(t *testing.T, ts *testServer, handySitzung string, _ *testhilfe.Geraet) {
				if _, err := ts.backupSpeichern(handySitzung, []byte("backup-handy"), 0); err != nil {
					t.Fatal(err)
				}
			}, connect.CodeFailedPrecondition},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			handy := testhilfe.NeuesGeraet(t)
			handySitzung := ts.anmelden(t, "google-kevin", handy).Sitzung.Token
			laptop := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
			if laptop.Freigeschaltet {
				t.Fatal("Laptop ist ohne Zustimmung freigeschaltet")
			}
			fall.vorher(t, ts, handySitzung, handy)
			_, err := ts.backupSpeichern(laptop.Sitzung.Token, []byte("backup-laptop"), 0)
			if fall.wantErr == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				erwarteCode(t, err, fall.wantErr)
			}
			b, err := ts.backupHolen(laptop.Sitzung.Token)
			if fall.wantErr == 0 && (err != nil || string(b.Chiffrat) != "backup-laptop") {
				t.Errorf("Backup = %v, Fehler %v", b, err)
			}
			if fall.wantErr != 0 && err == nil && string(b.Chiffrat) == "backup-laptop" {
				t.Error("Backup des Laptops liegt trotz Ablehnung vor")
			}
			// Danach freigeschaltet heißt: Es darf, was nur freigeschaltete Geräte dürfen.
			darf := ts.sicherungLoeschen(laptop.Sitzung.Token)
			if fall.wantErr == 0 && darf != nil {
				t.Errorf("Laptop ist nach dem ersten Backup nicht freigeschaltet: %v", darf)
			}
			if fall.wantErr != 0 && connect.CodeOf(darf) != connect.CodePermissionDenied {
				t.Errorf("Laptop nach der Ablehnung: Code %v, erwartet permission_denied", connect.CodeOf(darf))
			}
		})
	}
}

func TestGeraetFreischalten(t *testing.T) {
	t.Run("wirkt sofort und ist wiederholbar", func(t *testing.T) {
		ts := neuerTestServer(t)
		k := ts.kontoMitNeuemGeraet(t)
		ts.geraetFreischaltenOK(t, k.handySitzung, k.laptop.ID())
		ts.geraetFreischaltenOK(t, k.handySitzung, k.laptop.ID())
		// dieselbe Sitzung, kein neues Anmelden
		if err := ts.sicherungLoeschen(k.laptopSitzung); err != nil {
			t.Fatal(err)
		}
		if !ts.anmelden(t, "google-kevin", k.laptop).Freigeschaltet {
			t.Error("Anmelden meldet den Laptop nicht als freigeschaltet")
		}
	})
	t.Run("lehnt ein Gerät eines anderen Kontos ab", func(t *testing.T) {
		ts := neuerTestServer(t)
		k := ts.kontoMitNeuemGeraet(t)
		ts.anmelden(t, "google-andere", testhilfe.NeuesGeraet(t))
		fremd := testhilfe.NeuesGeraet(t)
		fremdeSitzung := ts.anmelden(t, "google-andere", fremd).Sitzung.Token
		erwarteCode(t, ts.geraetFreischalten(k.handySitzung, fremd.ID()), connect.CodeNotFound)
		erwarteCode(t, ts.sicherungLoeschen(fremdeSitzung), connect.CodePermissionDenied)
	})
	t.Run("lehnt eine unbekannte Geräte-ID ab", func(t *testing.T) {
		ts := neuerTestServer(t)
		k := ts.kontoMitNeuemGeraet(t)
		erwarteCode(t, ts.geraetFreischalten(k.handySitzung, testhilfe.NeuesGeraet(t).ID()), connect.CodeNotFound)
		erwarteCode(t, ts.geraetFreischalten(k.handySitzung, "kein-format"), connect.CodeNotFound)
	})
	t.Run("lehnt ein gesperrtes Gerät ab", func(t *testing.T) {
		ts := neuerTestServer(t)
		k := ts.kontoMitNeuemGeraet(t)
		if err := ts.sperren(k.handySitzung, k.laptop.ID()); err != nil {
			t.Fatal(err)
		}
		erwarteCode(t, ts.geraetFreischalten(k.handySitzung, k.laptop.ID()), connect.CodeFailedPrecondition)
	})
}

// KRITISCH: Mit dem Wiederherstellungsschlüssel schaltet sich ein Gerät nur
// selbst frei, und nur mit einer Signatur über seine eigene Geräte-ID mit dem
// Schlüssel, der am Backup des eigenen Kontos liegt.
func TestMitWiederherstellungFreischalten(t *testing.T) {
	_, fremderSchluessel, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ablehnungen := []struct {
		name     string
		signatur func(k konto) []byte
		code     connect.Code
	}{
		{"Signatur für ein anderes Gerät", func(k konto) []byte { return signiereFreischaltung(fsPrivat, k.handy.ID()) }, connect.CodePermissionDenied},
		{"Signatur mit einem anderen Schlüssel", func(k konto) []byte { return signiereFreischaltung(fremderSchluessel, k.laptop.ID()) }, connect.CodePermissionDenied},
		{"leere Signatur", func(konto) []byte { return nil }, connect.CodePermissionDenied},
	}
	for _, fall := range ablehnungen {
		t.Run("lehnt ab: "+fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			k := ts.kontoMitNeuemGeraet(t)
			erwarteCode(t, ts.mitWiederherstellungFreischalten(k.laptopSitzung, fall.signatur(k)), fall.code)
			erwarteCode(t, ts.sicherungLoeschen(k.laptopSitzung), connect.CodePermissionDenied)
			ts.erwarteUnveraendert(t, k)
		})
	}

	t.Run("schaltet mit der richtigen Signatur frei", func(t *testing.T) {
		ts := neuerTestServer(t)
		k := ts.kontoMitNeuemGeraet(t)
		if err := ts.mitWiederherstellungFreischalten(k.laptopSitzung, signiereFreischaltung(fsPrivat, k.laptop.ID())); err != nil {
			t.Fatal(err)
		}
		if err := ts.sperren(k.laptopSitzung, k.handy.ID()); err != nil {
			t.Fatalf("Sperren des Handys nach Freischaltung: %v", err)
		}
	})

	t.Run("lehnt ab, wenn es kein Backup gibt", func(t *testing.T) {
		ts := neuerTestServer(t)
		ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
		laptop := testhilfe.NeuesGeraet(t)
		laptopSitzung := ts.anmelden(t, "google-kevin", laptop).Sitzung.Token
		err := ts.mitWiederherstellungFreischalten(laptopSitzung, signiereFreischaltung(fsPrivat, laptop.ID()))
		erwarteCode(t, err, connect.CodeFailedPrecondition)
		erwarteCode(t, ts.sicherungLoeschen(laptopSitzung), connect.CodePermissionDenied)
	})
}

// Die Prüfung direkt, ohne Sitzung: Über die API sind gesperrte Geräte und
// fremde Konten schon an der Sitzung draußen. nurFreigeschaltetesGeraet soll
// sie trotzdem selbst ablehnen, damit eine Lücke in der Sitzungsprüfung nicht
// zum Schreibrecht wird.
func TestNurFreigeschaltetesGeraet(t *testing.T) {
	ctx := context.Background()
	sp := speicher.NeuerArbeitsspeicher()
	anmelden := func(kennung, geraetID string) string {
		t.Helper()
		e, err := sp.Anmelden(ctx, kennung, speicher.Geraet{ID: geraetID, Erstellt: start}, speicher.NeueSitzung{Hash: "h-" + geraetID, LaeuftAb: start.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		return e.KontoID
	}
	kontoA := anmelden("kevin", "erstes")
	anmelden("kevin", "zweites") // nicht freigeschaltet: "erstes" ist es schon
	kontoB := anmelden("anna", "annas")
	if err := sp.GeraetSperren(ctx, kontoB, "annas", start); err != nil {
		t.Fatal(err)
	}
	d := freischaltPruefer{geraete: sp}

	faelle := []struct {
		name    string
		id      sitzung.Identitaet
		erlaubt bool
	}{
		{"freigeschaltetes Gerät darf", sitzung.Identitaet{KontoID: kontoA, GeraetID: "erstes"}, true},
		// KRITISCH: die Ablehnungen
		{"nicht freigeschaltetes Gerät darf nicht", sitzung.Identitaet{KontoID: kontoA, GeraetID: "zweites"}, false},
		{"gesperrtes freigeschaltetes Gerät darf nicht", sitzung.Identitaet{KontoID: kontoB, GeraetID: "annas"}, false},
		{"Gerät eines anderen Kontos darf nicht", sitzung.Identitaet{KontoID: kontoB, GeraetID: "erstes"}, false},
		{"unbekanntes Gerät darf nicht", sitzung.Identitaet{KontoID: kontoA, GeraetID: "gibt-es-nicht"}, false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			err := d.nurFreigeschaltetesGeraet(ctx, f.id)
			if f.erlaubt {
				if err != nil {
					t.Fatalf("erwartet erlaubt, bekam %v", err)
				}
				return
			}
			erwarteCode(t, err, connect.CodePermissionDenied)
		})
	}
}
