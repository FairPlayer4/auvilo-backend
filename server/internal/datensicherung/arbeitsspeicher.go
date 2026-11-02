package datensicherung

import (
	"context"
	"sync"
)

// Arbeitsspeicher ist die Ablage für Tests. Der Vertragstest stellt sicher, dass
// sie sich wie Cloud Storage verhält.
type Arbeitsspeicher struct {
	mu         sync.Mutex // schützt die Felder darunter bei gleichzeitigen Aufrufen
	fassungen  map[string]Fassung
	generation int64
}

var _ Ablage = (*Arbeitsspeicher)(nil)

// NeuerArbeitsspeicher liefert eine leere Ablage.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{fassungen: map[string]Fassung{}, generation: 1_000}
}

func (a *Arbeitsspeicher) Speichern(_ context.Context, kontoID string, chiffrat []byte, erwartet int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fassungen[kontoID].Version != erwartet { // fehlt sie, ist Version 0
		return 0, ErrVersionVeraltet
	}
	// Wie in Cloud Storage springt die Version (ein Zähler für alle Konten), damit
	// kein Test und kein Client mit "+ 1" rechnet.
	a.generation += 7
	a.fassungen[kontoID] = Fassung{Chiffrat: append([]byte(nil), chiffrat...), Version: a.generation}
	return a.generation, nil
}

func (a *Arbeitsspeicher) Holen(_ context.Context, kontoID string) (Fassung, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fassungen[kontoID]
	if !ok {
		return Fassung{}, ErrNichtGefunden
	}
	return Fassung{Chiffrat: append([]byte(nil), f.Chiffrat...), Version: f.Version}, nil
}

func (a *Arbeitsspeicher) Loeschen(_ context.Context, kontoID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fassungen, kontoID)
	return nil
}
