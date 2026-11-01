package anmeldung

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
)

const (
	unsereID  = "test-client.apps.googleusercontent.com"
	zweiteID  = "app-client.apps.googleusercontent.com"
	fremdeID  = "fremd.apps.googleusercontent.com"
	googleSub = "112233445566778899"
)

var jetzt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// Testumgebung: ein "Google"-Schlüssel, ein fremder Schlüssel und ein Gerät.
type umgebung struct {
	google, fremd *rsa.PrivateKey
	geraet        *ecdsa.PrivateKey
	geraetDER     []byte
	pruefer       *Pruefer
}

func neueUmgebung(t *testing.T) *umgebung {
	t.Helper()
	google, _ := rsa.GenerateKey(rand.Reader, 2048)
	fremd, _ := rsa.GenerateKey(rand.Reader, 2048)
	geraet, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.MarshalPKIXPublicKey(&geraet.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	// StaticKeySet ersetzt im Test die Schlüssel, die Google im Netz veröffentlicht.
	schluessel := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&google.PublicKey}}
	p, err := NeuerPruefer(schluessel, []string{unsereID, zweiteID}, func() time.Time { return jetzt })
	if err != nil {
		t.Fatal(err)
	}
	return &umgebung{google: google, fremd: fremd, geraet: geraet, geraetDER: der, pruefer: p}
}

// ansprueche sind die Felder eines gültigen ID-Tokens; Testfälle ändern einzelne.
func (u *umgebung) ansprueche() map[string]any {
	return map[string]any{
		"iss":   GoogleAussteller,
		"aud":   unsereID,
		"sub":   googleSub,
		"iat":   jetzt.Add(-time.Minute).Unix(),
		"exp":   jetzt.Add(time.Hour).Unix(),
		"nonce": geraeteschluessel.Nonce(u.geraetDER),
	}
}

