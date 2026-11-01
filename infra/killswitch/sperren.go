package killswitch

import (
	"context"
	"errors"
	"fmt"
)

// Dienst ist das, was die Sperr-Logik über einen Cloud-Run-Dienst wissen muss.
type Dienst struct {
	Name   string // voller Name, z. B. "projects/p/locations/r/services/api"
	Intern bool   // Ingress steht schon auf "nur intern"
}

// CloudRun kapselt die beiden Cloud-Run-Aufrufe, die das Sperren braucht.
// Die echte Umsetzung steht in gcp.go, die Tests nutzen eine Attrappe. So
// bleibt die Logik in SperreAlle ohne GCP testbar.
type CloudRun interface {
	// Dienste listet alle Cloud-Run-Dienste der Region.
	Dienste(ctx context.Context) ([]Dienst, error)
	// Sperre stellt den Dienst mit diesem vollen Namen auf "nur intern".
	Sperre(ctx context.Context, name string) error
}

// SperreAlle stellt alle offenen Cloud-Run-Dienste auf "nur intern" und gibt
// zurück, wie viele es waren. Anfragen aus dem Internet werden dann
// abgewiesen, ohne dass eine Instanz startet – es entstehen keine
// Rechenkosten mehr.
//
// KRITISCH:
//   - Der eigene Dienst (die Funktion selbst) wird nie gesperrt.
//   - Schon interne Dienste werden nicht angefasst.
//   - Ein Fehler bei einem Dienst hält die anderen nicht auf; alle Fehler
//     werden gesammelt zurückgegeben.
//   - Scheitert schon das Auflisten, wird nichts gesperrt und der Fehler gemeldet.
func SperreAlle(ctx context.Context, cr CloudRun, eigenerDienst string) (int, error) {
	dienste, err := cr.Dienste(ctx)
	if err != nil {
		return 0, fmt.Errorf("Dienste auflisten: %w", err)
	}

	gesperrt := 0
	var fehler []error
	for _, d := range dienste {
		if kurzname(d.Name) == eigenerDienst || d.Intern {
			continue
		}
		if err := cr.Sperre(ctx, d.Name); err != nil {
			fehler = append(fehler, fmt.Errorf("%s: %w", d.Name, err))
			continue
		}
		gesperrt++
	}
	// errors.Join gibt nil zurück, wenn die Liste leer ist.
	return gesperrt, errors.Join(fehler...)
}

// kurzname macht aus "projects/p/locations/r/services/name" den Namen "name".
func kurzname(voll string) string {
	for i := len(voll) - 1; i >= 0; i-- {
		if voll[i] == '/' {
			return voll[i+1:]
		}
	}
	return voll
}
