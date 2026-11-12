package diagnose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
)

// Vertragstest: Dieselben Fälle laufen gegen beide Umsetzungen. Cloud Storage
// läuft gegen den Nachbau fake-gcs-server (siehe datensicherung/vertrag_test.go):
//
//	STORAGE_EMULATOR_HOST=localhost:4443 go test ./...
//
// Ohne Nachbau werden die Cloud-Storage-Fälle übersprungen; mit
// EMULATOR_PFLICHT=1 (in der CI) schlagen sie stattdessen fehl.

// pruefling ist eine Ablage und eine Möglichkeit, ihren Inhalt zu lesen (der
// Server selbst kann das nicht).
type pruefling struct {
	ablage Ablage
	inhalt func(t *testing.T) map[string][]byte
}

func TestArbeitsspeicherErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) pruefling {
		a := NeuerArbeitsspeicher()
		return pruefling{a, func(*testing.T) map[string][]byte { return a.Objekte() }}
	})
}

func TestCloudStorageErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) pruefling {
		c, bucket := testBucket(t)
		return pruefling{NeuesCloudStorage(c, bucket), func(t *testing.T) map[string][]byte {
			return liesBucket(t, c.Bucket(bucket))
		}}
	})
}

// testBucket legt im Nachbau einen eigenen Bucket an (ohne Versionierung, wie im
// Betrieb).
func testBucket(t *testing.T) (*storage.Client, string) {
	t.Helper()
	if os.Getenv("STORAGE_EMULATOR_HOST") == "" {
		if os.Getenv("EMULATOR_PFLICHT") != "" {
			t.Fatal("STORAGE_EMULATOR_HOST fehlt, Nachbau ist aber Pflicht")
		}
		t.Skip("kein fake-gcs-server (STORAGE_EMULATOR_HOST nicht gesetzt)")
	}
	ctx := context.Background()
	c, err := datensicherung.NeuerClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "test-" + hex.EncodeToString(b)
	if err := c.Bucket(name).Create(ctx, "test", nil); err != nil {
		t.Fatal(err)
	}
	return c, name
}

func liesBucket(t *testing.T, b *storage.BucketHandle) map[string][]byte {
	t.Helper()
	ctx := context.Background()
	inhalt := map[string][]byte{}
	it := b.Objects(ctx, nil)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return inhalt
		}
		if err != nil {
			t.Fatal(err)
		}
		r, err := b.Object(attrs.Name).NewReader(ctx)
		if err != nil {
			t.Fatal(err)
		}
		daten, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		inhalt[attrs.Name] = daten
	}
}

// festerZufall lässt objektname bis zum Ende des Tests immer denselben Namen
// liefern (Tests dieses Pakets laufen nicht parallel).
func festerZufall(t *testing.T) {
	t.Helper()
	vorher := zufall
	zufall = func(b []byte) (int, error) { clear(b); return len(b), nil }
	t.Cleanup(func() { zufall = vorher })
}

var namensmuster = regexp.MustCompile(`^diagnose/([0-9a-f]+)/20261104T120000Z-[0-9a-f]{8}\.jsonl\.gz$`)

