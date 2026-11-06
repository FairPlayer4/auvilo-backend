package kontingent

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Arbeitsspeicher ist der Zähler für Tests. Der Vertragstest stellt sicher, dass
// er sich wie Firestore verhält.
type Arbeitsspeicher struct {
	mu      sync.Mutex // schützt zaehler bei gleichzeitigen Aufrufen
	zaehler map[string]int
}

var _ Zaehler = (*Arbeitsspeicher)(nil)

// NeuerArbeitsspeicher liefert einen Zähler, der überall bei 0 steht.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{zaehler: map[string]int{}}
}

func (a *Arbeitsspeicher) Verbrauche(_ context.Context, art, id string, jetzt time.Time, max int) error {
	if art == "" || id == "" {
		return errors.New("Art und ID sind Pflicht")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	schluessel := dokID(art, id, jetzt)
	if a.zaehler[schluessel] >= max {
		return ErrErschoepft
	}
	a.zaehler[schluessel]++
	return nil
}
