package freischaltung

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type vektoren struct {
	WiederherstellungHex string `json:"wiederherstellungsschluessel_hex"`
	SchluesselHex        string `json:"freischaltschluessel_hex"`
	GeraetID             string `json:"geraet_id"`
	NachrichtHex         string `json:"nachricht_hex"`
	SignaturHex          string `json:"signatur_hex"`
}

func ladeVektoren(t *testing.T) vektoren {
	t.Helper()
	roh, err := os.ReadFile(filepath.Join("..", "..", "..", "proto", "testvektoren", "freischaltung.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v vektoren
	if err := json.Unmarshal(roh, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Gemeinsam mit Kotlin (FreischaltungTest): Laufen beide grün, leiten App und
// Server aus demselben Wiederherstellungsschlüssel denselben Schlüssel ab und
// signieren dieselben Bytes.
func TestTestvektoren(t *testing.T) {
	v := ladeVektoren(t)
	schluessel := hexBytes(t, v.SchluesselHex)

	if got := Nachricht(v.GeraetID); !bytes.Equal(got, hexBytes(t, v.NachrichtHex)) {
		t.Errorf("Nachricht = %x, erwartet %s", got, v.NachrichtHex)
	}
	if err := Pruefe(schluessel, v.GeraetID, hexBytes(t, v.SignaturHex)); err != nil {
		t.Errorf("Signatur aus den Testvektoren abgelehnt: %v", err)
	}

	// Die Ableitung steht nur im Client; hier geprüft, damit proto-Kommentar
	// (schluessel.proto) und Vektoren dasselbe sagen.
	seed, err := hkdf.Key(sha256.New, hexBytes(t, v.WiederherstellungHex), nil, "avlreminder/freischaltschluessel/v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	abgeleitet := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !bytes.Equal(abgeleitet, schluessel) {
		t.Errorf("abgeleiteter Freischaltschlüssel = %x, erwartet %s", abgeleitet, v.SchluesselHex)
	}
}

func TestPruefe(t *testing.T) {
	v := ladeVektoren(t)
	schluessel := hexBytes(t, v.SchluesselHex)
	signatur := hexBytes(t, v.SignaturHex)
	fremd, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	kaputt := bytes.Clone(signatur)
	kaputt[0] ^= 1

	tests := []struct {
		name       string
		schluessel []byte
		geraetID   string
		signatur   []byte
		wantOK     bool
	}{
		{"nimmt die gültige Signatur an", schluessel, v.GeraetID, signatur, true},
		// KRITISCH: Die Signatur gilt nur für das Gerät, dessen ID signiert ist.
		{"lehnt die Signatur für ein anderes Gerät ab", schluessel, "00000000000000000000000000000000", signatur, false},
		{"lehnt einen anderen Freischaltschlüssel ab", fremd, v.GeraetID, signatur, false},
		{"lehnt eine veränderte Signatur ab", schluessel, v.GeraetID, kaputt, false},
		{"lehnt eine leere Signatur ab", schluessel, v.GeraetID, nil, false},
		{"lehnt eine zu kurze Signatur ab", schluessel, v.GeraetID, signatur[:63], false},
		{"lehnt einen leeren Freischaltschlüssel ab", nil, v.GeraetID, signatur, false},
		{"lehnt einen zu langen Freischaltschlüssel ab", append(bytes.Clone(schluessel), 0), v.GeraetID, signatur, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Pruefe(tt.schluessel, tt.geraetID, tt.signatur)
			if tt.wantOK && err != nil {
				t.Errorf("abgelehnt: %v", err)
			}
			if !tt.wantOK && !errors.Is(err, ErrSignatur) {
				t.Errorf("Fehler = %v, erwartet ErrSignatur", err)
			}
		})
	}
}
