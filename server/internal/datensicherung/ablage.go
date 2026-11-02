// Package datensicherung legt die verschlüsselten Datensicherungen ab (Etappe B2b).
// Je Konto gibt es genau eine Sicherung. Der Server versteht ihren Inhalt nicht;
// er achtet nur darauf, dass kein Gerät eine neuere Fassung unbemerkt überschreibt.
//
// Eigene Schnittstelle statt einer Erweiterung von speicher.Speicher: Die
// Sicherungen liegen in Cloud Storage, nicht in Firestore (Review-Empfehlung A5,
// je Dienst eine eigene Ablage).
package datensicherung

import (
	"context"
	"errors"
)

// MaxChiffrat ist die größte erlaubte Sicherung (docs/b2b-entwurf.md, Messung:
// etwa 0,7 MiB je Jahr bei normaler Nutzung).
const MaxChiffrat = 8 << 20 // 8 MiB

var (
	// ErrVersionVeraltet: Die erwartete Version passt nicht zur gespeicherten.
	ErrVersionVeraltet = errors.New("Version der Sicherung veraltet")
	// ErrNichtGefunden: Für das Konto gibt es keine Sicherung.
	ErrNichtGefunden = errors.New("keine Sicherung")
)

// Fassung ist die gespeicherte Sicherung eines Kontos.
type Fassung struct {
	Chiffrat []byte
	// Version wächst mit jedem Speichern, aber nicht um 1 (in Cloud Storage ist es
	// die "Generation" des Objekts, eine Art Zeitstempel).
	Version int64
}

// Ablage speichert eine Sicherung je Konto. Die Konto-ID kommt immer aus der
// geprüften Sitzung.
type Ablage interface {
	// Speichern legt chiffrat ab, wenn die gespeicherte Version gleich erwartet ist
	// (0 = es gibt noch keine), und liefert die neue Version. Sonst
	// ErrVersionVeraltet, ohne etwas zu ändern.
	Speichern(ctx context.Context, kontoID string, chiffrat []byte, erwartet int64) (int64, error)
	// Holen liefert die neueste Fassung oder ErrNichtGefunden.
	Holen(ctx context.Context, kontoID string) (Fassung, error)
	// Loeschen entfernt die Sicherung mit allen älteren Fassungen. Gibt es keine,
	// ist das kein Fehler.
	Loeschen(ctx context.Context, kontoID string) error
}
