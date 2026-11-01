// Package killswitch begrenzt die Kosten des Projekts hart.
//
// Ein GCP-Budget verschickt nur Meldungen, es stoppt nichts. Diese Funktion
// empfängt die Budget-Meldungen über Pub/Sub und greift ein:
//   - ab Schwelle "Sperren" (Standard 100 %): alle Cloud-Run-Dienste nehmen keine
//     Anfragen von außen mehr an. Daten bleiben erhalten.
//   - ab Schwelle "Trennen" (Standard 150 %): zusätzlich wird das Projekt vom
//     Rechnungskonto getrennt. Dann stoppt alles; Ressourcen können nach einer
//     Frist von Google gelöscht werden.
//
// Die Entscheidung (diese Datei) ist reine Logik ohne Cloud-Zugriff und daher
// vollständig testbar. Die Ausführung steht in ausfuehrung.go.
package killswitch

import (
	"errors"
	"fmt"
	"math"
)

// BudgetMeldung ist der Inhalt einer Pub/Sub-Nachricht des Budgets.
// Die Feldnamen gibt Google vor (JSON-Tags). Felder, die wir nicht brauchen,
// werden beim Einlesen ignoriert.
type BudgetMeldung struct {
	BudgetDisplayName string  `json:"budgetDisplayName"`
	CostAmount        float64 `json:"costAmount"`   // bisherige Kosten im Zeitraum
	BudgetAmount      float64 `json:"budgetAmount"` // Budget im Zeitraum
	CurrencyCode      string  `json:"currencyCode"`
}

// Aktion beschreibt, was der Kill-Switch tun soll. In Go gibt es keine
// Enums wie in C#; üblich ist ein eigener int-Typ mit Konstanten (iota zählt hoch).
type Aktion int

const (
	Nichts            Aktion = iota // unter der ersten Schwelle
	DiensteSperren                  // Cloud Run nimmt keine Anfragen mehr an
	AbrechnungTrennen               // Dienste sperren und Projekt vom Rechnungskonto trennen
)

// String sorgt für lesbare Ausgaben in Logs und Testmeldungen.
func (a Aktion) String() string {
	switch a {
	case Nichts:
		return "nichts"
	case DiensteSperren:
		return "Dienste sperren"
	case AbrechnungTrennen:
		return "Abrechnung trennen"
	default:
		return fmt.Sprintf("Aktion(%d)", int(a))
	}
}

// Schwellen als Anteil des Budgets: 1.0 = 100 %.
type Schwellen struct {
	Sperren float64
	Trennen float64
}

// StandardSchwellen gelten, wenn nichts anderes konfiguriert ist.
var StandardSchwellen = Schwellen{Sperren: 1.0, Trennen: 1.5}

// Fehler, die Aufrufer gezielt prüfen können (errors.Is).
var (
	ErrUngueltigesBudget   = errors.New("Budget muss größer als 0 sein")
	ErrUngueltigeSchwellen = errors.New("Schwellen müssen endliche Zahlen > 0 sein und Sperren <= Trennen")
	ErrUngueltigeKosten    = errors.New("Kosten dürfen nicht negativ sein")
)

// Pruefe stellt sicher, dass die Schwellen sinnvoll sind.
// NaN muss eigens abgefangen werden: Jeder Vergleich mit NaN ist false, NaN
// käme also durch die übrigen Prüfungen, und der Kill-Switch griffe nie.
func (s Schwellen) Pruefe() error {
	endlich := func(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
	if !endlich(s.Sperren) || !endlich(s.Trennen) || s.Sperren <= 0 || s.Trennen <= 0 || s.Sperren > s.Trennen {
		return fmt.Errorf("%w: Sperren=%v, Trennen=%v", ErrUngueltigeSchwellen, s.Sperren, s.Trennen)
	}
	return nil
}

// Entscheide bestimmt anhand der Meldung, welche Aktion nötig ist.
//
// KRITISCH: Bei ungültigen Eingaben gibt es einen Fehler und NICHT stillschweigend "Nichts",
// damit ein Fehler im Format als ERROR im Log auffällt. Eine Wiederholung
// gibt es nicht (RETRY_POLICY_DO_NOT_RETRY); die nächste Budget-Meldung kommt
// ohnehin nach einigen Stunden.
func Entscheide(m BudgetMeldung, s Schwellen) (Aktion, error) {
	if err := s.Pruefe(); err != nil {
		return Nichts, err
	}
	if m.BudgetAmount <= 0 {
		return Nichts, fmt.Errorf("%w: %v", ErrUngueltigesBudget, m.BudgetAmount)
	}
	if m.CostAmount < 0 {
		return Nichts, fmt.Errorf("%w: %v", ErrUngueltigeKosten, m.CostAmount)
	}

	anteil := m.CostAmount / m.BudgetAmount
	switch {
	case anteil >= s.Trennen:
		return AbrechnungTrennen, nil
	case anteil >= s.Sperren:
		return DiensteSperren, nil
	default:
		return Nichts, nil
	}
}
