// Package geraeteschluessel enthält alles rund um den Geräteschlüssel: einlesen,
// Geräte-ID ableiten, Nonce für den Google-Login und die Texte, die ein Gerät
// signiert. Die Kotlin-Seite (client/protokoll, Geraetenachrichten) baut dieselben
// Bytes; gemeinsame Testvektoren in proto/testvektoren prüfen, dass beide gleich sind.
//
// Der Geräteschlüssel ist ein ECDSA-Schlüsselpaar auf der Kurve P-256
// (Entscheidung in docs/anforderungen.md). Der private Teil bleibt auf dem Gerät
// (Android Keystore); der Server kennt nur den öffentlichen Teil.
package geraeteschluessel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

// ErrUngueltigerSchluessel meldet: kein öffentlicher ECDSA-Schlüssel auf P-256.
var ErrUngueltigerSchluessel = errors.New("ungültiger Geräteschlüssel")

// Zweck-Präfixe: Jede Signatur beginnt mit dem Zweck. So passt eine Signatur für
// die Anmeldung nie als Signatur für eine Sitzung und umgekehrt.
const (
	zweckAnmelden = "avlreminder/anmelden/v1\n"
	zweckSitzung  = "avlreminder/sitzung/v1\n"
)

// Lese wandelt den öffentlichen Schlüssel aus der Anfrage (PKIX/DER, das
// Standardformat von Android und Java) in einen Go-Schlüssel.
//
// KRITISCH: Nur P-256 wird angenommen. Andere Kurven oder Verfahren (RSA,
// Ed25519) werden abgelehnt, damit es genau ein geprüftes Verfahren gibt.
func Lese(der []byte) (*ecdsa.PublicKey, error) {
	if len(der) == 0 {
		return nil, ErrUngueltigerSchluessel
	}
	schluessel, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.Join(ErrUngueltigerSchluessel, err)
	}
	// Typprüfung in Go: "wert, ok := x.(Typ)" ist wie "x as Typ" in C#.
	ec, ok := schluessel.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, ErrUngueltigerSchluessel
	}
	return ec, nil
}

// GeraetID leitet die Geräte-ID aus dem öffentlichen Schlüssel ab (die ersten
// 16 Bytes von SHA-256, hexadezimal). Derselbe Schlüssel ergibt also immer
// dasselbe Gerät; ein erneutes Anmelden legt kein zweites Gerät an.
func GeraetID(der []byte) string {
	summe := sha256.Sum256(der)
	return hex.EncodeToString(summe[:16])
}

// IstGeraetID sagt, ob s das Format hat, das GeraetID erzeugt: genau 32
// Zeichen 0-9 und a-f. So erreichen nur solche IDs den Speicher.
func IstGeraetID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Nonce ist der Wert, den das Gerät beim Google-Login als "nonce" mitschickt.
// Google übernimmt ihn ins ID-Token; der Server vergleicht. So gehört ein
// ID-Token zu genau einem Geräteschlüssel – ein abgefangenes Token nützt mit
// einem anderen Schlüssel nichts.
func Nonce(der []byte) string {
	summe := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(summe[:])
}

// AnmeldeNachricht sind die Bytes, die das Gerät beim Anmelden signiert.
func AnmeldeNachricht(idToken string) []byte {
	return []byte(zweckAnmelden + idToken)
}

// SitzungsNachricht sind die Bytes, die das Gerät zum Erneuern der Sitzung
// signiert. Der Zeitpunkt zählt in ganzen Sekunden.
func SitzungsNachricht(geraetID string, zeitpunkt time.Time) []byte {
	return []byte(zweckSitzung + geraetID + "\n" + strconv.FormatInt(zeitpunkt.Unix(), 10))
}

// PruefeSignatur prüft eine ECDSA-Signatur (ASN.1/DER, wie Android sie liefert)
// über SHA-256 der Nachricht.
//
// KRITISCH: Gibt nur bei gültiger Signatur true zurück.
func PruefeSignatur(schluessel *ecdsa.PublicKey, nachricht, signatur []byte) bool {
	summe := sha256.Sum256(nachricht)
	return ecdsa.VerifyASN1(schluessel, summe[:], signatur)
}
