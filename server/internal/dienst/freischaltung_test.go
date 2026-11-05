package dienst

import (
	"bytes"
	"context"
	"crypto/ed25519"
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
}

func (ts *testServer) kontoMitNeuemGeraet(t *testing.T) konto {
	t.Helper()
	k := konto{handy: testhilfe.NeuesGeraet(t), laptop: testhilfe.NeuesGeraet(t)}
	k.handySitzung = ts.anmelden(t, "google-kevin", k.handy).Sitzung.Token
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
		aktion func(ts *testServer, k konto) error
	}{
		{"anderes Gerät sperren", func(ts *testServer, k konto) error {
			return ts.sperren(k.laptopSitzung, k.handy.ID())
		}},
		{"Gerät freischalten", func(ts *testServer, k konto) error {
			return ts.geraetFreischalten(k.laptopSitzung, k.laptop.ID())
		}},
		{"Backup ersetzen", func(ts *testServer, k konto) error {
			_, err := ts.backupSpeichern(k.laptopSitzung, []byte("backup-laptop"), 1)
			return err
		}},
		{"Backup löschen", func(ts *testServer, k konto) error {
			return ts.backupLoeschen(k.laptopSitzung)
		}},
		{"Sicherung speichern", func(ts *testServer, k konto) error {
			geholt, err := ts.sicherungHolen(k.laptopSitzung)
			if err != nil {
				return err
			}
			_, err = ts.sicherungSpeichern(k.laptopSitzung, []byte("sicherung-laptop"), geholt.Version)
			return err
		}},
		{"Sicherung löschen", func(ts *testServer, k konto) error {
			return ts.sicherungLoeschen(k.laptopSitzung)
		}},
	}
	for _, fall := range faelle {
		t.Run("lehnt ab: "+fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			k := ts.kontoMitNeuemGeraet(t)
			erwarteCode(t, fall.aktion(ts, k), connect.CodePermissionDenied)
			ts.erwarteUnveraendert(t, k)
		})
		t.Run("erlaubt nach Freischaltung: "+fall.name, func(t *testing.T) {
			ts := neuerTestServer(t)
			k := ts.kontoMitNeuemGeraet(t)
			ts.geraetFreischaltenOK(t, k.handySitzung, k.laptop.ID())
			if err := fall.aktion(ts, k); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Was ein nicht freigeschaltetes Gerät darf (geraete.proto): nur lesen, sich
// selbst abmelden und sich freischalten.
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

// Grenze aus schluessel.go: Gibt es noch kein Backup, darf jedes Gerät eins
// anlegen (E-B2-5) – auch ein nicht freigeschaltetes.
func TestNichtFreigeschaltetesGeraetDarfErstesBackupAnlegen(t *testing.T) {
	ts := neuerTestServer(t)
	handySitzung := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	laptop := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
	if laptop.Freigeschaltet {
		t.Fatal("Laptop ist ohne Zustimmung freigeschaltet")
	}
	if _, err := ts.backupSpeichern(laptop.Sitzung.Token, []byte("backup-laptop"), 0); err != nil {
		t.Fatal(err)
	}
	if b, err := ts.backupHolen(handySitzung); err != nil || string(b.Chiffrat) != "backup-laptop" {
		t.Errorf("Backup = %v, Fehler %v", b, err)
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
	d := &dienste{k: Konfiguration{Speicher: sp}}

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