func pruefeVertrag(t *testing.T, neu func(t *testing.T) pruefling) {
	ctx := context.Background()
	jetzt := time.Date(2026, 11, 4, 13, 0, 0, 0, time.FixedZone("MEZ", 60*60)) // 12:00 UTC

	t.Run("legt die Sendung byte-gleich im Ordner des Kontos ab", func(t *testing.T) {
		p := neu(t)
		name, err := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("sendung"))
		if err != nil {
			t.Fatal(err)
		}
		m := namensmuster.FindStringSubmatch(name)
		if m == nil || m[1] != "a1" {
			t.Fatalf("Name %q passt nicht zu diagnose/a1/<UTC-Zeit>-<zufall>.jsonl.gz", name)
		}
		inhalt := p.inhalt(t)
		if len(inhalt) != 1 || string(inhalt[name]) != "sendung" {
			t.Errorf("abgelegt: %q", inhalt)
		}
	})

	// KRITISCH: Eine Sendung überschreibt nie eine andere, auch nicht in
	// derselben Sekunde.
	t.Run("zwei Sendungen in derselben Sekunde ergeben zwei Objekte", func(t *testing.T) {
		p := neu(t)
		n1, err1 := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("eins"))
		n2, err2 := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("zwei"))
		if err := errors.Join(err1, err2); err != nil {
			t.Fatal(err)
		}
		inhalt := p.inhalt(t)
		if n1 == n2 || string(inhalt[n1]) != "eins" || string(inhalt[n2]) != "zwei" {
			t.Errorf("Namen %q und %q, abgelegt: %q", n1, n2, inhalt)
		}
	})

	// KRITISCH: Trifft der Zufall doch einmal einen vorhandenen Namen, schlägt
	// das Ablegen fehl, statt die ältere Sendung zu überschreiben.
	t.Run("überschreibt bei gleichem Namen nicht, sondern meldet einen Fehler", func(t *testing.T) {
		p := neu(t)
		festerZufall(t)
		n1, err := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("eins"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("zwei")); err == nil {
			t.Error("kein Fehler, erwartet einen")
		}
		if inhalt := p.inhalt(t); len(inhalt) != 1 || string(inhalt[n1]) != "eins" {
			t.Errorf("abgelegt: %q", inhalt)
		}
	})

	t.Run("jedes Konto bekommt einen eigenen Ordner", func(t *testing.T) {
		p := neu(t)
		n1, err1 := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("x"))
		n2, err2 := p.ablage.Ablegen(ctx, "b2", jetzt, []byte("y"))
		if err := errors.Join(err1, err2); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(n1, "diagnose/a1/") || !strings.HasPrefix(n2, "diagnose/b2/") {
			t.Errorf("Namen %q und %q", n1, n2)
		}
	})

	// KRITISCH: Löschen trifft nur die Sendungen dieses Kontos, auch wenn eine
	// andere Konto-ID mit derselben Zeichenfolge beginnt (a1 und a10).
	t.Run("Löschen entfernt alle Sendungen des Kontos und nur diese", func(t *testing.T) {
		p := neu(t)
		var namen []string
		for _, konto := range []string{"a1", "a1", "a10", "b2"} {
			name, err := p.ablage.Ablegen(ctx, konto, jetzt, []byte(konto))
			if err != nil {
				t.Fatal(err)
			}
			namen = append(namen, name)
		}
		if err := p.ablage.Loeschen(ctx, "a1"); err != nil {
			t.Fatal(err)
		}
		inhalt := p.inhalt(t)
		if len(inhalt) != 2 || string(inhalt[namen[2]]) != "a10" || string(inhalt[namen[3]]) != "b2" {
			t.Errorf("nach dem Löschen: %q", inhalt)
		}
		// Wiederholt und ohne Sendungen: kein Fehler.
		if err := p.ablage.Loeschen(ctx, "a1"); err != nil {
			t.Errorf("zweites Löschen: %v", err)
		}
	})

	t.Run("Löschen ohne Konto-ID lehnt ab und löscht nichts", func(t *testing.T) {
		p := neu(t)
		if _, err := p.ablage.Ablegen(ctx, "a1", jetzt, []byte("x")); err != nil {
			t.Fatal(err)
		}
		if err := p.ablage.Loeschen(ctx, ""); err == nil {
			t.Error("kein Fehler, erwartet einen")
		}
		if inhalt := p.inhalt(t); len(inhalt) != 1 {
			t.Errorf("nach dem Löschen: %q", inhalt)
		}
	})

	t.Run("ohne Konto-ID wird nichts abgelegt", func(t *testing.T) {
		p := neu(t)
		if _, err := p.ablage.Ablegen(ctx, "", jetzt, []byte("x")); err == nil {
			t.Error("kein Fehler, erwartet einen")
		}
		if inhalt := p.inhalt(t); len(inhalt) != 0 {
			t.Errorf("abgelegt: %q", inhalt)
		}
	})
}
