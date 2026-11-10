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
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

// zufall ist der Zufallsanteil der Nonce in den Testanfragen (16 Bytes).
var zufall = []byte("0123456789abcdef")

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
		"iss":   "https://accounts.google.com", // eigene Zahl statt GoogleAussteller
		"aud":   unsereID,
		"sub":   googleSub,
		"iat":   jetzt.Add(-time.Minute).Unix(),
		"exp":   jetzt.Add(time.Hour).Unix(),
		"nonce": geraeteschluessel.Nonce(u.geraetDER, zufall),
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
		NonceZufall:       zufall,
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
			return u.anfrage(t, mit(gut, "nonce", geraeteschluessel.Nonce(anderesDER, zufall)))
		}, ErrNonce},
		{"lehnt Token mit Nonce eines anderen Zufallsanteils ab", func() Anfrage {
			a := u.anfrage(t, gut)
			a.NonceZufall = []byte("fedcba9876543210")
			return a
		}, ErrNonce},
		// Die alte, feste Nonce (nur der Schlüssel) gilt nicht mehr.
		{"lehnt Token mit Nonce ohne Zufallsanteil ab", func() Anfrage {
			s := sha256.Sum256(u.geraetDER)
			a := u.anfrage(t, mit(gut, "nonce", base64.RawURLEncoding.EncodeToString(s[:])))
			a.NonceZufall = nil
			return a
		}, ErrZufall},
		{"lehnt zu kurzen Zufallsanteil ab, auch wenn die Nonce dazu passt", func() Anfrage {
			kurz := zufall[:15]
			a := u.anfrage(t, mit(gut, "nonce", geraeteschluessel.Nonce(u.geraetDER, kurz)))
			a.NonceZufall = kurz
			return a
		}, ErrZufall},
		{"lehnt zu langen Zufallsanteil ab, auch wenn die Nonce dazu passt", func() Anfrage {
			lang := append([]byte("x"), zufall...)
			a := u.anfrage(t, mit(gut, "nonce", geraeteschluessel.Nonce(u.geraetDER, lang)))
			a.NonceZufall = lang
			return a
		}, ErrZufall},
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

// KRITISCH: Scheitert das Laden der Google-Schlüssel, ist das keine
// abgelehnte Anmeldung (E14): Pruefe meldet ErrSchluesselNichtLadbar, und der
// Server antwortet "unavailable". Geprüft gegen einen nachgebauten
// Schlüssel-Endpunkt mit echtem oidc.RemoteKeySet, damit eine neue Version
// von go-oidc, die Fehler anders verpackt, hier auffällt.
func TestSchluesselVon(t *testing.T) {
	u := neueUmgebung(t)
	jwks, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &u.google.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	gueltig := u.anfrage(t, u.ansprueche())
	fremd := u.anfrage(t, u.ansprueche())
	fremd.IDToken = signiereToken(t, u.fremd, u.ansprueche())
	fremd.Signatur = signiere(t, u.geraet, geraeteschluessel.AnmeldeNachricht(fremd.IDToken))

	faelle := []struct {
		name     string
		status   int    // Antwort des Endpunkts; 0 = nicht erreichbar
		inhalt   []byte // bei Status 200
		anfrage  Anfrage
		wantErr  error // nil: Anmeldung gelingt
		nichtErr error // dieser Fehler darf es nicht sein
	}{
		{"Google antwortet 503", http.StatusServiceUnavailable, nil, gueltig, ErrSchluesselNichtLadbar, ErrToken},
		{"Google liefert kaputtes JSON", http.StatusOK, []byte("{kaputt"), gueltig, ErrSchluesselNichtLadbar, ErrToken},
		{"Google nicht erreichbar", 0, nil, gueltig, ErrSchluesselNichtLadbar, ErrToken},
		{"Schlüssel geladen, Token gültig", http.StatusOK, jwks, gueltig, nil, nil},
		{"Schlüssel geladen, Token mit fremdem Schlüssel abgelehnt", http.StatusOK, jwks, fremd, ErrToken, ErrSchluesselNichtLadbar},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(fall.status)
				_, _ = w.Write(fall.inhalt)
			}))
			if fall.status == 0 {
				srv.Close() // Port zu: Verbindung abgelehnt
			} else {
				defer srv.Close()
			}
			ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 5 * time.Second})
			p, err := NeuerPruefer(SchluesselVon(ctx, srv.URL), []string{unsereID}, func() time.Time { return jetzt })
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Pruefe(context.Background(), fall.anfrage)
			if fall.wantErr == nil {
				if err != nil {
					t.Fatalf("Anmeldung abgelehnt: %v", err)
				}
				return
			}
			if !errors.Is(err, fall.wantErr) {
				t.Fatalf("Fehler = %v, erwartet %v", err, fall.wantErr)
			}
			if errors.Is(err, fall.nichtErr) {
				t.Errorf("Fehler %v enthält auch %v", err, fall.nichtErr)
			}
		})
	}
}
