package speicher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

// Vertragstest: Dieselben Fälle laufen gegen beide Umsetzungen. So ist der
// Arbeitsspeicher, mit dem die Dienste getestet werden, garantiert gleichwertig
// zu Firestore.
//
// Firestore läuft gegen den lokalen Emulator:
//
//	gcloud emulators firestore start --host-port=localhost:8086
//	FIRESTORE_EMULATOR_HOST=localhost:8086 go test ./...
//
// Ohne Emulator werden die Firestore-Fälle übersprungen; mit EMULATOR_PFLICHT=1
// (in der CI) schlagen sie stattdessen fehl.

func TestArbeitsspeicherErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) Speicher { return NeuerArbeitsspeicher() })
}

func TestFirestoreErfuelltVertrag(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		if os.Getenv("EMULATOR_PFLICHT") != "" {
			t.Fatal("FIRESTORE_EMULATOR_HOST fehlt, Emulator ist aber Pflicht")
		}
		t.Skip("kein Firestore-Emulator (FIRESTORE_EMULATOR_HOST nicht gesetzt)")
	}
	pruefeVertrag(t, func(t *testing.T) Speicher {
		// Jeder Testfall bekommt ein eigenes (Emulator-)Projekt und ist damit
		// unabhängig von den anderen.
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		c, err := firestore.NewClient(context.Background(), "test-"+hex.EncodeToString(b))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return NeuerFirestore(c)
	})
}

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func geraet(id string, minuten int) Geraet {
	return Geraet{ID: id, Schluessel: []byte("schluessel-" + id), Erstellt: t0.Add(time.Duration(minuten) * time.Minute)}
}

// fsSchluessel steht für einen Freischaltschlüssel. Der Speicher prüft ihn
// nicht (das macht der Dienst), er legt ihn nur ab.
var fsSchluessel = []byte("freischaltschluessel")

func sitzung(hash string) NeueSitzung {
	return NeueSitzung{Hash: hash, LaeuftAb: t0.Add(time.Hour)}
}

