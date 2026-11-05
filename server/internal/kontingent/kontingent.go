// Package kontingent zählt je Konto und Tag, wie oft etwas passiert ist, und
// lehnt ab, sobald eine Grenze erreicht ist (z. B. höchstens 10 Diagnosen am Tag).
// Es schützt die Kosten: Eine fehlerhafte App, die in einer Schleife sendet, kann
// den Bucket nicht unbegrenzt füllen.
//
// Eigenes Paket statt Teil von speicher.Speicher (Review-Empfehlung A5: je
// Aufgabe eine eigene, kleine Ablage). Diagnose-Log und später das Upload-Limit
// der Datensicherung nutzen denselben Zähler mit verschiedener Art.
package kontingent

import (
	"context"
	"errors"
	"time"
)

// ErrErschoepft meldet, dass für diesen Tag die Grenze erreicht ist.
var ErrErschoepft = errors.New("Tageskontingent erschöpft")

// Zaehler zählt je Art, Konto und Tag. Der Tag ist der Kalendertag in UTC; um
// Mitternacht UTC beginnt jedes Konto wieder bei 0.
type Zaehler interface {
	// Verbrauche zählt einen Vorgang, wenn bisher weniger als max gezählt sind.
	// Sonst ErrErschoepft, ohne zu zählen.
	//
	// Die Konto-ID kommt immer aus der geprüften Sitzung.
	Verbrauche(ctx context.Context, art, kontoID string, jetzt time.Time, max int) error
}

// tag liefert den Kalendertag in UTC, z. B. "2026-11-04".
func tag(jetzt time.Time) string { return jetzt.UTC().Format(time.DateOnly) }
