package geraeteschluessel

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Gemeinsame Testvektoren mit dem Kotlin-Client (client/protokoll,
// GeraetenachrichtenTest). Laufen beide Tests grün, signieren App und Server
// Byte für Byte dasselbe.
type vektorNachricht struct {
	IDToken       string `json:"id_token"`
	ZeitpunktUnix int64  `json:"zeitpunkt_unix"`
	NachrichtHex  string `json:"nachricht_hex"`
	SignaturB64   string `json:"signatur_base64"`
}

type vektoren struct {
	SchluesselDERB64 string          `json:"geraeteschluessel_der_base64"`
	GeraetID         string          `json:"geraet_id"`
	NonceZufallHex   string          `json:"nonce_zufall_hex"`
	Nonce            string          `json:"nonce"`
	Anmelden         vektorNachricht `json:"anmelden"`
	Sitzung          vektorNachricht `json:"sitzung"`
}

func ladeVektoren(t *testing.T) vektoren {
	t.Helper()
	// Tests laufen im Paketverzeichnis: server/internal/geraeteschluessel.
	roh, err := os.ReadFile(filepath.Join("..", "..", "..", "proto", "testvektoren", "geraetenachrichten.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vektoren
	if err := json.Unmarshal(roh, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func dekodiere(t *testing.T, f func(string) ([]byte, error), s string) []byte {
	t.Helper()
	b, err := f(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTestvektoren(t *testing.T) {
	v := ladeVektoren(t)
	der := dekodiere(t, base64.StdEncoding.DecodeString, v.SchluesselDERB64)
	schluessel, err := Lese(der)
	if err != nil {
		t.Fatal(err)
	}
	anmeldung := dekodiere(t, hex.DecodeString, v.Anmelden.NachrichtHex)
	sitzung := dekodiere(t, hex.DecodeString, v.Sitzung.NachrichtHex)
	sigAnmeldung := dekodiere(t, base64.StdEncoding.DecodeString, v.Anmelden.SignaturB64)
	sigSitzung := dekodiere(t, base64.StdEncoding.DecodeString, v.Sitzung.SignaturB64)

	t.Run("Geräte-ID stimmt mit dem Client überein", func(t *testing.T) {
		if got := GeraetID(der); got != v.GeraetID {
			t.Errorf("GeraetID = %q, Vektor %q", got, v.GeraetID)
		}
	})
	t.Run("Nonce stimmt mit dem Client überein", func(t *testing.T) {
		if got := Nonce(der, dekodiere(t, hex.DecodeString, v.NonceZufallHex)); got != v.Nonce {
			t.Errorf("Nonce = %q, Vektor %q", got, v.Nonce)
		}
	})
	t.Run("Anmelde-Nachricht stimmt Byte für Byte", func(t *testing.T) {
		if got := AnmeldeNachricht(v.Anmelden.IDToken); !bytes.Equal(got, anmeldung) {
			t.Errorf("AnmeldeNachricht = %q, Vektor %q", got, anmeldung)
		}
	})
	t.Run("Sitzungs-Nachricht stimmt Byte für Byte", func(t *testing.T) {
		got := SitzungsNachricht(v.GeraetID, time.Unix(v.Sitzung.ZeitpunktUnix, 0))
		if !bytes.Equal(got, sitzung) {
			t.Errorf("SitzungsNachricht = %q, Vektor %q", got, sitzung)
		}
	})
	t.Run("Signaturen im Vektor gelten", func(t *testing.T) {
		if !PruefeSignatur(schluessel, anmeldung, sigAnmeldung) || !PruefeSignatur(schluessel, sitzung, sigSitzung) {
			t.Error("Signatur aus dem Vektor abgelehnt")
		}
	})
	t.Run("lehnt Signatur der Anmeldung für die Sitzung ab", func(t *testing.T) {
		if PruefeSignatur(schluessel, sitzung, sigAnmeldung) {
			t.Error("Signatur für fremde Nachricht angenommen")
		}
	})
}
