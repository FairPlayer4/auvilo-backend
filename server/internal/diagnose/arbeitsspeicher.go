package diagnose

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"time"
)

// Arbeitsspeicher ist die Ablage für Tests. Der Vertragstest stellt sicher, dass
// sie sich wie Cloud Storage verhält.
type Arbeitsspeicher struct {
	mu      sync.Mutex // schützt objekte bei gleichzeitigen Aufrufen
	objekte map[string][]byte
}

var _ Ablage = (*Arbeitsspeicher)(nil)

// NeuerArbeitsspeicher liefert eine leere Ablage.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{objekte: map[string][]byte{}}
}

func (a *Arbeitsspeicher) Ablegen(_ context.Context, kontoID string, jetzt time.Time, nutzlast []byte) (string, error) {
	if kontoID == "" {
		return "", errors.New("Konto-ID fehlt")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	name := objektname(kontoID, jetzt)
	if _, gibtEs := a.objekte[name]; gibtEs {
		return "", errors.New("Sendung gibt es schon")
	}
	a.objekte[name] = append([]byte(nil), nutzlast...)
	return name, nil
}

func (a *Arbeitsspeicher) Loeschen(_ context.Context, kontoID string) error {
	if kontoID == "" {
		return errors.New("Konto-ID fehlt")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for name := range a.objekte {
		if strings.HasPrefix(name, ordner(kontoID)) {
			delete(a.objekte, name)
		}
	}
	return nil
}

// Objekte liefert eine Kopie aller Sendungen (nur für Tests).
func (a *Arbeitsspeicher) Objekte() map[string][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return maps.Clone(a.objekte)
}
