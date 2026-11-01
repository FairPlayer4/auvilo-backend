package killswitch

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// sperrZeitlimit begrenzt das Sperren vor dem Trennen. Die Funktion hat 120 s
// (infra/kosten.tf); hängt das Sperren, bleibt so genug Zeit zum Trennen.
// Eine Variable statt Konstante, damit Tests sie verkürzen können.
var sperrZeitlimit = 60 * time.Second

// Plattform kapselt die Eingriffe in GCP. Das Interface steht hier beim
// Verwender (Go-Stil): Die echte Umsetzung liegt in gcp.go, die Tests nutzen
// eine Attrappe. Go-Interfaces werden implizit erfüllt – es gibt kein
// "implements".
type Plattform interface {
	// DiensteSperren sperrt alle Cloud-Run-Dienste für Anfragen von außen und
	// gibt zurück, wie viele Dienste geändert wurden.
	DiensteSperren(ctx context.Context) (int, error)
	// AbrechnungTrennen trennt das Projekt vom Rechnungskonto.
	AbrechnungTrennen(ctx context.Context) error
}

// Ergebnis beschreibt, was tatsächlich passiert ist (fürs Log).
type Ergebnis struct {
	Aktion             Aktion
	GesperrteDienste   int
	AbrechnungGetrennt bool
}

// Ausfuehren führt die Aktion aus.
//
// KRITISCH: Beim Trennen werden vorher die Dienste gesperrt: Falls das Trennen scheitert,
// ist so wenigstens der Zugriff von außen schon zu. Scheitert das Sperren, wird
// trotzdem getrennt – Kosten stoppen geht vor. Auch ein hängendes Sperren darf
// das Trennen nicht verhindern: Es bekommt ein eigenes, kürzeres Zeitlimit.
func Ausfuehren(ctx context.Context, a Aktion, p Plattform) (Ergebnis, error) {
	erg := Ergebnis{Aktion: a}
	switch a {
	case Nichts:
		return erg, nil

	case DiensteSperren:
		n, err := p.DiensteSperren(ctx)
		erg.GesperrteDienste = n
		if err != nil {
			return erg, fmt.Errorf("Dienste sperren: %w", err)
		}
		return erg, nil

	case AbrechnungTrennen:
		sperrCtx, abbrechen := context.WithTimeout(ctx, sperrZeitlimit)
		n, sperrFehler := p.DiensteSperren(sperrCtx)
		abbrechen() // gibt den Timer frei; ctx fürs Trennen bleibt unberührt
		erg.GesperrteDienste = n
		if err := p.AbrechnungTrennen(ctx); err != nil {
			trennFehler := fmt.Errorf("Abrechnung trennen: %w", err)
			if sperrFehler != nil {
				// errors.Join hält beide Ursachen fest; errors.Is findet jede davon.
				return erg, errors.Join(trennFehler, fmt.Errorf("Dienste sperren: %w", sperrFehler))
			}
			return erg, trennFehler
		}
		erg.AbrechnungGetrennt = true
		if sperrFehler != nil {
			// Abrechnung ist getrennt, damit stoppen die Kosten ohnehin.
			// Den Fehler trotzdem melden, damit er im Log auffällt.
			return erg, fmt.Errorf("Dienste sperren: %w", sperrFehler)
		}
		return erg, nil

	default:
		return erg, fmt.Errorf("unbekannte Aktion %v", a)
	}
}
