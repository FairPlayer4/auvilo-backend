// Package sitzung stellt Sitzungs-Tokens aus und prüft sie: beim Erneuern
// (Signatur des Geräts) und bei jeder Anfrage (Interceptor).
//
// Ein Sitzungs-Token ist ein Zufallswert. Gespeichert wird nur sein SHA-256;
// geprüft wird jede Anfrage gegen den Speicher. Gesperrte Geräte sind dadurch
// sofort draußen (Entscheidung in docs/anforderungen.md).
package sitzung

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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

// Neu erzeugt ein Token (256 Bit Zufall) und die Daten zum Speichern.
func Neu(jetzt time.Time) (token string, s speicher.NeueSitzung) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // liefert seit Go 1.24 nie einen Fehler
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, speicher.NeueSitzung{Hash: Hash(token), LaeuftAb: jetzt.Add(Dauer)}
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
