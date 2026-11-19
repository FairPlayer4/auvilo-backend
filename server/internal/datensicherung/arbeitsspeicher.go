package datensicherung

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// Arbeitsspeicher ist die Ablage für Tests. Der Vertragstest stellt sicher, dass
// sie sich wie Cloud Storage verhält. Anders als dort bleiben ältere Fassungen
// bis zum Löschen (der Lebenszyklus des Buckets gehört nicht zum Vertrag).
type Arbeitsspeicher struct {
	mu sync.Mutex // schützt die Felder darunter bei gleichzeitigen Aufrufen
	// fassungen je Konto, die älteste zuerst; die letzte ist die aktuelle.
	fassungen  map[string][]gespeichert
	generation int64
	// Uhr liefert den Zeitpunkt des Speicherns (nil = time.Now). Tests stellen
	// sie, um „vor einer Stunde gespeichert“ nachzubauen.
	Uhr func() time.Time
}

type gespeichert struct {
	Fassung
	zeit time.Time
}

var _ Ablage = (*Arbeitsspeicher)(nil)

// NeuerArbeitsspeicher liefert eine leere Ablage.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{fassungen: map[string][]gespeichert{}, generation: 1_000}
}

// aktuell liefert die Version der neuesten Fassung, 0 ohne Sicherung.
func (a *Arbeitsspeicher) aktuell(kontoID string) int64 {
	liste := a.fassungen[kontoID]
	if len(liste) == 0 {
		return 0
	}
	return liste[len(liste)-1].Version
}

func (a *Arbeitsspeicher) Speichern(_ context.Context, kontoID string, chiffrat []byte, erwartet int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.aktuell(kontoID) != erwartet {
		return 0, ErrVersionVeraltet
	}
	// Wie in Cloud Storage springt die Version (ein Zähler für alle Konten), damit
	// kein Test und kein Client mit "+ 1" rechnet.
	a.generation += 7
	f := Fassung{Chiffrat: append([]byte(nil), chiffrat...), Version: a.generation}
	jetzt := time.Now()
	if a.Uhr != nil {
		jetzt = a.Uhr()
	}
	a.fassungen[kontoID] = append(a.fassungen[kontoID], gespeichert{f, jetzt})
	return a.generation, nil
}

func (a *Arbeitsspeicher) Holen(_ context.Context, kontoID string, version int64) (Fassung, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if version == 0 {
		version = a.aktuell(kontoID)
	}
	for _, g := range a.fassungen[kontoID] {
		if g.Version == version {
			return Fassung{Chiffrat: append([]byte(nil), g.Chiffrat...), Version: g.Version}, nil
		}
	}
	return Fassung{}, ErrNichtGefunden
}

func (a *Arbeitsspeicher) Fassungen(_ context.Context, kontoID string) ([]FassungsInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	liste := []FassungsInfo{}
	for _, g := range slices.Backward(a.fassungen[kontoID]) {
		liste = append(liste, FassungsInfo{Version: g.Version, Gespeichert: g.zeit, Bytes: int64(len(g.Chiffrat))})
	}
	return liste, nil
}

func (a *Arbeitsspeicher) Loeschen(_ context.Context, kontoID string) error {
	if kontoID == "" {
		return errors.New("Konto-ID fehlt") // wie Cloud Storage
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.fassungen, kontoID)
	return nil
}

func (a *Arbeitsspeicher) Konten(_ context.Context, vor time.Time) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := []string{}
	for id, liste := range a.fassungen {
		if slices.ContainsFunc(liste, func(g gespeichert) bool { return g.zeit.Before(vor) }) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}
