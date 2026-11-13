// Package diagnose legt die Diagnose-Logs aus der App ab (docs/diagnose-entwurf.md).
// Jede Sendung wird ein eigenes Objekt; nichts wird überschrieben. Der Server
// liest die Sendungen nie (er darf es im Betrieb auch nicht: anlegen, Namen
// auflisten und löschen, aber nicht lesen); Kevin holt sie mit
// werkzeuge/diagnose-holen.sh.
package diagnose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// zufall füllt den Zufallsteil des Namens. Nur Tests ersetzen ihn, um zu prüfen,
// dass ein schon vorhandener Name nie überschrieben wird.
var zufall = rand.Read

// MaxNutzlast ist die größte erlaubte Sendung (gzip-komprimiert).
const MaxNutzlast = 256 << 10 // 256 KiB

// Ablage nimmt Sendungen entgegen. Die Konto-ID kommt immer aus der geprüften
// Sitzung.
type Ablage interface {
	// Ablegen speichert eine Sendung unter einem neuen Namen (objektname) und
	// liefert diesen zurück. Eine vorhandene Sendung wird nie überschrieben.
	Ablegen(ctx context.Context, kontoID string, jetzt time.Time, nutzlast []byte) (string, error)

	// Loeschen entfernt alle Sendungen des Kontos (Konto löschen, B1). Ohne
	// Sendungen ist das kein Fehler.
	//
	// KRITISCH: nur die Sendungen dieses Kontos, auch wenn eine andere
	// Konto-ID mit derselben Zeichenfolge beginnt.
	Loeschen(ctx context.Context, kontoID string) error
}

// ordner: Präfix aller Sendungen eines Kontos. Der Schrägstrich am Ende trennt
// Konto "a1" von Konto "a10".
func ordner(kontoID string) string {
	return "diagnose/" + kontoID + "/"
}

// objektname: "diagnose/<konto>/<zeit>-<zufall>.jsonl.gz". Der Ordner je Konto
// erlaubt, beim Löschen eines Kontos auch seine Diagnosen zu löschen (E-D-4);
// die Zeit vorn sortiert die Sendungen; der Zufall verhindert, dass zwei
// Sendungen in derselben Sekunde denselben Namen bekommen. Die Konto-ID ist
// zufällig (128 Bit), kein Name und keine E-Mail.
func objektname(kontoID string, jetzt time.Time) string {
	b := make([]byte, 4)
	_, _ = zufall(b) // rand.Read liefert seit Go 1.24 nie einen Fehler
	return ordner(kontoID) + jetzt.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b) + ".jsonl.gz"
}
