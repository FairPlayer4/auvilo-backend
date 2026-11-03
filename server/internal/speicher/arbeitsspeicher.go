package speicher

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Arbeitsspeicher hält alles im Speicher. Für Tests und lokales Ausprobieren;
// nach einem Neustart ist alles weg.
type Arbeitsspeicher struct {
	// Ein Mutex für alles: Jede Methode ist damit atomar wie eine Transaktion.
	mu        sync.Mutex
	kennungen map[string]string // Kennung → Konto-ID
	geraete   map[string]Geraet // Geräte-ID → Gerät
	sitzungen map[string]Sitzung
	backups   map[string]Backup // Konto-ID → Backup
}

// NeuerArbeitsspeicher legt einen leeren Speicher an.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{
		kennungen: map[string]string{},
		geraete:   map[string]Geraet{},
		sitzungen: map[string]Sitzung{},
		backups:   map[string]Backup{},
	}
}

// Prüft beim Übersetzen, dass Arbeitsspeicher die Schnittstelle erfüllt.
var _ Speicher = (*Arbeitsspeicher)(nil)

func (a *Arbeitsspeicher) Anmelden(_ context.Context, kennung string, g Geraet, s NeueSitzung) (AnmeldeErgebnis, error) {
	a.mu.Lock()
	defer a.mu.Unlock() // defer: läuft beim Verlassen der Methode (wie finally)

	kontoID, gibtEs := a.kennungen[kennung]
	neu := !gibtEs
	if neu {
		kontoID = neueKontoID()
	}
	if vorhanden, ok := a.geraete[g.ID]; ok {
		if vorhanden.KontoID != kontoID {
			return AnmeldeErgebnis{}, ErrGeraetFremd
		}
		if vorhanden.Gesperrt {
			return AnmeldeErgebnis{}, ErrGeraetGesperrt
		}
	} else {
		g.KontoID = kontoID
		g.Gesperrt = false
		g.GesperrtAm = time.Time{}
		g.Erstellt = g.Erstellt.UTC().Truncate(time.Millisecond)
		g.Schluessel = slices.Clone(g.Schluessel)
		a.geraete[g.ID] = g
	}
	// Erst schreiben, wenn alle Prüfungen bestanden sind (wie eine Transaktion).
	a.kennungen[kennung] = kontoID
	a.sitzungen[s.Hash] = Sitzung{Hash: s.Hash, KontoID: kontoID, GeraetID: g.ID, LaeuftAb: s.LaeuftAb.UTC().Truncate(time.Millisecond)}
	return AnmeldeErgebnis{KontoID: kontoID, NeuesKonto: neu}, nil
}

func (a *Arbeitsspeicher) Geraet(_ context.Context, geraetID string) (Geraet, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.geraete[geraetID]
	if !ok {
		return Geraet{}, ErrNichtGefunden
	}
	return g, nil
}

func (a *Arbeitsspeicher) SitzungAnlegen(_ context.Context, geraetID string, s NeueSitzung) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.geraete[geraetID]
	if !ok {
		return "", ErrNichtGefunden
	}
	if g.Gesperrt {
		return "", ErrGeraetGesperrt
	}
	a.sitzungen[s.Hash] = Sitzung{Hash: s.Hash, KontoID: g.KontoID, GeraetID: g.ID, LaeuftAb: s.LaeuftAb.UTC().Truncate(time.Millisecond)}
	return g.KontoID, nil
}

func (a *Arbeitsspeicher) Sitzung(_ context.Context, hash string) (Sitzung, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sitzungen[hash]
	if !ok {
		return Sitzung{}, ErrNichtGefunden
	}
	return s, nil
}

func (a *Arbeitsspeicher) Geraete(_ context.Context, kontoID string) ([]Geraet, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var liste []Geraet
	for _, g := range a.geraete {
		if g.KontoID == kontoID {
			liste = append(liste, g)
		}
	}
	sortiereGeraete(liste)
	return liste, nil
}

func (a *Arbeitsspeicher) GeraetSperren(_ context.Context, kontoID, geraetID string, jetzt time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.geraete[geraetID]
	if !ok || g.KontoID != kontoID {
		return ErrNichtGefunden
	}
	if !g.Gesperrt {
		g.Gesperrt = true
		g.GesperrtAm = jetzt.UTC().Truncate(time.Millisecond)
		a.geraete[geraetID] = g
	}
	for hash, s := range a.sitzungen {
		if s.GeraetID == geraetID {
			delete(a.sitzungen, hash)
		}
	}
	return nil
}

func (a *Arbeitsspeicher) BackupSpeichern(_ context.Context, kontoID string, chiffrat []byte, erwartet int64, jetzt time.Time) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Ohne Eintrag liefert die Map den Nullwert: Version 0 = es gibt keins.
	if a.backups[kontoID].Version != erwartet {
		return 0, ErrVersionVeraltet
	}
	b := Backup{Chiffrat: slices.Clone(chiffrat), Version: erwartet + 1, Geaendert: jetzt.UTC().Truncate(time.Millisecond)}
	a.backups[kontoID] = b
	return b.Version, nil
}

func (a *Arbeitsspeicher) BackupHolen(_ context.Context, kontoID string) (Backup, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.backups[kontoID]
	if !ok {
		return Backup{}, ErrNichtGefunden
	}
	b.Chiffrat = slices.Clone(b.Chiffrat) // Kopie: Der Aufrufer soll den Speicher nicht verändern können
	return b, nil
}

func (a *Arbeitsspeicher) BackupLoeschen(_ context.Context, kontoID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.backups, kontoID)
	return nil
}

// sortiereGeraete sortiert nach Erstellung, bei Gleichstand nach ID – so ist
// die Reihenfolge in beiden Umsetzungen gleich.
func sortiereGeraete(liste []Geraet) {
	slices.SortFunc(liste, func(x, y Geraet) int {
		if c := x.Erstellt.Compare(y.Erstellt); c != 0 {
			return c
		}
		if x.ID < y.ID {
			return -1
		}
		if x.ID > y.ID {
			return 1
		}
		return 0
	})
}
