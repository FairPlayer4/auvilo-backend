package kontingent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
)

// Vertragstest: Dieselben Fälle laufen gegen beide Umsetzungen. So verhält sich
// der Arbeitsspeicher, mit dem die Dienste getestet werden, wie Firestore.
//
//	gcloud emulators firestore start --host-port=localhost:8086
//	FIRESTORE_EMULATOR_HOST=localhost:8086 go test ./...
//
// Ohne Emulator werden die Firestore-Fälle übersprungen; mit EMULATOR_PFLICHT=1
// (in der CI) schlagen sie stattdessen fehl.

func TestArbeitsspeicherErfuelltVertrag(t *testing.T) {
	pruefeVertrag(t, func(t *testing.T) Zaehler { return NeuerArbeitsspeicher() })
}

func TestFirestoreErfuelltVertrag(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		if os.Getenv("EMULATOR_PFLICHT") != "" {
			t.Fatal("FIRESTORE_EMULATOR_HOST fehlt, Emulator ist aber Pflicht")
		}
		t.Skip("kein Firestore-Emulator (FIRESTORE_EMULATOR_HOST nicht gesetzt)")
	}
	pruefeVertrag(t, func(t *testing.T) Zaehler {
		// Jeder Testfall bekommt ein eigenes (Emulator-)Projekt.
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

// Mittag des 4. November in UTC.
var mittag = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// vorgang ist ein Aufruf von Verbrauche mit dem erwarteten Ergebnis.
type vorgang struct {
	art, konto string
	jetzt      time.Time
	max        int
	wantOK     bool
}

func pruefeVertrag(t *testing.T, neu func(t *testing.T) Zaehler) {
	ctx := context.Background()
	berlin := time.FixedZone("MEZ", 60*60)

	// KRITISCH: Zu jeder Erlaubnis der passende Ablehnungsfall.
	tests := []struct {
		name      string
		vorgaenge []vorgang
	}{
		{"zählt bis zur Grenze, danach erschöpft", []vorgang{
			{"diagnose", "k1", mittag, 2, true},
			{"diagnose", "k1", mittag, 2, true},
			{"diagnose", "k1", mittag, 2, false},
		}},
		// Eine Ablehnung verbraucht nichts: Nach zwei abgelehnten Versuchen mit
		// Grenze 1 geht mit Grenze 2 genau einer mehr.
		{"abgelehnte Vorgänge werden nicht gezählt", []vorgang{
			{"diagnose", "k1", mittag, 1, true},
			{"diagnose", "k1", mittag, 1, false},
			{"diagnose", "k1", mittag, 1, false},
			{"diagnose", "k1", mittag, 2, true},
			{"diagnose", "k1", mittag, 2, false},
		}},
		{"Grenze 0 lehnt immer ab", []vorgang{
			{"diagnose", "k1", mittag, 0, false},
		}},
		{"jedes Konto hat einen eigenen Zähler", []vorgang{
			{"diagnose", "k1", mittag, 1, true},
			{"diagnose", "k2", mittag, 1, true},
			{"diagnose", "k1", mittag, 1, false},
		}},
		{"jede Art hat einen eigenen Zähler", []vorgang{
			{"diagnose", "k1", mittag, 1, true},
			{"sicherung", "k1", mittag, 1, true},
			{"diagnose", "k1", mittag, 1, false},
		}},
		{"um Mitternacht UTC beginnt der Zähler neu", []vorgang{
			{"diagnose", "k1", time.Date(2026, 11, 4, 23, 59, 59, 0, time.UTC), 1, true},
			{"diagnose", "k1", time.Date(2026, 11, 4, 23, 59, 59, 0, time.UTC), 1, false},
			{"diagnose", "k1", time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), 1, true},
		}},
		// 00:30 in Berlin (MEZ) ist noch 23:30 UTC des Vortags.
		{"gezählt wird nach UTC, nicht nach Ortszeit", []vorgang{
			{"diagnose", "k1", time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC), 1, true},
			{"diagnose", "k1", time.Date(2026, 11, 5, 0, 30, 0, 0, berlin), 1, false},
		}},
		{"Art ohne Konto ist ein Fehler", []vorgang{
			{"diagnose", "", mittag, 5, false},
		}},
		{"Konto ohne Art ist ein Fehler", []vorgang{
			{"", "k1", mittag, 5, false},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := neu(t)
			for i, v := range tt.vorgaenge {
				err := z.Verbrauche(ctx, v.art, v.konto, v.jetzt, v.max)
				if v.wantOK && err != nil {
					t.Fatalf("Vorgang %d: Fehler %v, erwartet erlaubt", i, err)
				}
				if !v.wantOK && err == nil {
					t.Fatalf("Vorgang %d: erlaubt, erwartet abgelehnt", i)
				}
				// Nur die Grenze meldet ErrErschoepft, fehlende Angaben nicht.
				fehltEtwas := v.art == "" || v.konto == ""
				if !v.wantOK && errors.Is(err, ErrErschoepft) == fehltEtwas {
					t.Fatalf("Vorgang %d: Fehler %v passt nicht zum Grund", i, err)
				}
			}
		})
	}

	// KRITISCH: Gleichzeitige Aufrufe dürfen die Grenze nicht überschreiten.
	// Andere Fehler sind erlaubt (der Emulator bricht Transaktionen bei vielen
	// gleichzeitigen Zugriffen ab, der Dienst meldet dann "unavailable"); sie
	// dürfen aber nichts zählen. Darum am Ende nacheinander auffüllen: Insgesamt
	// gehen genau so viele durch, wie die Grenze erlaubt.
	t.Run("gleichzeitig kommen höchstens so viele durch wie erlaubt", func(t *testing.T) {
		z := neu(t)
		const versuche, grenze = 8, 4
		var erlaubt atomic.Int32
		var wg sync.WaitGroup
		for range versuche {
			wg.Go(func() {
				err := z.Verbrauche(ctx, "diagnose", "k1", mittag, grenze)
				switch {
				case err == nil:
					erlaubt.Add(1)
				case !errors.Is(err, ErrErschoepft):
					t.Logf("vorübergehender Fehler: %v", err)
				}
			})
		}
		wg.Wait()
		if n := erlaubt.Load(); n > grenze {
			t.Fatalf("%d von %d gleichzeitig erlaubt, Grenze %d", n, versuche, grenze)
		}
		for z.Verbrauche(ctx, "diagnose", "k1", mittag, grenze) == nil {
			erlaubt.Add(1)
		}
		if n := erlaubt.Load(); n != grenze {
			t.Errorf("insgesamt %d erlaubt, erwartet genau %d", n, grenze)
		}
	})
}