func pruefeVertrag(t *testing.T, neu func(t *testing.T) Speicher) {
	ctx := context.Background()

	// muss bricht den Test bei einem Fehler ab (für Vorbereitungsschritte).
	muss := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	anmelden := func(t *testing.T, s Speicher, kennung, geraetID, hash string, minuten int) AnmeldeErgebnis {
		t.Helper()
		e, err := s.Anmelden(ctx, kennung, geraet(geraetID, minuten), sitzung(hash))
		muss(t, err)
		return e
	}

	t.Run("erstes Anmelden legt Konto, Gerät und Sitzung an", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		if !e.NeuesKonto || e.KontoID == "" {
			t.Fatalf("Ergebnis = %+v", e)
		}
		g, err := s.Geraet(ctx, "geraet-1")
		muss(t, err)
		if g.KontoID != e.KontoID || string(g.Schluessel) != "schluessel-geraet-1" || g.Gesperrt || !g.Erstellt.Equal(t0) {
			t.Errorf("Gerät = %+v", g)
		}
		si, err := s.Sitzung(ctx, "hash-1")
		muss(t, err)
		if si.KontoID != e.KontoID || si.GeraetID != "geraet-1" || !si.LaeuftAb.Equal(t0.Add(time.Hour)) {
			t.Errorf("Sitzung = %+v", si)
		}
	})

	t.Run("erneutes Anmelden mit demselben Gerät legt nichts doppelt an", func(t *testing.T) {
		s := neu(t)
		erstes := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		zweites := anmelden(t, s, "kennung-a", "geraet-1", "hash-2", 5)
		if zweites.NeuesKonto || zweites.KontoID != erstes.KontoID {
			t.Errorf("zweites Ergebnis = %+v, erstes = %+v", zweites, erstes)
		}
		liste, err := s.Geraete(ctx, erstes.KontoID)
		muss(t, err)
		if len(liste) != 1 || !liste[0].Erstellt.Equal(t0) {
			t.Errorf("Geräte = %+v, erwartet eins mit erster Erstellzeit", liste)
		}
	})

	t.Run("zweites Gerät desselben Google-Kontos landet im selben Konto", func(t *testing.T) {
		s := neu(t)
		erstes := anmelden(t, s, "kennung-a", "geraet-b", "hash-1", 0)
		zweites := anmelden(t, s, "kennung-a", "geraet-a", "hash-2", 1)
		if zweites.KontoID != erstes.KontoID || zweites.NeuesKonto {
			t.Fatalf("zweites Ergebnis = %+v", zweites)
		}
		liste, err := s.Geraete(ctx, erstes.KontoID)
		muss(t, err)
		if len(liste) != 2 || liste[0].ID != "geraet-b" || liste[1].ID != "geraet-a" {
			t.Errorf("Geräte = %+v, erwartet nach Erstellung sortiert", liste)
		}
	})

	t.Run("anderes Google-Konto bekommt ein anderes Konto", func(t *testing.T) {
		s := neu(t)
		a := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		b := anmelden(t, s, "kennung-b", "geraet-2", "hash-2", 0)
		if a.KontoID == b.KontoID || !b.NeuesKonto {
			t.Errorf("a = %+v, b = %+v", a, b)
		}
		liste, err := s.Geraete(ctx, a.KontoID)
		muss(t, err)
		if len(liste) != 1 || liste[0].ID != "geraet-1" {
			t.Errorf("Geräte von a = %+v, erwartet nur geraet-1", liste)
		}
	})

	t.Run("lehnt Geräteschlüssel eines anderen Kontos ab und legt nichts an", func(t *testing.T) {
		s := neu(t)
		anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		_, err := s.Anmelden(ctx, "kennung-b", geraet("geraet-1", 1), sitzung("hash-2"))
		if !errors.Is(err, ErrGeraetFremd) {
			t.Fatalf("Fehler = %v, erwartet ErrGeraetFremd", err)
		}
		if _, err := s.Sitzung(ctx, "hash-2"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Sitzung trotz Ablehnung angelegt: %v", err)
		}
		// Kennung b darf nicht halb angelegt worden sein.
		e := anmelden(t, s, "kennung-b", "geraet-2", "hash-3", 2)
		if !e.NeuesKonto {
			t.Error("Konto für kennung-b wurde trotz Ablehnung angelegt")
		}
	})

	t.Run("lehnt Anmelden eines gesperrten Geräts ab", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0))
		_, err := s.Anmelden(ctx, "kennung-a", geraet("geraet-1", 1), sitzung("hash-2"))
		if !errors.Is(err, ErrGeraetGesperrt) {
			t.Fatalf("Fehler = %v, erwartet ErrGeraetGesperrt", err)
		}
		if _, err := s.Sitzung(ctx, "hash-2"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Sitzung für gesperrtes Gerät angelegt: %v", err)
		}
	})

	t.Run("unbekanntes Gerät und unbekannte Sitzung melden nicht gefunden", func(t *testing.T) {
		s := neu(t)
		if _, err := s.Geraet(ctx, "gibt-es-nicht"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Geraet: %v", err)
		}
		if _, err := s.Sitzung(ctx, "gibt-es-nicht"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Sitzung: %v", err)
		}
		liste, err := s.Geraete(ctx, "gibt-es-nicht")
		muss(t, err)
		if len(liste) != 0 {
			t.Errorf("Geräte = %+v, erwartet keine", liste)
		}
	})

	t.Run("SitzungAnlegen stellt Sitzung für aktives Gerät aus", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		kontoID, err := s.SitzungAnlegen(ctx, "geraet-1", sitzung("hash-2"))
		muss(t, err)
		if kontoID != e.KontoID {
			t.Errorf("Konto = %q, erwartet %q", kontoID, e.KontoID)
		}
		si, err := s.Sitzung(ctx, "hash-2")
		muss(t, err)
		if si.KontoID != e.KontoID || si.GeraetID != "geraet-1" {
			t.Errorf("Sitzung = %+v", si)
		}
	})

	t.Run("SitzungAnlegen lehnt gesperrtes Gerät ab", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0))
		if _, err := s.SitzungAnlegen(ctx, "geraet-1", sitzung("hash-2")); !errors.Is(err, ErrGeraetGesperrt) {
			t.Fatalf("Fehler = %v, erwartet ErrGeraetGesperrt", err)
		}
		if _, err := s.Sitzung(ctx, "hash-2"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Sitzung für gesperrtes Gerät angelegt: %v", err)
		}
	})

	t.Run("SitzungAnlegen lehnt unbekanntes Gerät ab", func(t *testing.T) {
		s := neu(t)
		if _, err := s.SitzungAnlegen(ctx, "gibt-es-nicht", sitzung("hash-1")); !errors.Is(err, ErrNichtGefunden) {
			t.Fatalf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("Sperren markiert das Gerät und löscht nur dessen Sitzungen", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		anmelden(t, s, "kennung-a", "geraet-2", "hash-2", 1)
		_, err := s.SitzungAnlegen(ctx, "geraet-1", sitzung("hash-3"))
		muss(t, err)

		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0.Add(time.Minute)))

		g, err := s.Geraet(ctx, "geraet-1")
		muss(t, err)
		if !g.Gesperrt || !g.GesperrtAm.Equal(t0.Add(time.Minute)) {
			t.Errorf("Gerät = %+v, erwartet gesperrt", g)
		}
		for _, hash := range []string{"hash-1", "hash-3"} {
			if _, err := s.Sitzung(ctx, hash); !errors.Is(err, ErrNichtGefunden) {
				t.Errorf("Sitzung %s des gesperrten Geräts noch da: %v", hash, err)
			}
		}
		if _, err := s.Sitzung(ctx, "hash-2"); err != nil {
			t.Errorf("Sitzung des anderen Geräts gelöscht: %v", err)
		}
	})

	t.Run("lehnt Sperren eines fremden Geräts ab und ändert nichts", func(t *testing.T) {
		s := neu(t)
		anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		b := anmelden(t, s, "kennung-b", "geraet-2", "hash-2", 0)
		if err := s.GeraetSperren(ctx, b.KontoID, "geraet-1", t0); !errors.Is(err, ErrNichtGefunden) {
			t.Fatalf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
		g, err := s.Geraet(ctx, "geraet-1")
		muss(t, err)
		if g.Gesperrt {
			t.Error("fremdes Gerät wurde gesperrt")
		}
		if _, err := s.Sitzung(ctx, "hash-1"); err != nil {
			t.Errorf("Sitzung des fremden Geräts gelöscht: %v", err)
		}
	})

	t.Run("lehnt Sperren eines unbekannten Geräts ab", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		if err := s.GeraetSperren(ctx, e.KontoID, "gibt-es-nicht", t0); !errors.Is(err, ErrNichtGefunden) {
			t.Fatalf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("zweimal sperren behält den ersten Zeitpunkt", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0))
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0.Add(time.Hour)))
		g, err := s.Geraet(ctx, "geraet-1")
		muss(t, err)
		if !g.GesperrtAm.Equal(t0) {
			t.Errorf("GesperrtAm = %v, erwartet %v", g.GesperrtAm, t0)
		}
	})

	// KRITISCH: Auch mit mehr Sitzungen, als in einen Stapel passen, ist danach
	// keine mehr übrig (Gegenprobe: Sitzungen des anderen Geräts bleiben).
	t.Run("Sperren löscht auch sehr viele Sitzungen", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-1", "hash-1", 0)
		anmelden(t, s, "kennung-a", "geraet-2", "hash-2", 1)
		for i := range 450 {
			_, err := s.SitzungAnlegen(ctx, "geraet-1", sitzung(fmt.Sprintf("viele-%d", i)))
			muss(t, err)
		}
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0))
		// Alle prüfen, nicht nur Stichproben: Welche im ersten Stapel liegen, hängt
		// von der Sortierung in Firestore ab (nach Dokument-ID, also "viele-99" zuletzt).
		geloescht := []string{"hash-1"}
		for i := range 450 {
			geloescht = append(geloescht, fmt.Sprintf("viele-%d", i))
		}
		for _, hash := range geloescht {
			if _, err := s.Sitzung(ctx, hash); !errors.Is(err, ErrNichtGefunden) {
				t.Errorf("Sitzung %s des gesperrten Geräts noch da: %v", hash, err)
			}
		}
		if _, err := s.Sitzung(ctx, "hash-2"); err != nil {
			t.Errorf("Sitzung des anderen Geräts gelöscht: %v", err)
		}
	})

	// --- Freischaltung (E-B2b-13) -------------------------------------------

	// Jeder Fall: Vorgeschichte im Konto "kennung-a", dann meldet sich
	// "geraet-neu" an. KRITISCH: Freigeschaltet wird es nur, wenn es im Konto
	// nichts zu schützen gibt.
	freischaltFaelle := []struct {
		name   string
		vorher func(t *testing.T, s Speicher)
		frei   bool
	}{
		{"erstes Gerät eines neuen Kontos ist freigeschaltet", func(*testing.T, Speicher) {}, true},
		{"neues Gerät neben einem freigeschalteten ist es nicht", func(t *testing.T, s Speicher) {
			anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
		}, false},
		{"neues Gerät ist freigeschaltet, wenn das einzige freigeschaltete gesperrt ist und es kein Backup gibt", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
		}, true},
		{"neues Gerät ist es nicht, wenn es ein Backup mit Freischaltschlüssel gibt", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			_, err := s.BackupSpeichern(ctx, e.KontoID, []byte("backup"), fsSchluessel, 0, "", t0)
			muss(t, err)
			muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
		}, false},
		{"Backup ohne Freischaltschlüssel (vor E-B2b-13) schützt nichts", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			_, err := s.BackupSpeichern(ctx, e.KontoID, []byte("backup"), nil, 0, "", t0)
			muss(t, err)
			muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
		}, true},
		{"nach dem Löschen des Backups schützt sein Schlüssel nichts mehr", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			_, err := s.BackupSpeichern(ctx, e.KontoID, []byte("backup"), fsSchluessel, 0, "", t0)
			muss(t, err)
			muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
			muss(t, s.BackupLoeschen(ctx, e.KontoID))
		}, true},
		{"ein nicht freigeschaltetes Gerät schützt nichts", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			anmelden(t, s, "kennung-a", "geraet-zweit", "hash-zweit", 1) // nicht freigeschaltet
			muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
		}, true},
		{"Geräte und Backup eines anderen Kontos zählen nicht", func(t *testing.T, s Speicher) {
			e := anmelden(t, s, "kennung-b", "geraet-b", "hash-b", 0)
			_, err := s.BackupSpeichern(ctx, e.KontoID, []byte("backup"), fsSchluessel, 0, "", t0)
			muss(t, err)
		}, true},
	}
	for _, fall := range freischaltFaelle {
		t.Run(fall.name, func(t *testing.T) {
			s := neu(t)
			fall.vorher(t, s)
			e := anmelden(t, s, "kennung-a", "geraet-neu", "hash-neu", 10)
			if e.Freigeschaltet != fall.frei {
				t.Errorf("Ergebnis Freigeschaltet = %v, erwartet %v", e.Freigeschaltet, fall.frei)
			}
			g, err := s.Geraet(ctx, "geraet-neu")
			muss(t, err)
			if g.Freigeschaltet != fall.frei {
				t.Errorf("gespeichert Freigeschaltet = %v, erwartet %v", g.Freigeschaltet, fall.frei)
			}
		})
	}

	// KRITISCH: Höchstens zehn neue Geräte, die weder freigeschaltet noch
	// gesperrt sind. Jeder Fall legt neben dem freigeschalteten "geraet-alt"
	// offene Geräte an und meldet dann "geraet-neu" an.
	geraeteFaelle := []struct {
		name     string
		offen    int  // so viele offene Geräte vorher
		gesperrt int  // davon werden so viele gesperrt
		frei     int  // davon werden so viele freigeschaltet
		erlaubt  bool // darf sich geraet-neu anmelden?
	}{
		{"zehntes offenes Gerät darf sich anmelden", 9, 0, 0, true},
		{"elftes offenes Gerät wird abgelehnt", 10, 0, 0, false},
		{"gesperrte Geräte zählen nicht", 10, 1, 0, true},
		{"freigeschaltete Geräte zählen nicht", 10, 0, 1, true},
	}
	for _, fall := range geraeteFaelle {
		t.Run(fall.name, func(t *testing.T) {
			s := neu(t)
			e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0)
			for i := range fall.offen {
				anmelden(t, s, "kennung-a", fmt.Sprintf("offen-%d", i), fmt.Sprintf("hash-offen-%d", i), i+1)
			}
			for i := range fall.gesperrt {
				muss(t, s.GeraetSperren(ctx, e.KontoID, fmt.Sprintf("offen-%d", i), t0))
			}
			for i := range fall.frei {
				muss(t, s.GeraetFreischalten(ctx, e.KontoID, fmt.Sprintf("offen-%d", i)))
			}
			_, err := s.Anmelden(ctx, "kennung-a", geraet("geraet-neu", 30), sitzung("hash-neu"))
			if fall.erlaubt {
				muss(t, err)
				return
			}
			if !errors.Is(err, ErrZuVieleGeraete) {
				t.Fatalf("Fehler = %v, erwartet ErrZuVieleGeraete", err)
			}
			if _, err := s.Geraet(ctx, "geraet-neu"); !errors.Is(err, ErrNichtGefunden) {
				t.Errorf("abgelehntes Gerät wurde angelegt: %v", err)
			}
			if _, err := s.Sitzung(ctx, "hash-neu"); !errors.Is(err, ErrNichtGefunden) {
				t.Errorf("abgelehntes Gerät hat eine Sitzung: %v", err)
			}
			// Vorhandene Geräte melden sich weiter an, ein anderes Konto ebenso.
			anmelden(t, s, "kennung-a", "offen-0", "hash-wieder", 40)
			anmelden(t, s, "kennung-b", "geraet-b", "hash-b", 41)
		})
	}

	t.Run("vorhandenes Gerät behält beim erneuten Anmelden sein Merkmal", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-1", 0)
		anmelden(t, s, "kennung-a", "geraet-neu", "hash-2", 1)
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-alt", t0))
		// Jetzt gäbe es nichts mehr zu schützen – trotzdem bleibt geraet-neu,
		// wie es ist; nur ein neues Gerät wird nach der Regel eingestuft.
		if wieder := anmelden(t, s, "kennung-a", "geraet-neu", "hash-3", 2); wieder.Freigeschaltet {
			t.Error("nicht freigeschaltetes Gerät ist nach erneutem Anmelden freigeschaltet")
		}
		if wieder := anmelden(t, s, "kennung-c", "geraet-c", "hash-4", 0); !wieder.Freigeschaltet {
			t.Fatal("Vorbereitung: erstes Gerät nicht freigeschaltet")
		}
		anmelden(t, s, "kennung-c", "geraet-c2", "hash-5", 1)
		if wieder := anmelden(t, s, "kennung-c", "geraet-c", "hash-6", 2); !wieder.Freigeschaltet {
			t.Error("freigeschaltetes Gerät ist nach erneutem Anmelden nicht mehr freigeschaltet")
		}
	})

	t.Run("GeraetFreischalten schaltet das Gerät frei, wiederholbar", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-1", 0)
		anmelden(t, s, "kennung-a", "geraet-neu", "hash-2", 1)
		muss(t, s.GeraetFreischalten(ctx, e.KontoID, "geraet-neu"))
		muss(t, s.GeraetFreischalten(ctx, e.KontoID, "geraet-neu"))
		liste, err := s.Geraete(ctx, e.KontoID)
		muss(t, err)
		if len(liste) != 2 || !liste[0].Freigeschaltet || !liste[1].Freigeschaltet {
			t.Errorf("Geräte = %+v, erwartet beide freigeschaltet", liste)
		}
		if wieder := anmelden(t, s, "kennung-a", "geraet-neu", "hash-3", 2); !wieder.Freigeschaltet {
			t.Error("Anmelden meldet das freigeschaltete Gerät als nicht freigeschaltet")
		}
	})

	t.Run("lehnt Freischalten eines fremden Geräts ab und ändert nichts", func(t *testing.T) {
		s := neu(t)
		a := anmelden(t, s, "kennung-a", "geraet-a", "hash-a", 0)
		anmelden(t, s, "kennung-b", "geraet-b", "hash-b", 0)
		anmelden(t, s, "kennung-b", "geraet-b2", "hash-b2", 1) // nicht freigeschaltet
		if err := s.GeraetFreischalten(ctx, a.KontoID, "geraet-b2"); !errors.Is(err, ErrNichtGefunden) {
			t.Fatalf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
		g, err := s.Geraet(ctx, "geraet-b2")
		muss(t, err)
		if g.Freigeschaltet {
			t.Error("fremdes Gerät wurde freigeschaltet")
		}
	})

	t.Run("lehnt Freischalten eines unbekannten Geräts ab", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-a", "hash-a", 0)
		if err := s.GeraetFreischalten(ctx, e.KontoID, "gibt-es-nicht"); !errors.Is(err, ErrNichtGefunden) {
			t.Fatalf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("lehnt Freischalten eines gesperrten Geräts ab und ändert nichts", func(t *testing.T) {
		s := neu(t)
		e := anmelden(t, s, "kennung-a", "geraet-alt", "hash-1", 0)
		anmelden(t, s, "kennung-a", "geraet-neu", "hash-2", 1)
		muss(t, s.GeraetSperren(ctx, e.KontoID, "geraet-neu", t0))
		if err := s.GeraetFreischalten(ctx, e.KontoID, "geraet-neu"); !errors.Is(err, ErrGeraetGesperrt) {
			t.Fatalf("Fehler = %v, erwartet ErrGeraetGesperrt", err)
		}
		g, err := s.Geraet(ctx, "geraet-neu")
		muss(t, err)
		if g.Freigeschaltet || !g.Gesperrt {
			t.Errorf("Gerät = %+v, erwartet gesperrt und nicht freigeschaltet", g)
		}
	})

	// --- Backup (B2) ---------------------------------------------------------

	backupPruefen := func(t *testing.T, s Speicher, kontoID, chiffrat string, version int64) {
		t.Helper()
		b, err := s.BackupHolen(ctx, kontoID)
		muss(t, err)
		if string(b.Chiffrat) != chiffrat || b.Version != version {
			t.Errorf("Backup = %q Version %d, erwartet %q Version %d", b.Chiffrat, b.Version, chiffrat, version)
		}
	}

	t.Run("erstes Backup bekommt Version 1", func(t *testing.T) {
		s := neu(t)
		v, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-1"), fsSchluessel, 0, "", t0)
		muss(t, err)
		if v != 1 {
			t.Errorf("Version = %d, erwartet 1", v)
		}
		backupPruefen(t, s, "konto-a", "backup-1", 1)
		b, _ := s.BackupHolen(ctx, "konto-a")
		if !b.Geaendert.Equal(t0) {
			t.Errorf("Geändert = %v, erwartet %v", b.Geaendert, t0)
		}
		if string(b.Freischaltschluessel) != string(fsSchluessel) {
			t.Errorf("Freischaltschlüssel = %q, erwartet %q", b.Freischaltschluessel, fsSchluessel)
		}
	})

	t.Run("Backup mit passender Version ersetzt das alte", func(t *testing.T) {
		s := neu(t)
		_, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-1"), fsSchluessel, 0, "", t0)
		muss(t, err)
		v, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-2"), []byte("neuer-schluessel"), 1, "", t0.Add(time.Minute))
		muss(t, err)
		if v != 2 {
			t.Errorf("Version = %d, erwartet 2", v)
		}
		backupPruefen(t, s, "konto-a", "backup-2", 2)
		if b, _ := s.BackupHolen(ctx, "konto-a"); string(b.Freischaltschluessel) != "neuer-schluessel" {
			t.Errorf("Freischaltschlüssel = %q, erwartet den neuen", b.Freischaltschluessel)
		}
	})

	// KRITISCH: Zu jeder Erlaubnis oben die Ablehnung: Mit falscher Version
	// wird nichts geschrieben.
	versionsFaelle := []struct {
		name      string
		vorher    int // so viele Backups vorher gespeichert (0 = keins)
		erwartet  int64
		verbleibt string // Chiffrat, das danach gespeichert sein muss ("" = keins)
	}{
		{"lehnt erstes Backup mit erwarteter Version 1 ab", 0, 1, ""},
		{"lehnt Version 0 ab, wenn es schon ein Backup gibt", 1, 0, "backup-1"},
		{"lehnt veraltete Version ab", 2, 1, "backup-2"},
		{"lehnt Version aus der Zukunft ab", 1, 2, "backup-1"},
	}
	for _, fall := range versionsFaelle {
		t.Run(fall.name, func(t *testing.T) {
			s := neu(t)
			for i := range fall.vorher {
				_, err := s.BackupSpeichern(ctx, "konto-a", []byte(fmt.Sprintf("backup-%d", i+1)), fsSchluessel, int64(i), "", t0)
				muss(t, err)
			}
			_, err := s.BackupSpeichern(ctx, "konto-a", []byte("fremd"), fsSchluessel, fall.erwartet, "", t0)
			if !errors.Is(err, ErrVersionVeraltet) {
				t.Fatalf("Fehler = %v, erwartet ErrVersionVeraltet", err)
			}
			if fall.verbleibt == "" {
				if _, err := s.BackupHolen(ctx, "konto-a"); !errors.Is(err, ErrNichtGefunden) {
					t.Errorf("Holen: Fehler = %v, erwartet ErrNichtGefunden", err)
				}
				return
			}
			backupPruefen(t, s, "konto-a", fall.verbleibt, int64(fall.vorher))
		})
	}

	t.Run("Holen ohne Backup meldet nicht gefunden", func(t *testing.T) {
		s := neu(t)
		if _, err := s.BackupHolen(ctx, "konto-a"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("nach dem Löschen ist das Backup weg, ein neues beginnt mit Version 1", func(t *testing.T) {
		s := neu(t)
		_, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-1"), fsSchluessel, 0, "", t0)
		muss(t, err)
		_, err = s.BackupSpeichern(ctx, "konto-b", []byte("backup-b"), fsSchluessel, 0, "", t0)
		muss(t, err)
		muss(t, s.BackupLoeschen(ctx, "konto-a"))
		if _, err := s.BackupHolen(ctx, "konto-a"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Holen nach dem Löschen: Fehler = %v, erwartet ErrNichtGefunden", err)
		}
		// KRITISCH: Gelöscht wird nur das Backup dieses Kontos.
		backupPruefen(t, s, "konto-b", "backup-b", 1)
		v, err := s.BackupSpeichern(ctx, "konto-a", []byte("neu"), fsSchluessel, 0, "", t0)
		muss(t, err)
		if v != 1 {
			t.Errorf("Version = %d, erwartet 1", v)
		}
	})

	t.Run("Löschen ohne Backup ist kein Fehler", func(t *testing.T) {
		muss(t, neu(t).BackupLoeschen(ctx, "konto-a"))
	})

	t.Run("Backups verschiedener Konten sind getrennt", func(t *testing.T) {
		s := neu(t)
		_, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-a"), fsSchluessel, 0, "", t0)
		muss(t, err)
		_, err = s.BackupSpeichern(ctx, "konto-b", []byte("backup-b"), fsSchluessel, 0, "", t0)
		muss(t, err)
		backupPruefen(t, s, "konto-a", "backup-a", 1)
		backupPruefen(t, s, "konto-b", "backup-b", 1)
		if _, err := s.BackupHolen(ctx, "konto-c"); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Konto ohne Backup: Fehler = %v", err)
		}
	})

	// KRITISCH: Ein nicht freigeschaltetes Gerät (ungeschuetztFuer) legt das
	// erste Backup nur an, wenn das Konto kein freigeschaltetes, ungesperrtes
	// Gerät hat, und ist danach freigeschaltet. Die Version wird vorher
	// geprüft. Jeder Fall bereitet Konto "kennung-a" mit dem nicht
	// freigeschalteten Gerät "geraet-neu" vor.
	ungeschuetztFaelle := []struct {
		name    string
		vorher  func(t *testing.T, s Speicher, kontoA string) // kontoA hat geraet-alt (frei) und geraet-neu
		geraet  string                                        // wer speichert
		wantErr error                                         // nil: erlaubt
	}{
		{"abgelehnt, wenn es ein freigeschaltetes Gerät gibt",
			func(*testing.T, Speicher, string) {}, "geraet-neu", ErrKontoGeschuetzt},
		{"erlaubt, wenn das einzige freigeschaltete Gerät gesperrt ist",
			func(t *testing.T, s Speicher, kontoA string) {
				muss(t, s.GeraetSperren(ctx, kontoA, "geraet-alt", t0))
			}, "geraet-neu", nil},
		{"erlaubt, Geräte eines anderen Kontos zählen nicht",
			func(t *testing.T, s Speicher, kontoA string) {
				muss(t, s.GeraetSperren(ctx, kontoA, "geraet-alt", t0))
				anmelden(t, s, "kennung-b", "geraet-b", "hash-b", 2)
			}, "geraet-neu", nil},
		{"abgelehnt für ein Gerät eines anderen Kontos",
			func(t *testing.T, s Speicher, kontoA string) {
				muss(t, s.GeraetSperren(ctx, kontoA, "geraet-alt", t0))
				anmelden(t, s, "kennung-b", "geraet-b", "hash-b", 2)
			}, "geraet-b", ErrNichtGefunden},
		{"abgelehnt für ein gesperrtes Gerät, auch ohne freigeschaltetes",
			func(t *testing.T, s Speicher, kontoA string) {
				muss(t, s.GeraetSperren(ctx, kontoA, "geraet-alt", t0))
				muss(t, s.GeraetSperren(ctx, kontoA, "geraet-neu", t0))
			}, "geraet-neu", ErrGeraetGesperrt},
	}
	for _, fall := range ungeschuetztFaelle {
		t.Run("erstes Backup eines nicht freigeschalteten Geräts: "+fall.name, func(t *testing.T) {
			s := neu(t)
			kontoA := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0).KontoID
			anmelden(t, s, "kennung-a", "geraet-neu", "hash-neu", 1)
			fall.vorher(t, s, kontoA)
			v, err := s.BackupSpeichern(ctx, kontoA, []byte("backup-neu"), fsSchluessel, 0, fall.geraet, t0)
			g, gErr := s.Geraet(ctx, fall.geraet)
			muss(t, gErr)
			if fall.wantErr == nil {
				muss(t, err)
				backupPruefen(t, s, kontoA, "backup-neu", v)
				if !g.Freigeschaltet {
					t.Error("Gerät ist nach dem ersten Backup nicht freigeschaltet")
				}
				return
			}
			if !errors.Is(err, fall.wantErr) {
				t.Fatalf("Fehler = %v, erwartet %v", err, fall.wantErr)
			}
			if _, err := s.BackupHolen(ctx, kontoA); !errors.Is(err, ErrNichtGefunden) {
				t.Errorf("Backup wurde trotzdem angelegt: %v", err)
			}
			if fall.geraet == "geraet-neu" && g.Freigeschaltet {
				t.Error("abgelehntes Gerät ist freigeschaltet")
			}
			// Gegenprobe: Ohne die Einschränkung (freigeschaltetes Gerät) geht es.
			_, err = s.BackupSpeichern(ctx, kontoA, []byte("backup-alt"), fsSchluessel, 0, "", t0)
			muss(t, err)
		})
	}

	// KRITISCH: Erst die Version, dann der Schutz. Ein nicht freigeschaltetes
	// Gerät mit veralteter Version erfährt, dass es ein Backup gibt (und holt
	// es), statt nur „nicht freigeschaltet“ zu hören (Review-Lauf 9).
	t.Run("veraltete Version eines nicht freigeschalteten Geräts meldet ErrVersionVeraltet", func(t *testing.T) {
		s := neu(t)
		kontoA := anmelden(t, s, "kennung-a", "geraet-alt", "hash-alt", 0).KontoID
		anmelden(t, s, "kennung-a", "geraet-neu", "hash-neu", 1)
		_, err := s.BackupSpeichern(ctx, kontoA, []byte("backup-alt"), fsSchluessel, 0, "", t0)
		muss(t, err)
		_, err = s.BackupSpeichern(ctx, kontoA, []byte("backup-neu"), fsSchluessel, 0, "geraet-neu", t0)
		if !errors.Is(err, ErrVersionVeraltet) {
			t.Fatalf("Fehler = %v, erwartet ErrVersionVeraltet", err)
		}
		backupPruefen(t, s, kontoA, "backup-alt", 1)
	})

	// Gleichzeitigkeit: Die folgenden Fälle starten mehrere Aufrufe zur selben
	// Zeit (Goroutinen = leichtgewichtige Threads). Ein Rennen tritt nicht in
	// jedem Durchlauf auf; deshalb mehrere Runden mit je frischem Speicher.
	// Fehler einzelner Aufrufe (etwa zu viele Konflikte in Firestore) sind
	// erlaubt – geprüft wird, was danach im Speicher steht. Damit der Test nicht
	// nur deshalb grün ist, weil jeder Aufruf scheitert, muss über alle Runden
	// mindestens einer gelingen.
	// Gegen den Speicher im Arbeitsspeicher beweisen diese Fälle wenig: Er hält
	// eine Sperre über den ganzen Aufruf. Aussagekräftig sind sie gegen Firestore.

	// KRITISCH: Gesperrt heißt sofort draußen. Egal wie Sperren und neue
	// Sitzungen sich überschneiden: Danach darf keine Sitzung des Geräts übrig
	// sein. Gegenprobe zu "SitzungAnlegen prüft die Sperre in der Transaktion".
	sperrRennen := []struct {
		name   string
		aufruf func(s Speicher, hash string) error // legt eine Sitzung für geraet-1 an
	}{
		{"Erneuern", func(s Speicher, hash string) error {
			_, err := s.SitzungAnlegen(ctx, "geraet-1", sitzung(hash))
			return err
		}},
		{"Anmelden", func(s Speicher, hash string) error {
			_, err := s.Anmelden(ctx, "kennung-a", geraet("geraet-1", 0), sitzung(hash))
			return err
		}},
	}
	for _, rennen := range sperrRennen {
		t.Run("Sperren gewinnt gegen gleichzeitiges "+rennen.name, func(t *testing.T) {
			var erfolge atomic.Int32 // atomar: Goroutinen zählen gleichzeitig hoch
			for runde := range gleichzeitigRunden {
				s := neu(t)
				e := anmelden(t, s, "kennung-a", "geraet-1", "hash-0", 0)

				var wg sync.WaitGroup
				var sperrFehler error
				hashes := []string{"hash-0"}
				for i := range gleichzeitigAufrufe {
					hash := fmt.Sprintf("hash-%d-%d", runde, i+1)
					hashes = append(hashes, hash)
					wg.Go(func() {
						switch err := rennen.aufruf(s, hash); {
						case err == nil:
							erfolge.Add(1)
						case !errors.Is(err, ErrGeraetGesperrt):
							t.Logf("Runde %d, %s: %v", runde, hash, err)
						}
					})
					if i == gleichzeitigAufrufe/2 { // Sperren mitten zwischen den anderen starten
						wg.Go(func() { sperrFehler = s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0) })
					}
				}
				wg.Wait()

				muss(t, sperrFehler)
				for _, hash := range hashes {
					if _, err := s.Sitzung(ctx, hash); !errors.Is(err, ErrNichtGefunden) {
						t.Fatalf("Runde %d: Sitzung %s des gesperrten Geräts ist noch da (Fehler: %v)", runde, hash, err)
					}
				}
			}
			if erfolge.Load() == 0 {
				t.Fatal("kein einziger Aufruf gelungen – das Rennen hat nie stattgefunden")
			}
		})
	}

	// KRITISCH: Freischalten und Sperren überschneiden sich (E18, Review-Lauf 8):
	// Danach ist das Gerät gesperrt, egal in welcher Reihenfolge. Ein
	// Freischalten, das den alten Stand zurückschreibt, höbe die Sperre auf.
	// Die zweite Hälfte der Aufrufe startet sicher erst nach dem Sperren, damit
	// auch der Arbeitsspeicher (eine Sperre über alles) den Fall sieht.
	t.Run("Sperren gewinnt gegen gleichzeitiges Freischalten", func(t *testing.T) {
		var erfolge atomic.Int32
		for runde := range gleichzeitigRunden {
			s := neu(t)
			e := anmelden(t, s, "kennung-a", "geraet-1", "hash-0", 0)
			var wg sync.WaitGroup
			var sperrFehler error
			gesperrt := make(chan struct{})
			for i := range gleichzeitigAufrufe {
				wg.Go(func() {
					if i > gleichzeitigAufrufe/2 {
						<-gesperrt
					}
					switch err := s.GeraetFreischalten(ctx, e.KontoID, "geraet-1"); {
					case err == nil:
						erfolge.Add(1)
					case !errors.Is(err, ErrGeraetGesperrt):
						t.Logf("Runde %d, Aufruf %d: %v", runde, i, err)
					}
				})
				if i == gleichzeitigAufrufe/2 {
					wg.Go(func() {
						defer close(gesperrt)
						sperrFehler = s.GeraetSperren(ctx, e.KontoID, "geraet-1", t0)
					})
				}
			}
			wg.Wait()

			muss(t, sperrFehler)
			g, err := s.Geraet(ctx, "geraet-1")
			muss(t, err)
			if !g.Gesperrt {
				t.Fatalf("Runde %d: Gerät nach Sperren und Freischalten nicht gesperrt", runde)
			}
		}
		if erfolge.Load() == 0 {
			t.Fatal("kein einziges Freischalten gelungen – das Rennen hat nie stattgefunden")
		}
	})

	// KRITISCH: Meldet sich ein Google-Konto auf mehreren Geräten zugleich zum
	// ersten Mal an, entsteht genau ein Konto. Sonst wären die Geräte getrennt
	// und das zweite Konto verwaist.
	t.Run("gleichzeitiges erstes Anmelden legt genau ein Konto an", func(t *testing.T) {
		for runde := range gleichzeitigRunden {
			s := neu(t)
			ergebnisse := make([]AnmeldeErgebnis, gleichzeitigAufrufe)
			fehler := make([]error, gleichzeitigAufrufe)
			var wg sync.WaitGroup
			for i := range gleichzeitigAufrufe {
				wg.Go(func() {
					id := fmt.Sprintf("geraet-%d", i)
					ergebnisse[i], fehler[i] = s.Anmelden(ctx, "kennung-a", geraet(id, i), sitzung("hash-"+id))
				})
			}
			wg.Wait()

			konten := map[string]bool{}
			neue := 0
			for i, e := range ergebnisse {
				if fehler[i] != nil {
					t.Logf("Runde %d, Aufruf %d: %v", runde, i, fehler[i])
					continue
				}
				konten[e.KontoID] = true
				if e.NeuesKonto {
					neue++
				}
			}
			if len(konten) != 1 || neue != 1 {
				t.Fatalf("Runde %d: %d Konten, %d davon als neu gemeldet – erwartet genau 1 und 1", runde, len(konten), neue)
			}
			// Auch im Speicher: alle erfolgreich angemeldeten Geräte im selben Konto.
			var kontoID string
			for k := range konten {
				kontoID = k
			}
			liste, err := s.Geraete(ctx, kontoID)
			muss(t, err)
			erfolgreich := 0
			for _, f := range fehler {
				if f == nil {
					erfolgreich++
				}
			}
			if len(liste) != erfolgreich {
				t.Errorf("Runde %d: %d Geräte im Konto, erwartet %d", runde, len(liste), erfolgreich)
			}
		}
	})
	// KRITISCH: Speichern mehrere Geräte gleichzeitig mit derselben erwarteten
	// Version, gewinnt genau eins. Die anderen bekommen ErrVersionVeraltet (oder
	// einen anderen Fehler), und gespeichert ist das Backup des Gewinners.
	t.Run("gleichzeitiges Speichern derselben Version: genau einer gewinnt", func(t *testing.T) {
		for runde := range gleichzeitigRunden {
			s := neu(t)
			_, err := s.BackupSpeichern(ctx, "konto-a", []byte("backup-0"), fsSchluessel, 0, "", t0)
			muss(t, err)

			fehler := make([]error, gleichzeitigAufrufe)
			var wg sync.WaitGroup
			for i := range gleichzeitigAufrufe {
				wg.Go(func() {
					_, fehler[i] = s.BackupSpeichern(ctx, "konto-a", []byte(fmt.Sprintf("geraet-%d", i)), fsSchluessel, 1, "", t0)
				})
			}
			wg.Wait()

			gewinner := -1
			for i, f := range fehler {
				switch {
				case f == nil:
					if gewinner >= 0 {
						t.Fatalf("Runde %d: Aufruf %d und %d haben beide gespeichert", runde, gewinner, i)
					}
					gewinner = i
				case !errors.Is(f, ErrVersionVeraltet):
					t.Logf("Runde %d, Aufruf %d: %v", runde, i, f)
				}
			}
			if gewinner < 0 {
				t.Fatalf("Runde %d: kein Aufruf gelungen – das Rennen hat nicht stattgefunden", runde)
			}
			backupPruefen(t, s, "konto-a", fmt.Sprintf("geraet-%d", gewinner), 2)
		}
	})
}

// Umfang der Gleichzeitigkeitsfälle: genug, dass Rennen gegen den Emulator
// zuverlässig auftreten, und wenig genug für ca. 35 s Laufzeit.
const (
	gleichzeitigRunden  = 5
	gleichzeitigAufrufe = 8
)
