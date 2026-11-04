package dienst

import (
	"bytes"
	"compress/gzip"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Grenzen aus diagnose.proto als eigene Zahlen, nicht als Konstanten des Codes:
// Sonst blieben die Tests grün, wenn jemand die Konstante ändert.
const (
	kiB256         = 256 << 10
	diagnosenAmTag = 10
)

func (ts *testServer) diagnoseSenden(token string, nutzlast []byte) error {
	_, err := ts.diagnose.DiagnoseSenden(context.Background(), mitSitzung(&v1.DiagnoseSendenRequest{Nutzlast: nutzlast}, token))
	return err
}

// freischalten meldet ein Gerät an und schaltet sein Konto für Diagnosen frei.
func (ts *testServer) freischalten(t *testing.T, sub string) (token, kontoID string) {
	t.Helper()
	res := ts.anmelden(t, sub, testhilfe.NeuesGeraet(t))
	ts.freigeschaltet[res.KontoId] = true
	return res.Sitzung.Token, res.KontoId
}

// gzipJSON komprimiert eine kleine Sendung, wie die App sie schickt.
func gzipJSON(t *testing.T, zeilen string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(zeilen)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// mitGzipKopf: n Bytes, die wie gzip beginnen (für Größentests).
func mitGzipKopf(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte{0x1f, 0x8b, 0x08})
	return b
}

func TestDiagnoseSendenLegtDieSendungImOrdnerDesKontosAb(t *testing.T) {
	// Das Log darf die Diagnose nicht enthalten (nur Gerät und Größe).
	var log bytes.Buffer
	vorher := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(vorher) })

	ts := neuerTestServer(t)
	token, konto := ts.freischalten(t, "google-kevin")
	sendung := gzipJSON(t, `{"art":"kopf","app":"geheimes-merkmal"}`+"\n")

	if err := ts.diagnoseSenden(token, sendung); err != nil {
		t.Fatal(err)
	}
	objekte := ts.diagnosen.Objekte()
	if len(objekte) != 1 {
		t.Fatalf("%d Objekte, erwartet 1", len(objekte))
	}
	for name, inhalt := range objekte {
		if !strings.HasPrefix(name, "diagnose/"+konto+"/") || !bytes.Equal(inhalt, sendung) {
			t.Errorf("abgelegt als %q mit %d Bytes", name, len(inhalt))
		}
	}
	if strings.Contains(log.String(), "geheimes-merkmal") || bytes.Contains(log.Bytes(), sendung) {
		t.Error("Inhalt der Diagnose steht im Log")
	}
}

func TestDiagnoseSendenPrueftDieAnfrage(t *testing.T) {
	ts := neuerTestServer(t)
	frei, _ := ts.freischalten(t, "google-kevin")
	fremd := ts.anmelden(t, "google-tester", testhilfe.NeuesGeraet(t)).Sitzung.Token

	t.Run("nimmt eine Sendung von genau 256 KiB an", func(t *testing.T) {
		if err := ts.diagnoseSenden(frei, mitGzipKopf(kiB256)); err != nil {
			t.Fatal(err)
		}
	})

	// KRITISCH: Zu jeder Erlaubnis der Ablehnungsfall; abgelehnt wird nichts
	// abgelegt.
	faelle := []struct {
		name     string
		token    string
		nutzlast []byte
		code     connect.Code
	}{
		{"lehnt Konto ab, das nicht freigeschaltet ist", fremd, gzipJSON(t, "{}"), connect.CodePermissionDenied},
		{"lehnt Aufruf ohne Sitzung ab", "", gzipJSON(t, "{}"), connect.CodeUnauthenticated},
		{"lehnt leere Sendung ab", frei, nil, connect.CodeInvalidArgument},
		{"lehnt Sendung ohne gzip ab", frei, []byte(`{"art":"kopf"}`), connect.CodeInvalidArgument},
		{"lehnt Sendung mit unvollständigem gzip-Kopf ab", frei, []byte{0x1f, 0x8b}, connect.CodeInvalidArgument},
		// Knapp darüber prüft der Dienst, weit darüber schon Connect.
		{"lehnt Sendung knapp über 256 KiB ab", frei, mitGzipKopf(kiB256 + 1), connect.CodeResourceExhausted},
		{"lehnt Sendung weit über 256 KiB ab", frei, mitGzipKopf(kiB256 + 8<<10), connect.CodeResourceExhausted},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			vorher := len(ts.diagnosen.Objekte())
			erwarteCode(t, ts.diagnoseSenden(fall.token, fall.nutzlast), fall.code)
			if n := len(ts.diagnosen.Objekte()); n != vorher {
				t.Errorf("%d Objekte, vorher %d: abgelehnte Sendung wurde abgelegt", n, vorher)
			}
		})
	}
}

func TestDiagnoseSendenHatEinTageslimitJeKonto(t *testing.T) {
	ts := neuerTestServer(t)
	kevin, _ := ts.freischalten(t, "google-kevin")
	anna, _ := ts.freischalten(t, "google-anna")
	sendung := gzipJSON(t, "{}")

	for i := range diagnosenAmTag - 1 {
		if err := ts.diagnoseSenden(kevin, sendung); err != nil {
			t.Fatalf("Sendung %d: %v", i+1, err)
		}
	}
	// Abgelehnte Sendungen zählen nicht mit.
	for range 3 {
		erwarteCode(t, ts.diagnoseSenden(kevin, []byte("kein gzip")), connect.CodeInvalidArgument)
	}
	if err := ts.diagnoseSenden(kevin, sendung); err != nil {
		t.Fatalf("letzte erlaubte Sendung: %v", err)
	}
	t.Run("danach lehnt der Server ab", func(t *testing.T) {
		erwarteCode(t, ts.diagnoseSenden(kevin, sendung), connect.CodeResourceExhausted)
		if n := len(ts.diagnosen.Objekte()); n != diagnosenAmTag {
			t.Errorf("%d Objekte, erwartet %d", n, diagnosenAmTag)
		}
	})
	t.Run("ein anderes Konto hat sein eigenes Limit", func(t *testing.T) {
		if err := ts.diagnoseSenden(anna, sendung); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("am nächsten Tag (UTC) geht es wieder", func(t *testing.T) {
		ts.jetzt = time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 1, 0, time.UTC)
		neu, _ := ts.freischalten(t, "google-kevin") // die alte Sitzung ist abgelaufen
		if err := ts.diagnoseSenden(neu, sendung); err != nil {
			t.Fatal(err)
		}
	})
}
