// Package testhilfe enthält Bausteine für Tests: ein simuliertes Google, das
// ID-Tokens ausstellt, und simulierte Geräte mit Geräteschlüssel. Nur für Tests
// gedacht (internal: von außen nicht nutzbar).
package testhilfe

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
)

// Google simuliert den Aussteller der ID-Tokens.
type Google struct {
	schluessel *rsa.PrivateKey
	ClientID   string
}

// NeuesGoogle erzeugt einen Signierschlüssel.
func NeuesGoogle(t testing.TB, clientID string) *Google {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &Google{schluessel: k, ClientID: clientID}
}

// Schluessel ist die Schlüsselquelle für anmeldung.NeuerPruefer.
func (g *Google) Schluessel() oidc.KeySet {
	return &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&g.schluessel.PublicKey}}
}

// IDToken stellt ein gültiges ID-Token für das Google-Konto sub aus, gebunden
// an das Gerät (nonce).
func (g *Google) IDToken(t testing.TB, sub string, geraet *Geraet, jetzt time.Time) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: g.schluessel}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(map[string]any{
		"iss":   "https://accounts.google.com",
		"aud":   g.ClientID,
		"sub":   sub,
		"iat":   jetzt.Unix(),
		"exp":   jetzt.Add(time.Hour).Unix(),
		"nonce": geraeteschluessel.Nonce(geraet.DER),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// Geraet simuliert ein Gerät mit Geräteschlüssel.
type Geraet struct {
	privat *ecdsa.PrivateKey
	DER    []byte // öffentlicher Schlüssel, PKIX/DER
}

// NeuesGeraet erzeugt einen Geräteschlüssel.
func NeuesGeraet(t testing.TB) *Geraet {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return &Geraet{privat: k, DER: der}
}

// ID ist die Geräte-ID, die der Server vergibt.
func (g *Geraet) ID() string { return geraeteschluessel.GeraetID(g.DER) }

// Signiere signiert eine Nachricht wie ein echtes Gerät.
func (g *Geraet) Signiere(t testing.TB, nachricht []byte) []byte {
	t.Helper()
	summe := sha256.Sum256(nachricht)
	sig, err := ecdsa.SignASN1(rand.Reader, g.privat, summe[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}
