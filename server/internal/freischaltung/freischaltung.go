// Package freischaltung prüft, ob ein Gerät den Wiederherstellungsschlüssel
// kennt, ohne dass der Server ihn je sieht (E-B2b-13, docs/b2b-entwurf.md).
//
// Ablauf: Aus dem Wiederherstellungsschlüssel leitet die App einen
// Ed25519-Schlüssel ab (client/protokoll, Wiederherstellungsschluessel). Den
// öffentlichen Teil, den Freischaltschlüssel, lädt das erste Gerät mit dem
// Schlüssel-Backup hoch. Ein neues Gerät signiert damit seine Geräte-ID; der
// Server prüft die Signatur hier. Gemeinsame Testvektoren mit Kotlin in
// proto/testvektoren/freischaltung.json.
package freischaltung

import (
	"crypto/ed25519"
	"errors"
)

// SchluesselLaenge ist die Länge eines Freischaltschlüssels (Ed25519, öffentlich).
const SchluesselLaenge = ed25519.PublicKeySize

// zweck steht vor der Geräte-ID. So passt diese Signatur nie für etwas anderes.
const zweck = "avlreminder/freischalten/v1\n"

// ErrSignatur meldet: Schlüssel oder Signatur haben die falsche Länge, oder die
// Signatur stimmt nicht.
var ErrSignatur = errors.New("Signatur der Freischaltung ungültig")

// Nachricht sind die Bytes, die das Gerät signiert.
func Nachricht(geraetID string) []byte {
	return []byte(zweck + geraetID)
}

// Pruefe prüft die Signatur eines Geräts über seine eigene Geräte-ID.
//
// KRITISCH:
//   - Nur eine gültige Ed25519-Signatur mit genau diesem Freischaltschlüssel
//     über genau diese Geräte-ID wird angenommen. Die Signatur für ein anderes
//     Gerät schaltet dieses nicht frei.
//   - Falsche Längen werden abgelehnt (ed25519.Verify bräche sonst ab).
func Pruefe(freischaltschluessel []byte, geraetID string, signatur []byte) error {
	if len(freischaltschluessel) != ed25519.PublicKeySize || len(signatur) != ed25519.SignatureSize {
		return ErrSignatur
	}
	if !ed25519.Verify(freischaltschluessel, Nachricht(geraetID), signatur) {
		return ErrSignatur
	}
	return nil
}
