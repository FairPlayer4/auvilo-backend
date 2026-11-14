// Package sitzung stellt Sitzungs-Tokens aus und prüft sie: beim Erneuern
// (Signatur des Geräts) und bei jeder Anfrage (Interceptor).
//
// Ein Sitzungs-Token ist ein Zufallswert mit einer Signatur des Servers
// (HMAC, ID8 in docs/anforderungen.md). Gespeichert wird nur sein SHA-256;
// geprüft wird jede Anfrage gegen den Speicher. Gesperrte Geräte sind dadurch
// sofort draußen (Entscheidung in docs/anforderungen.md). Die Signatur prüft
// der Server vorher ohne Speicher: Erfundene Tokens kosten so keinen Zugriff
// auf Firestore.
package sitzung

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Dauer gibt an, wie lange eine Sitzung gilt.
const Dauer = time.Hour

// Toleranz gibt an, wie weit die Uhr des Geräts beim Erneuern abweichen darf. Größer
// heißt: eine abgefangene Erneuerung bleibt länger verwendbar.
const Toleranz = 2 * time.Minute

// Fehler beim Erneuern. Nach außen meldet der Server "Erneuern abgelehnt".
var (
	ErrGesperrt  = errors.New("Gerät ist gesperrt")
	ErrZeitpunkt = errors.New("Zeitpunkt weicht zu stark ab")
	ErrSignatur  = errors.New("Signatur ungültig")
)

// zweckToken trennt den Schlüssel der Tokens von anderen Verwendungen des
// Pfeffers (die Kennung ist HMAC(Pfeffer, Google-Konto-ID)).
const zweckToken = "avlreminder/sitzungstoken/v1"

// Aussteller stellt Tokens aus und erkennt die eigenen (ID8).
type Aussteller struct {
	schluessel []byte
}

// NeuerAussteller leitet den Schlüssel für die Tokens aus dem Pfeffer ab. Ein
// eigenes Geheimnis bräuchte einen weiteren Eintrag im Secret Manager; so
// ersetzt die Rotation des Pfeffers (Sicherheitsphase) auch diesen Schlüssel.
// Danach gelten alte Tokens nicht mehr, und die Geräte erneuern ihre Sitzung.
func NeuerAussteller(pfeffer []byte) Aussteller {
	mac := hmac.New(sha256.New, pfeffer)
	mac.Write([]byte(zweckToken))
	return Aussteller{schluessel: mac.Sum(nil)}
}

// signatur ist der HMAC über den Zufallsteil, gekürzt auf 128 Bit (genug
// gegen Raten, und das Token bleibt kurz).
func (a Aussteller) signatur(zufall []byte) []byte {
	mac := hmac.New(sha256.New, a.schluessel)
	mac.Write(zufall)
	return mac.Sum(nil)[:16]
}

// Neu erzeugt ein Token und die Daten zum Speichern. Aufbau:
// base64url(256 Bit Zufall) + "." + base64url(Signatur), ohne Auffüllung.
func (a Aussteller) Neu(jetzt time.Time) (token string, s speicher.NeueSitzung) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // liefert seit Go 1.24 nie einen Fehler
	token = base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(a.signatur(b))
	return token, speicher.NeueSitzung{Hash: Hash(token), LaeuftAb: jetzt.Add(Dauer)}
}

// Echt sagt, ob das Token von diesem Aussteller stammt. Es sagt nicht, ob
// die Sitzung noch gilt; das weiß nur der Speicher.
//
// KRITISCH: Vergleich in konstanter Zeit (hmac.Equal), sonst ließe sich die
// Signatur über die Antwortzeit Byte für Byte erraten.
func (a Aussteller) Echt(token string) bool {
	zufallText, sigText, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	zufall, err := base64.RawURLEncoding.DecodeString(zufallText)
	if err != nil || len(zufall) != 32 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigText)
	if err != nil {
		return false
	}
	return hmac.Equal(sig, a.signatur(zufall))
}

// Hash ist der Wert, unter dem eine Sitzung gespeichert wird.
func Hash(token string) string {
	summe := sha256.Sum256([]byte(token))
	return hex.EncodeToString(summe[:])
}

// PruefeErneuerung prüft, ob ein Gerät eine neue Sitzung bekommt.
//
// KRITISCH: Nur ein nicht gesperrtes Gerät mit gültiger Signatur über seine
// Geräte-ID und einen Zeitpunkt nahe der Serverzeit bekommt eine Sitzung. Die
// Sperre wird zuletzt geprüft: Nur wer den Geräteschlüssel hat, erfährt, dass
// das Gerät gesperrt ist.
func PruefeErneuerung(g speicher.Geraet, zeitpunkt time.Time, signatur []byte, jetzt time.Time) error {
	abstand := jetzt.Sub(zeitpunkt).Abs()
	if abstand > Toleranz {
		return ErrZeitpunkt
	}
	schluessel, err := geraeteschluessel.Lese(g.Schluessel)
	if err != nil {
		return err
	}
	if !geraeteschluessel.PruefeSignatur(schluessel, geraeteschluessel.SitzungsNachricht(g.ID, zeitpunkt), signatur) {
		return ErrSignatur
	}
	if g.Gesperrt {
		return ErrGesperrt
	}
	return nil
}
