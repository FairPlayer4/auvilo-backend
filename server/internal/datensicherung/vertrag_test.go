package datensicherung

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// Vertragstest: Dieselben Fälle laufen gegen beide Umsetzungen. So verhält sich
// der Arbeitsspeicher, mit dem die Dienste getestet werden, wie Cloud Storage.
//
// Cloud Storage läuft gegen den Nachbau fake-gcs-server (Docker):
//
//	docker run -d --rm --name fakegcs -p 4443:4443 fsouza/fake-gcs-server:1.56.1 -scheme http -port 4443 -backend memory
//	STORAGE_EMULATOR_HOST=localhost:4443 go test ./...
//
// Ohne Nachbau werden die Cloud-Storage-Fälle übersprungen; mit
// EMULATOR_PFLICHT=1 (in der CI) schlagen sie stattdessen fehl.

func TestArbeitsspeicherErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) Ablage { return NeuerArbeitsspeicher() })
}

func TestCloudStorageErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) Ablage {
		c, bucket := testBucket(t)
		return NeuesCloudStorage(c, bucket)
	})
}

// testBucket legt im Nachbau einen eigenen Bucket mit Versionierung an, damit
// jeder Testfall unabhängig von den anderen ist.
func testBucket(t *testing.T) (*storage.Client, string) {
	t.Helper()
	if os.Getenv("STORAGE_EMULATOR_HOST") == "" {
		if os.Getenv("EMULATOR_PFLICHT") != "" {
			t.Fatal("STORAGE_EMULATOR_HOST fehlt, Nachbau ist aber Pflicht")
		}
		t.Skip("kein fake-gcs-server (STORAGE_EMULATOR_HOST nicht gesetzt)")
	}
	ctx := context.Background()
	c, err := NeuerClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "test-" + hex.EncodeToString(b)
	if err := c.Bucket(name).Create(ctx, "test", &storage.BucketAttrs{VersioningEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return c, name
}

func speichere(t *testing.T, a Ablage, konto, inhalt string, erwartet int64) int64 {
	t.Helper()
	v, err := a.Speichern(context.Background(), konto, []byte(inhalt), erwartet)
	if err != nil {
		t.Fatalf("Speichern %q: %v", inhalt, err)
	}
	return v
}

func erwarteFassung(t *testing.T, a Ablage, konto, inhalt string, version int64) {
	t.Helper()
	f, err := a.Holen(context.Background(), konto, 0)
	if err != nil {
		t.Fatalf("Holen: %v", err)
	}
	if !bytes.Equal(f.Chiffrat, []byte(inhalt)) || f.Version != version {
		t.Errorf("Holen = %q Version %d, erwartet %q Version %d", f.Chiffrat, f.Version, inhalt, version)
	}
}

func erwarteVersion(t *testing.T, a Ablage, konto string, version int64, inhalt string) {
	t.Helper()
	f, err := a.Holen(context.Background(), konto, version)
	if err != nil {
		t.Fatalf("Holen Version %d: %v", version, err)
	}
	if !bytes.Equal(f.Chiffrat, []byte(inhalt)) || f.Version != version {
		t.Errorf("Holen Version %d = %q Version %d, erwartet %q", version, f.Chiffrat, f.Version, inhalt)
	}
}

func fassungen(t *testing.T, a Ablage, konto string) []FassungsInfo {
	t.Helper()
	liste, err := a.Fassungen(context.Background(), konto)
	if err != nil {
		t.Fatalf("Fassungen: %v", err)
	}
	return liste
}

func pruefeVertrag(t *testing.T, neu func(t *testing.T) Ablage) {
	ctx := context.Background()

	t.Run("ohne Sicherung meldet Holen nicht gefunden", func(t *testing.T) {
		_, err := neu(t).Holen(ctx, "konto-1", 0)
		if !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Fehler = %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("erste Sicherung mit Version 0, Holen liefert sie", func(t *testing.T) {
		a := neu(t)
		v := speichere(t, a, "konto-1", "erste", 0)
		if v <= 0 {
			t.Errorf("Version = %d, erwartet > 0", v)
		}
		erwarteFassung(t, a, "konto-1", "erste", v)
	})

	t.Run("ersetzen mit der gesehenen Version liefert eine größere Version", func(t *testing.T) {
		a := neu(t)
		v1 := speichere(t, a, "konto-1", "erste", 0)
		v2 := speichere(t, a, "konto-1", "zweite", v1)
		if v2 <= v1 {
			t.Errorf("neue Version %d nicht größer als %d", v2, v1)
		}
		erwarteFassung(t, a, "konto-1", "zweite", v2)
	})

	// KRITISCH: Ein Gerät mit veraltetem Stand überschreibt nichts.
	t.Run("lehnt falsche Version ab und lässt die Sicherung unverändert", func(t *testing.T) {
		faelle := []struct {
			name     string
			erwartet func(v1, v2 int64) int64
		}{
			{"0, obwohl es eine gibt", func(_, _ int64) int64 { return 0 }},
			{"die vorige Version", func(v1, _ int64) int64 { return v1 }},
			{"eine erfundene Version", func(_, v2 int64) int64 { return v2 + 1 }},
		}
		for _, fall := range faelle {
			t.Run(fall.name, func(t *testing.T) {
				a := neu(t)
				v1 := speichere(t, a, "konto-1", "erste", 0)
				v2 := speichere(t, a, "konto-1", "zweite", v1)
				_, err := a.Speichern(ctx, "konto-1", []byte("veraltet"), fall.erwartet(v1, v2))
				if !errors.Is(err, ErrVersionVeraltet) {
					t.Errorf("Fehler = %v, erwartet ErrVersionVeraltet", err)
				}
				erwarteFassung(t, a, "konto-1", "zweite", v2)
			})
		}
	})

	t.Run("lehnt eine Version ab, wenn es noch keine Sicherung gibt", func(t *testing.T) {
		a := neu(t)
		_, err := a.Speichern(ctx, "konto-1", []byte("erste"), 5)
		if !errors.Is(err, ErrVersionVeraltet) {
			t.Errorf("Fehler = %v, erwartet ErrVersionVeraltet", err)
		}
	})

	// KRITISCH: Speichern zwei Geräte gleichzeitig mit derselben Version, gewinnt
	// genau eins; das andere erfährt es.
	t.Run("bei gleichzeitigem Speichern gewinnt genau ein Gerät", func(t *testing.T) {
		a := neu(t)
		v1 := speichere(t, a, "konto-1", "erste", 0)
		const geraete = 5
		var wg sync.WaitGroup
		fehler := make([]error, geraete)
		for i := range geraete {
			wg.Go(func() {
				_, fehler[i] = a.Speichern(ctx, "konto-1", []byte(fmt.Sprintf("geraet-%d", i)), v1)
			})
		}
		wg.Wait()
		erfolgreich := 0
		for _, err := range fehler {
			switch {
			case err == nil:
				erfolgreich++
			case !errors.Is(err, ErrVersionVeraltet):
				t.Errorf("unerwarteter Fehler: %v", err)
			}
		}
		if erfolgreich != 1 {
			t.Errorf("%d Geräte erfolgreich, erwartet genau 1", erfolgreich)
		}
	})

	// KRITISCH: Jedes Konto hat seine eigene Sicherung. "konto-1" und "konto-10"
	// prüfen nebenbei, dass ein Name, der mit einem anderen beginnt, getrennt bleibt.
	t.Run("Konten bleiben getrennt, auch beim Löschen", func(t *testing.T) {
		a := neu(t)
		speichere(t, a, "konto-1", "von konto-1", 0)
		v10 := speichere(t, a, "konto-10", "von konto-10", 0)
		if err := a.Loeschen(ctx, "konto-1"); err != nil {
			t.Fatal(err)
		}
		erwarteFassung(t, a, "konto-10", "von konto-10", v10)
		if _, err := a.Holen(ctx, "konto-1", 0); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("konto-1 nach dem Löschen: %v, erwartet ErrNichtGefunden", err)
		}
	})

	t.Run("nach dem Löschen beginnt das Konto wieder mit Version 0", func(t *testing.T) {
		a := neu(t)
		v1 := speichere(t, a, "konto-1", "erste", 0)
		speichere(t, a, "konto-1", "zweite", v1)
		if err := a.Loeschen(ctx, "konto-1"); err != nil {
			t.Fatal(err)
		}
		// KRITISCH: Auch die älteren Fassungen sind weg (DSGVO Art. 17).
		if liste := fassungen(t, a, "konto-1"); len(liste) != 0 {
			t.Errorf("nach dem Löschen noch Fassungen: %v", liste)
		}
		if _, err := a.Holen(ctx, "konto-1", v1); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("ältere Fassung nach dem Löschen: %v, erwartet ErrNichtGefunden", err)
		}
		if _, err := a.Holen(ctx, "konto-1", 0); !errors.Is(err, ErrNichtGefunden) {
			t.Errorf("Holen nach dem Löschen: %v, erwartet ErrNichtGefunden", err)
		}
		v := speichere(t, a, "konto-1", "neu", 0)
		erwarteFassung(t, a, "konto-1", "neu", v)
	})

	// Anforderung R-4: Ältere Fassungen lassen sich auflisten und holen.
	t.Run("listet alle Fassungen, die neueste zuerst, und holt jede", func(t *testing.T) {
		a := neu(t)
		vorher := time.Now().Add(-time.Minute) // Uhr des Nachbaus darf etwas abweichen
		v1 := speichere(t, a, "konto-1", "erste", 0)
		v2 := speichere(t, a, "konto-1", "zweite!", v1)
		v3 := speichere(t, a, "konto-1", "dritte!!", v2)
		speichere(t, a, "konto-10", "anderes konto", 0)
		liste := fassungen(t, a, "konto-1")
		erwartet := []struct {
			version int64
			inhalt  string
		}{{v3, "dritte!!"}, {v2, "zweite!"}, {v1, "erste"}}
		if len(liste) != len(erwartet) {
			t.Fatalf("%d Fassungen (%v), erwartet %d", len(liste), liste, len(erwartet))
		}
		for i, e := range erwartet {
			f := liste[i]
			if f.Version != e.version || f.Bytes != int64(len(e.inhalt)) {
				t.Errorf("Fassung %d = Version %d, %d Bytes; erwartet Version %d, %d Bytes", i, f.Version, f.Bytes, e.version, len(e.inhalt))
			}
			if f.Gespeichert.Before(vorher) || f.Gespeichert.After(time.Now().Add(time.Minute)) {
				t.Errorf("Fassung %d gespeichert %v, erwartet etwa jetzt", i, f.Gespeichert)
			}
			erwarteVersion(t, a, "konto-1", e.version, e.inhalt)
		}
		// Version 0 bleibt die neueste.
		erwarteFassung(t, a, "konto-1", "dritte!!", v3)
	})

	t.Run("ohne Sicherung ist die Liste leer", func(t *testing.T) {
		if liste := fassungen(t, neu(t), "konto-1"); len(liste) != 0 {
			t.Errorf("Fassungen = %v, erwartet keine", liste)
		}
	})

	// KRITISCH: Eine Version holt nur eine Fassung des eigenen Kontos; alles
	// andere ist "nicht gefunden", nie die neueste oder eine fremde.
	t.Run("holt keine unbekannte, negative oder fremde Version", func(t *testing.T) {
		a := neu(t)
		v1 := speichere(t, a, "konto-1", "erste", 0)
		v2 := speichere(t, a, "konto-1", "zweite", v1)
		fremd := speichere(t, a, "konto-10", "von konto-10", 0)
		faelle := []struct {
			name    string
			version int64
		}{
			{"erfundene Version", v2 + 1},
			{"negative Version", -1},
			{"Version eines anderen Kontos", fremd},
		}
		for _, fall := range faelle {
			t.Run(fall.name, func(t *testing.T) {
				f, err := a.Holen(ctx, "konto-1", fall.version)
				if !errors.Is(err, ErrNichtGefunden) {
					t.Errorf("Holen = %q, %v; erwartet ErrNichtGefunden", f.Chiffrat, err)
				}
			})
		}
		// Gegenprobe: Die Liste von konto-1 enthält die fremde Version nicht.
		if slices.ContainsFunc(fassungen(t, a, "konto-1"), func(f FassungsInfo) bool { return f.Version == fremd }) {
			t.Error("Fassung von konto-10 in der Liste von konto-1")
		}
	})

	t.Run("Löschen ohne Sicherung ist kein Fehler", func(t *testing.T) {
		if err := neu(t).Loeschen(ctx, "konto-1"); err != nil {
			t.Errorf("Fehler = %v", err)
		}
	})

	t.Run("Löschen ohne Konto-ID ist ein Fehler", func(t *testing.T) {
		if err := neu(t).Loeschen(ctx, ""); err == nil {
			t.Error("kein Fehler")
		}
	})

	// Konten (für den Lauf, der Sicherungen ohne Konto löscht). Die Grenze liegt
	// eine Stunde neben jetzt, damit die Uhr des Emulators nicht stört.
	konten := func(t *testing.T, a Ablage, vor time.Time) []string {
		t.Helper()
		ids, err := a.Konten(ctx, vor)
		if err != nil {
			t.Fatalf("Konten: %v", err)
		}
		return ids
	}
	t.Run("Konten nennt jedes Konto mit Fassungen vor der Grenze einmal", func(t *testing.T) {
		a := neu(t)
		v1 := speichere(t, a, "konto-1", "erste", 0)
		speichere(t, a, "konto-1", "zweite", v1)
		speichere(t, a, "konto-10", "anderes Konto", 0)
		if ids := konten(t, a, time.Now().Add(time.Hour)); !slices.Equal(ids, []string{"konto-1", "konto-10"}) {
			t.Errorf("Konten = %v, erwartet [konto-1 konto-10]", ids)
		}
	})
	t.Run("Konten lässt neuere Fassungen und gelöschte Sicherungen weg", func(t *testing.T) {
		a := neu(t)
		speichere(t, a, "konto-1", "erste", 0)
		speichere(t, a, "konto-2", "zweite", 0)
		if ids := konten(t, a, time.Now().Add(-time.Hour)); len(ids) != 0 {
			t.Errorf("Konten vor einer Stunde = %v, erwartet keine", ids)
		}
		if err := a.Loeschen(ctx, "konto-1"); err != nil {
			t.Fatal(err)
		}
		if ids := konten(t, a, time.Now().Add(time.Hour)); !slices.Equal(ids, []string{"konto-2"}) {
			t.Errorf("Konten = %v, erwartet [konto-2]", ids)
		}
	})
}

// KRITISCH: In Cloud Storage bleiben nach dem Löschen auch keine älteren Fassungen
// liegen (DSGVO Art. 17). Der Vertrag prüft das über Fassungen; dieser Test
// zählt zusätzlich direkt im Bucket nach, an der Ablage vorbei.
func TestCloudStorageLoeschtAlleFassungen(t *testing.T) {
	ctx := context.Background()
	c, bucket := testBucket(t)
	a := NeuesCloudStorage(c, bucket)
	v1 := speichere(t, a, "konto-1", "erste", 0)
	v2 := speichere(t, a, "konto-1", "zweite", v1)
	speichere(t, a, "konto-1", "dritte", v2)
	speichere(t, a, "konto-10", "anderes konto", 0)

	generationen := func(name string) int {
		n := 0
		it := c.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: name, Versions: true})
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return n
			}
			if err != nil {
				t.Fatal(err)
			}
			if attrs.Name == name {
				n++
			}
		}
	}
	if n := generationen("sicherung/konto-1"); n != 3 {
		t.Fatalf("vor dem Löschen %d Fassungen, erwartet 3 (Versionierung an?)", n)
	}
	if err := a.Loeschen(ctx, "konto-1"); err != nil {
		t.Fatal(err)
	}
	if n := generationen("sicherung/konto-1"); n != 0 {
		t.Errorf("nach dem Löschen noch %d Fassungen", n)
	}
	if n := generationen("sicherung/konto-10"); n != 1 {
		t.Errorf("anderes Konto hat %d Fassungen, erwartet 1", n)
	}
}
