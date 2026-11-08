package geraeteschluessel

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"testing"
	"time"
)

func neuerSchluessel(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	privat, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privat.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privat, der
}

func pkix(t *testing.T, schluessel any) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(schluessel)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func signiere(t *testing.T, privat *ecdsa.PrivateKey, nachricht []byte) []byte {
	t.Helper()
	summe := sha256.Sum256(nachricht)
	sig, err := ecdsa.SignASN1(rand.Reader, privat, summe[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func TestLese(t *testing.T) {
	_, p256 := neuerSchluessel(t)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	edPub, _, _ := ed25519.GenerateKey(rand.Reader)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	x25519, _ := ecdh.X25519().GenerateKey(rand.Reader)

	tests := []struct {
		name    string
		der     []byte
		wantErr bool
	}{
		{"nimmt P-256 an", p256, false},
		{"lehnt leeren Schlüssel ab", nil, true},
		{"lehnt Müll ab", []byte("kein schlüssel"), true},
		{"lehnt P-384 ab", pkix(t, &p384.PublicKey), true},
		{"lehnt Ed25519 ab", pkix(t, edPub), true},
		{"lehnt RSA ab", pkix(t, &rsaKey.PublicKey), true},
		{"lehnt X25519 ab", pkix(t, x25519.PublicKey()), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Lese(tt.der)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrUngueltigerSchluessel) {
				t.Errorf("falscher Fehlertyp: %v", err)
			}
		})
	}
}

func TestPruefeSignatur(t *testing.T) {
	privat, der := neuerSchluessel(t)
	anderer, _ := neuerSchluessel(t)
	schluessel, err := Lese(der)
	if err != nil {
		t.Fatal(err)
	}
	jetzt := time.Unix(1_800_000_000, 0)
	nachricht := SitzungsNachricht("geraet-1", jetzt)

	tests := []struct {
		name      string
		nachricht []byte
		signatur  []byte
		want      bool
	}{
		{"nimmt passende Signatur an", nachricht, signiere(t, privat, nachricht), true},
		{"lehnt Signatur eines anderen Schlüssels ab", nachricht, signiere(t, anderer, nachricht), false},
		{"lehnt Signatur über andere Geräte-ID ab", nachricht, signiere(t, privat, SitzungsNachricht("geraet-2", jetzt)), false},
		{"lehnt Signatur über anderen Zeitpunkt ab", nachricht, signiere(t, privat, SitzungsNachricht("geraet-1", jetzt.Add(time.Second))), false},
		{"lehnt Anmelde-Signatur als Sitzungs-Signatur ab", nachricht, signiere(t, privat, AnmeldeNachricht("geraet-1\n1800000000")), false},
		{"lehnt leere Signatur ab", nachricht, nil, false},
		{"lehnt Müll als Signatur ab", nachricht, []byte{1, 2, 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PruefeSignatur(schluessel, tt.nachricht, tt.signatur); got != tt.want {
				t.Errorf("PruefeSignatur = %v, erwartet %v", got, tt.want)
			}
		})
	}
}

// Die Zweck-Präfixe müssen verschiedene Nachrichten erzeugen, auch wenn ein
// Angreifer den Rest passend wählt.
func TestNachrichtenSindNachZweckGetrennt(t *testing.T) {
	jetzt := time.Unix(1_800_000_000, 0)
	a := string(AnmeldeNachricht("x"))
	s := string(SitzungsNachricht("x", jetzt))
	if a[:len(zweckAnmelden)] == s[:len(zweckAnmelden)] {
		t.Errorf("Präfixe gleich: %q / %q", a, s)
	}
}

func TestGeraetIDUndNonceSindStabilUndVerschieden(t *testing.T) {
	_, a := neuerSchluessel(t)
	_, b := neuerSchluessel(t)
	kopie := append([]byte(nil), a...)
	z1, z2 := []byte("0123456789abcdef"), []byte("fedcba9876543210")
	if GeraetID(a) != GeraetID(kopie) || Nonce(a, z1) != Nonce(kopie, z1) {
		t.Error("gleicher Schlüssel ergibt unterschiedliche Werte")
	}
	if GeraetID(a) == GeraetID(b) || Nonce(a, z1) == Nonce(b, z1) {
		t.Error("verschiedene Schlüssel ergeben gleiche Werte")
	}
	if Nonce(a, z1) == Nonce(a, z2) {
		t.Error("verschiedener Zufallsanteil ergibt dieselbe Nonce")
	}
	if len(GeraetID(a)) != 32 {
		t.Errorf("Geräte-ID hat Länge %d, erwartet 32", len(GeraetID(a)))
	}
}

func TestIstGeraetID(t *testing.T) {
	_, der := neuerSchluessel(t)
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"nimmt abgeleitete ID an", GeraetID(der), true},
		{"lehnt leere ID ab", "", false},
		{"lehnt Großbuchstaben ab", "ABCDEF0123456789ABCDEF0123456789", false},
		{"lehnt 31 Zeichen ab", GeraetID(der)[:31], false},
		{"lehnt 33 Zeichen ab", GeraetID(der) + "0", false},
		{"lehnt Pfad ab", "0123456789abcdef/123456789abcdef", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IstGeraetID(tt.id); got != tt.want {
				t.Errorf("IstGeraetID(%q) = %v, erwartet %v", tt.id, got, tt.want)
			}
		})
	}
}