func signiereToken(t *testing.T, schluessel *rsa.PrivateKey, ansprueche map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: schluessel}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(ansprueche).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func signiere(t *testing.T, schluessel *ecdsa.PrivateKey, nachricht []byte) []byte {
	t.Helper()
	summe := sha256.Sum256(nachricht)
	sig, err := ecdsa.SignASN1(rand.Reader, schluessel, summe[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// anfrage baut eine Anfrage: Token aus den Ansprüchen, korrekt signiert.
func (u *umgebung) anfrage(t *testing.T, ansprueche map[string]any) Anfrage {
	t.Helper()
	token := signiereToken(t, u.google, ansprueche)
	return Anfrage{
		IDToken:           token,
		Geraeteschluessel: u.geraetDER,
		Signatur:          signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht(token)),
	}
}

func mit(basis map[string]any, schluessel string, wert any) map[string]any {
	kopie := map[string]any{}
	for k, v := range basis {
		kopie[k] = v
	}
	if wert == nil {
		delete(kopie, schluessel)
	} else {
		kopie[schluessel] = wert
	}
	return kopie
}

func TestPruefe(t *testing.T) {
	u := neueUmgebung(t)
	anderesGeraet, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	anderesDER, _ := x509.MarshalPKIXPublicKey(&anderesGeraet.PublicKey)
	gut := u.ansprueche()

	tests := []struct {
		name    string
		anfrage func() Anfrage
		wantErr error // nil = Anmeldung gilt
	}{
		{"nimmt gültige Anmeldung an", func() Anfrage { return u.anfrage(t, gut) }, nil},
		{"nimmt zweite eigene Client-ID an", func() Anfrage { return u.anfrage(t, mit(gut, "aud", zweiteID)) }, nil},
		{"nimmt Aussteller ohne https an (so stellt Google manche Tokens aus)", func() Anfrage {
			return u.anfrage(t, mit(gut, "iss", "accounts.google.com"))
		}, nil},
		{"nimmt Token an, das knapp 5 Minuten alt ist", func() Anfrage {
			return u.anfrage(t, mit(gut, "iat", jetzt.Add(-5*time.Minute+time.Second).Unix()))
		}, nil},

		// Google-ID-Token
		{"lehnt Token mit fremdem Schlüssel signiert ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.IDToken = signiereToken(t, u.fremd, gut)
			a.Signatur = signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht(a.IDToken))
			return a
		}, ErrToken},
		{"lehnt fremden Aussteller ab", func() Anfrage { return u.anfrage(t, mit(gut, "iss", "https://evil.example")) }, ErrToken},
		{"lehnt abgelaufenes Token ab", func() Anfrage { return u.anfrage(t, mit(gut, "exp", jetzt.Add(-time.Second).Unix())) }, ErrToken},
		{"lehnt Token ohne exp ab", func() Anfrage { return u.anfrage(t, mit(gut, "exp", nil)) }, ErrToken},
		{"lehnt Token ohne sub ab", func() Anfrage { return u.anfrage(t, mit(gut, "sub", nil)) }, ErrToken},
		{"lehnt Müll als Token ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.IDToken = "kein.token.hier"
			a.Signatur = signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht(a.IDToken))
			return a
		}, ErrToken},
		{"lehnt unsigniertes Token ab (alg none)", func() Anfrage {
			a := u.anfrage(t, gut)
			teile := strings.Split(a.IDToken, ".")
			a.IDToken = "eyJhbGciOiJub25lIn0." + teile[1] + "."
			a.Signatur = signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht(a.IDToken))
			return a
		}, ErrToken},

		// Zielgruppe
		{"lehnt Token für fremde Client-ID ab", func() Anfrage { return u.anfrage(t, mit(gut, "aud", fremdeID)) }, ErrZielgruppe},
		{"lehnt Token mit mehreren Zielgruppen ab, auch wenn unsere dabei ist", func() Anfrage {
			return u.anfrage(t, mit(gut, "aud", []string{unsereID, fremdeID}))
		}, ErrZielgruppe},

		// Alter
		// Feste Werte statt MaxAlter und uhrAbweichung: sonst wanderten die Tests
		// mit den Konstanten mit und bemerkten keine geänderte Grenze.
		{"lehnt Token älter als 5 Minuten ab", func() Anfrage {
			return u.anfrage(t, mit(gut, "iat", jetzt.Add(-5*time.Minute-time.Second).Unix()))
		}, ErrZuAlt},
		{"nimmt Token an, dessen iat 59 Sekunden in der Zukunft liegt (Uhr von Google vor)", func() Anfrage {
			return u.anfrage(t, mit(gut, "iat", jetzt.Add(59*time.Second).Unix()))
		}, nil},
		{"lehnt Token ab, dessen iat 61 Sekunden in der Zukunft liegt", func() Anfrage {
			return u.anfrage(t, mit(gut, "iat", jetzt.Add(61*time.Second).Unix()))
		}, ErrZuAlt},
		{"lehnt Token ohne iat ab", func() Anfrage { return u.anfrage(t, mit(gut, "iat", nil)) }, ErrZuAlt},
		{"lehnt Token aus der Zukunft ab", func() Anfrage {
			return u.anfrage(t, mit(gut, "iat", jetzt.Add(2*time.Minute).Unix()))
		}, ErrZuAlt},

		// Nonce
		{"lehnt Token ohne Nonce ab", func() Anfrage { return u.anfrage(t, mit(gut, "nonce", nil)) }, ErrNonce},
		{"lehnt Token mit Nonce eines anderen Geräts ab", func() Anfrage {
			return u.anfrage(t, mit(gut, "nonce", geraeteschluessel.Nonce(anderesDER)))
		}, ErrNonce},
		{"lehnt abgefangenes Token mit eigenem Schlüssel des Angreifers ab", func() Anfrage {
			a := u.anfrage(t, gut) // Token gehört zum Gerät des Opfers
			a.Geraeteschluessel = anderesDER
			a.Signatur = signiere(t, anderesGeraet, geraeteschluessel.AnmeldeNachricht(a.IDToken))
			return a
		}, ErrNonce},

		// Signatur des Geräts
		{"lehnt fehlende Signatur ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.Signatur = nil
			return a
		}, ErrSignatur},
		{"lehnt Signatur über ein anderes Token ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.Signatur = signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht("anderes"))
			return a
		}, ErrSignatur},
		{"lehnt Signatur ohne Zweck-Präfix ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.Signatur = signiere(t, u.geraet, []byte(a.IDToken))
			return a
		}, ErrSignatur},

		// Geräteschlüssel
		{"lehnt ungültigen Geräteschlüssel ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.Geraeteschluessel = []byte("müll")
			return a
		}, geraeteschluessel.ErrUngueltigerSchluessel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := u.pruefer.Pruefe(context.Background(), tt.anfrage())
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unerwarteter Fehler: %v", err)
				}
				if got.GoogleSub != googleSub || got.GeraetID != geraeteschluessel.GeraetID(u.geraetDER) || got.Schluessel == nil {
					t.Errorf("Ergebnis = %+v", got)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Fehler = %v, erwartet %v", err, tt.wantErr)
			}
			if got != (Ergebnis{}) {
				t.Errorf("bei Fehler kein Ergebnis erwartet, bekommen %+v", got)
			}
		})
	}
}

func TestNeuerPrueferVerlangtClientIDs(t *testing.T) {
	if _, err := NeuerPruefer(&oidc.StaticKeySet{}, nil, nil); err == nil {
		t.Error("ohne Client-IDs angelegt, erwartet Fehler")
	}
}

func TestKennung(t *testing.T) {
	pfeffer := []byte("0123456789abcdef0123456789abcdef")
	anderer := []byte("fedcba9876543210fedcba9876543210")

	if erste, zweite := Kennung(pfeffer, googleSub), Kennung(pfeffer, googleSub); erste != zweite {
		t.Error("gleiche Eingabe ergibt unterschiedliche Kennung")
	}
	if Kennung(pfeffer, googleSub) == Kennung(pfeffer, "998877") {
		t.Error("verschiedene Konten ergeben dieselbe Kennung")
	}
	if Kennung(pfeffer, googleSub) == Kennung(anderer, googleSub) {
		t.Error("anderer Pfeffer ergibt dieselbe Kennung")
	}
	if strings.Contains(Kennung(pfeffer, googleSub), googleSub) {
		t.Error("Kennung enthält die Google-Konto-ID im Klartext")
	}
}
