// Package anmeldung prüft das erste Anmelden eines Geräts: Google-ID-Token,
// Bindung an den Geräteschlüssel (Nonce) und Besitz des privaten Schlüssels
// (Signatur). Ablauf und Gründe: docs/b1-entwurf.md.
//
// Das Paket spricht weder mit Firestore noch mit dem Netz (außer über die
// übergebene Schlüsselquelle von Google) und ist dadurch vollständig testbar.
package anmeldung

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
)

// Fehler, die Pruefe liefern kann. Nach außen meldet der Server bei allen nur
// "Anmeldung abgelehnt"; die Unterscheidung dient Tests und Logs.
var (
	ErrToken      = errors.New("Google-ID-Token ungültig")
	ErrZielgruppe = errors.New("ID-Token für eine fremde Client-ID")
	ErrZuAlt      = errors.New("ID-Token zu alt")
	ErrNonce      = errors.New("Nonce passt nicht zum Geräteschlüssel")
	ErrSignatur   = errors.New("Signatur des Geräts ungültig")
)

const (
	// GoogleAussteller ist der Aussteller der Google-ID-Tokens.
	GoogleAussteller = "https://accounts.google.com"
	// GoogleSchluessel ist die Adresse der öffentlichen Schlüssel, mit denen
	// Google die ID-Tokens signiert.
	GoogleSchluessel = "https://www.googleapis.com/oauth2/v3/certs"
)

// MaxAlter gibt an, wie alt ein ID-Token höchstens sein darf. Google stellt Tokens für
// eine Stunde aus; wir verlangen ein frisches, um das Zeitfenster für ein
// abgefangenes Token klein zu halten.
const MaxAlter = 5 * time.Minute

// uhrAbweichung gibt an, wie weit die Uhr von Google vor unserer gehen darf.
const uhrAbweichung = time.Minute

// Pruefer prüft Anmeldungen. Anlegen mit NeuerPruefer.
type Pruefer struct {
	verifier  *oidc.IDTokenVerifier
	clientIDs []string
	jetzt     func() time.Time
}

// NeuerPruefer legt einen Pruefer an.
//   - schluessel: Quelle der Google-Schlüssel; im Betrieb
//     oidc.NewRemoteKeySet(ctx, GoogleSchluessel), in Tests feste Schlüssel.
//   - clientIDs: unsere OAuth-Client-IDs; nur Tokens für diese gelten.
//   - jetzt: Uhr (in Tests fest), nil = time.Now.
func NeuerPruefer(schluessel oidc.KeySet, clientIDs []string, jetzt func() time.Time) (*Pruefer, error) {
	if len(clientIDs) == 0 {
		return nil, errors.New("keine Client-IDs konfiguriert")
	}
	if jetzt == nil {
		jetzt = time.Now
	}
	v := oidc.NewVerifier(GoogleAussteller, schluessel, &oidc.Config{
		// Die Zielgruppe prüfen wir selbst, weil wir mehrere Client-IDs haben
		// (später App und Test-Client).
		SkipClientIDCheck:    true,
		SupportedSigningAlgs: []string{oidc.RS256},
		Now:                  jetzt,
	})
	return &Pruefer{verifier: v, clientIDs: slices.Clone(clientIDs), jetzt: jetzt}, nil
}

// Anfrage enthält die Felder aus AnmeldenRequest.
type Anfrage struct {
	IDToken           string
	Geraeteschluessel []byte // PKIX/DER
	Signatur          []byte // ASN.1/DER
}

// Ergebnis einer erfolgreichen Prüfung.
type Ergebnis struct {
	GoogleSub  string // Google-Konto-ID; wird nie gespeichert, nur als Kennung (HMAC)
	GeraetID   string
	Schluessel *ecdsa.PublicKey
}

// Pruefe prüft eine Anmeldung vollständig.
//
// KRITISCH: Nur wenn ALLE Prüfungen bestehen, gibt es ein Ergebnis ohne Fehler:
//  1. Geräteschlüssel ist ein P-256-Schlüssel
//  2. ID-Token von Google signiert, Aussteller Google, nicht abgelaufen
//  3. Zielgruppe ist eine unserer Client-IDs
//  4. höchstens MaxAlter alt und höchstens uhrAbweichung (1 Minute) in der Zukunft
//  5. Nonce im Token = Nonce des Geräteschlüssels
//  6. Signatur über das Token mit dem Geräteschlüssel
func (p *Pruefer) Pruefe(ctx context.Context, a Anfrage) (Ergebnis, error) {
	schluessel, err := geraeteschluessel.Lese(a.Geraeteschluessel)
	if err != nil {
		return Ergebnis{}, err
	}

	token, err := p.verifier.Verify(ctx, a.IDToken)
	if err != nil {
		return Ergebnis{}, errors.Join(ErrToken, err)
	}
	if token.Subject == "" {
		return Ergebnis{}, fmt.Errorf("%w: ohne sub", ErrToken)
	}
	if !p.istUnsereZielgruppe(token.Audience) {
		return Ergebnis{}, fmt.Errorf("%w: %v", ErrZielgruppe, token.Audience)
	}
	jetzt := p.jetzt()
	if token.IssuedAt.Before(jetzt.Add(-MaxAlter)) || token.IssuedAt.After(jetzt.Add(uhrAbweichung)) {
		return Ergebnis{}, fmt.Errorf("%w: ausgestellt %s", ErrZuAlt, token.IssuedAt.UTC().Format(time.RFC3339))
	}
	// hmac.Equal vergleicht in konstanter Zeit (verrät nichts über Teiltreffer).
	if !hmac.Equal([]byte(token.Nonce), []byte(geraeteschluessel.Nonce(a.Geraeteschluessel))) {
		return Ergebnis{}, ErrNonce
	}
	if !geraeteschluessel.PruefeSignatur(schluessel, geraeteschluessel.AnmeldeNachricht(a.IDToken), a.Signatur) {
		return Ergebnis{}, ErrSignatur
	}

	return Ergebnis{
		GoogleSub:  token.Subject,
		GeraetID:   geraeteschluessel.GeraetID(a.Geraeteschluessel),
		Schluessel: schluessel,
	}, nil
}

// istUnsereZielgruppe: Genau eine Zielgruppe, und die ist eine unserer
// Client-IDs. Tokens mit mehreren Zielgruppen lehnen wir ab – Google stellt sie
// für unseren Fall nicht aus, und so ist die Regel eindeutig.
func (p *Pruefer) istUnsereZielgruppe(aud []string) bool {
	return len(aud) == 1 && slices.Contains(p.clientIDs, aud[0])
}

// Kennung macht aus der Google-Konto-ID den Wert, unter dem das Konto
// gespeichert wird: HMAC-SHA256 mit einem geheimen Pfeffer, hexadezimal.
//
// KRITISCH: Die Google-Konto-ID selbst wird nie gespeichert. Ohne den Pfeffer
// lässt sich die Kennung nicht auf ein Google-Konto zurückführen.
func Kennung(pfeffer []byte, googleSub string) string {
	mac := hmac.New(sha256.New, pfeffer)
	mac.Write([]byte(googleSub))
	return hex.EncodeToString(mac.Sum(nil))
}
