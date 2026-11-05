package killswitch

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cloud.google.com/go/iam/apiv1/iampb"
)

// Dienst ist das, was die Sperr-Logik über einen Cloud-Run-Dienst wissen muss.
type Dienst struct {
	Name     string // voller Name, z. B. "projects/p/locations/r/services/api"
	Gesperrt bool   // niemand aus dem Internet darf ihn aufrufen (oeffentlich fehlt)
	// OhneAufrufpruefung: Cloud Run prüft das Aufrufrecht gar nicht
	// (invokerIamDisabled). Dann ist der Dienst ohne "allUsers" offen, und
	// die IAM-Richtlinie zu ändern sperrt nichts.
	OhneAufrufpruefung bool
}

// CloudRun kapselt die beiden Cloud-Run-Aufrufe, die das Sperren braucht.
// Die echte Umsetzung steht in gcp.go, die Tests nutzen eine Attrappe. So
// bleibt die Logik in SperreAlle ohne GCP testbar.
type CloudRun interface {
	// Dienste listet alle Cloud-Run-Dienste der Region.
	Dienste(ctx context.Context) ([]Dienst, error)
	// Sperre entzieht dem Internet das Recht, den Dienst mit diesem vollen
	// Namen aufzurufen (siehe ohneOeffentlich). Gibt zurück, ob sich dabei
	// etwas geändert hat (false: war inzwischen schon gesperrt).
	Sperre(ctx context.Context, name string) (bool, error)
}

// oeffentlich sind die beiden IAM-Mitglieder, über die jeder aus dem Internet
// einen Dienst aufrufen darf: "allUsers" (jeder) und "allAuthenticatedUsers"
// (jedes Google-Konto).
var oeffentlich = []string{"allUsers", "allAuthenticatedUsers"}

// istOeffentlich sagt, ob die IAM-Richtlinie eines Dienstes dem Internet
// irgendeine Rolle gibt.
func istOeffentlich(p *iampb.Policy) bool {
	for _, b := range p.GetBindings() {
		for _, m := range b.GetMembers() {
			if slices.Contains(oeffentlich, m) {
				return true
			}
		}
	}
	return false
}

// ohneOeffentlich entfernt "allUsers" und "allAuthenticatedUsers" aus allen
// Rollen der Richtlinie und gibt zurück, ob sich etwas geändert hat. Rollen,
// in denen dann niemand mehr steht, fallen weg (die API lehnt leere ab).
//
// Warum so statt Ingress "nur intern": Den Ingress zu ändern ist ein Update des
// Dienstes, und dafür verlangt Cloud Run das Recht, dessen Laufzeitkonto zu
// "benutzen" (actAs). Mit diesem Recht könnte kompromittierter Code im
// Kill-Switch ein fremdes Image als Konto "server" starten. Die IAM-Richtlinie
// zu ändern braucht kein actAs. Abgewiesen wird dann schon vor dem Dienst
// (403), es startet keine Instanz und es entstehen keine Rechenkosten.
//
// KRITISCH:
//   - Entfernt wird aus jeder Rolle, nicht nur aus run.invoker; sonst bliebe
//     ein Dienst über eine andere Rolle mit Aufrufrecht offen.
//   - Alle anderen Mitglieder (z. B. das Konto, das den Kill-Switch aufruft)
//     bleiben, damit der Kill-Switch sich nicht selbst aussperrt.
func ohneOeffentlich(p *iampb.Policy) bool {
	geaendert := false
	var bleiben []*iampb.Binding
	for _, b := range p.GetBindings() {
		vorher := len(b.Members)
		b.Members = slices.DeleteFunc(b.Members, func(m string) bool {
			return slices.Contains(oeffentlich, m)
		})
		if len(b.Members) != vorher {
			geaendert = true
		}
		if len(b.Members) > 0 {
			bleiben = append(bleiben, b)
		}
	}
	if p != nil {
		p.Bindings = bleiben
	}
	return geaendert
}

// ErrOhneAufrufpruefung meldet einen Dienst, der das Aufrufrecht nicht prüft
// (invokerIamDisabled) und damit auch ohne "allUsers" offen ist.
var ErrOhneAufrufpruefung = errors.New("Aufrufprüfung abgeschaltet (invokerIamDisabled): Dienst ist offen und lässt sich so nicht sperren")

// Sperrung zählt, was SperreAlle vorgefunden und getan hat.
type Sperrung struct {
	// Gefunden zählt alle Dienste außer dem eigenen, auch schon gesperrte.
	// 0 heißt: Der Kill-Switch schützt nichts (z. B. falsche REGION).
	Gefunden int
	// Gesperrt zählt die Dienste, die jetzt neu für das Internet gesperrt sind.
	Gesperrt int
}

// SperreAlle sperrt alle offenen Cloud-Run-Dienste für das Internet und gibt
// zurück, wie viele Dienste es gefunden und gesperrt hat. Anfragen aus dem
// Internet werden dann abgewiesen, ohne dass eine Instanz startet – es
// entstehen keine Rechenkosten mehr.
//
// KRITISCH:
//   - Der eigene Dienst (die Funktion selbst) wird nie gesperrt.
//   - Schon gesperrte Dienste werden nicht angefasst.
//   - Ein Fehler bei einem Dienst hält die anderen nicht auf; alle Fehler
//     werden gesammelt zurückgegeben.
//   - Scheitert schon das Auflisten, wird nichts gesperrt und der Fehler gemeldet.
func SperreAlle(ctx context.Context, cr CloudRun, eigenerDienst string) (Sperrung, error) {
	var s Sperrung
	dienste, err := cr.Dienste(ctx)
	if err != nil {
		return s, fmt.Errorf("Dienste auflisten: %w", err)
	}

	var fehler []error
	for _, d := range dienste {
		if kurzname(d.Name) == eigenerDienst {
			continue
		}
		s.Gefunden++
		// KRITISCH: Ohne Aufrufprüfung kann der Kill-Switch den Dienst nicht
		// sperren (dafür müsste er ihn ändern, und das darf er bewusst nicht).
		// Ein Fehler löst die Alarm-Mail aus, statt still „gesperrt“ zu melden.
		if d.OhneAufrufpruefung {
			fehler = append(fehler, fmt.Errorf("%s: %w", d.Name, ErrOhneAufrufpruefung))
			continue
		}
		if d.Gesperrt {
			continue
		}
		geaendert, err := cr.Sperre(ctx, d.Name)
		if err != nil {
			fehler = append(fehler, fmt.Errorf("%s: %w", d.Name, err))
			continue
		}
		if geaendert {
			s.Gesperrt++
		}
	}
	// errors.Join gibt nil zurück, wenn die Liste leer ist.
	return s, errors.Join(fehler...)
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
