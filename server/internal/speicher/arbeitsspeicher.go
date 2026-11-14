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
	konten    map[string]konto  // Konto-ID → Konto
	geraete   map[string]Geraet // Geräte-ID → Gerät
	sitzungen map[string]Sitzung
	backups   map[string]Backup // Konto-ID → Backup
}

// NeuerArbeitsspeicher legt einen leeren Speicher an.
func NeuerArbeitsspeicher() *Arbeitsspeicher {
	return &Arbeitsspeicher{
		kennungen: map[string]string{},
		konten:    map[string]konto{},
		geraete:   map[string]Geraet{},
		sitzungen: map[string]Sitzung{},
		backups:   map[string]Backup{},
	}
}

// konto: was der Arbeitsspeicher zu einem Konto hält.
type konto struct {
	loeschenAb time.Time // Nullwert = keine Löschung beantragt
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
		g = vorhanden
	} else {
		frei, err := neuesGeraetFreigeschaltet(a.geraeteVon(kontoID), len(a.backups[kontoID].Freischaltschluessel) > 0)
		if err != nil {
			return AnmeldeErgebnis{}, err
		}
		g.KontoID = kontoID
		g.Freigeschaltet = frei
		g.Gesperrt = false
		g.GesperrtAm = time.Time{}
		g.Erstellt = g.Erstellt.UTC().Truncate(time.Millisecond)
		g.Schluessel = slices.Clone(g.Schluessel)
		a.geraete[g.ID] = g
	}
	// Erst schreiben, wenn alle Prüfungen bestanden sind (wie eine Transaktion).
	a.kennungen[kennung] = kontoID
	if neu {
		a.konten[kontoID] = konto{}
	}
	a.sitzungen[s.Hash] = Sitzung{Hash: s.Hash, KontoID: kontoID, GeraetID: g.ID, LaeuftAb: s.LaeuftAb.UTC().Truncate(time.Millisecond)}
	return AnmeldeErgebnis{KontoID: kontoID, NeuesKonto: neu, Freigeschaltet: g.Freigeschaltet}, nil
}

// geraeteVon liefert die Geräte eines Kontos (unsortiert). Aufruf nur mit mu.
func (a *Arbeitsspeicher) geraeteVon(kontoID string) []Geraet {
	var liste []Geraet
	for _, g := range a.geraete {
		if g.KontoID == kontoID {
			liste = append(liste, g)
		}
	}
	return liste
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

func (a *Arbeitsspeicher) KennungVon(_ context.Context, kontoID string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for kennung, id := range a.kennungen {
		if id == kontoID {
			return kennung, nil
		}
	}
	return "", ErrNichtGefunden
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
	liste := a.geraeteVon(kontoID)
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

func (a *Arbeitsspeicher) GeraetFreischalten(_ context.Context, kontoID, geraetID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	g, ok := a.geraete[geraetID]
	if err := darfFreischalten(g, ok, kontoID); err != nil {
		return err
	}
	g.Freigeschaltet = true
	a.geraete[geraetID] = g
	return nil
}

func (a *Arbeitsspeicher) BackupSpeichern(_ context.Context, kontoID string, chiffrat, freischaltschluessel []byte, erwartet int64, ungeschuetztFuer string, jetzt time.Time) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Ohne Eintrag liefert die Map den Nullwert: Version 0 = es gibt keins.
	if err := darfBackupSpeichern(a.backups[kontoID].Version, erwartet, a.geraeteVon(kontoID), ungeschuetztFuer); err != nil {
		return 0, err
	}
	if ungeschuetztFuer != "" {
		g := a.geraete[ungeschuetztFuer]
		g.Freigeschaltet = true
		a.geraete[ungeschuetztFuer] = g
	}
	b := Backup{
		Chiffrat:             slices.Clone(chiffrat),
		Freischaltschluessel: slices.Clone(freischaltschluessel),
		Version:              erwartet + 1,
		Geaendert:            jetzt.UTC().Truncate(time.Millisecond),
	}
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
	// Kopien: Der Aufrufer soll den Speicher nicht verändern können.
	b.Chiffrat = slices.Clone(b.Chiffrat)
	b.Freischaltschluessel = slices.Clone(b.Freischaltschluessel)
	return b, nil
}

func (a *Arbeitsspeicher) BackupLoeschen(_ context.Context, kontoID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.backups, kontoID)
	return nil
}

func (a *Arbeitsspeicher) LoeschungBeantragen(_ context.Context, kontoID string, ab time.Time) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, ok := a.konten[kontoID]
	if !ok {
		return time.Time{}, ErrNichtGefunden
	}
	k.loeschenAb = frueherTermin(k.loeschenAb, ab.UTC().Truncate(time.Millisecond))
	a.konten[kontoID] = k
	return k.loeschenAb, nil
}

func (a *Arbeitsspeicher) LoeschungAbbrechen(_ context.Context, kontoID string, jetzt time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	k, ok := a.konten[kontoID]
	if !ok || k.loeschenAb.IsZero() {
		return nil
	}
	if istFaellig(k.loeschenAb, jetzt) {
		return ErrLoeschungLaeuft
	}
	k.loeschenAb = time.Time{}
	a.konten[kontoID] = k
	return nil
}

func (a *Arbeitsspeicher) LoeschenAb(_ context.Context, kontoID string) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.konten[kontoID].loeschenAb, nil
}

func (a *Arbeitsspeicher) FaelligeLoeschungen(_ context.Context, jetzt time.Time, max int) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []string
	for id, k := range a.konten {
		if istFaellig(k.loeschenAb, jetzt) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids) // Maps haben keine feste Reihenfolge
	if len(ids) > max {
		ids = ids[:max]
	}
	return ids, nil
}

// faellig prüft, ob ein Konto entfernt werden darf (gibtEs = es gibt das
// Konto noch). Aufruf nur mit mu.
func (a *Arbeitsspeicher) faellig(kontoID string, jetzt time.Time) (gibtEs bool, err error) {
	k, ok := a.konten[kontoID]
	if !ok {
		return false, nil
	}
	if !istFaellig(k.loeschenAb, jetzt) {
		return true, ErrNichtFaellig
	}
	return true, nil
}

func (a *Arbeitsspeicher) KontoSperren(_ context.Context, kontoID string, jetzt time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gibtEs, err := a.faellig(kontoID, jetzt); !gibtEs || err != nil {
		return err
	}
	a.loescheKennungenVon(kontoID)
	for id, g := range a.geraete {
		if g.KontoID != kontoID {
			continue
		}
		if !g.Gesperrt {
			g.Gesperrt = true
			g.GesperrtAm = jetzt.UTC().Truncate(time.Millisecond)
			a.geraete[id] = g
		}
	}
	a.loescheSitzungenVon(kontoID)
	return nil
}

func (a *Arbeitsspeicher) KontoEntfernen(_ context.Context, kontoID string, jetzt time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gibtEs, err := a.faellig(kontoID, jetzt); !gibtEs || err != nil {
		return err
	}
	a.loescheKennungenVon(kontoID) // falls KontoSperren nicht gelaufen ist
	for id, g := range a.geraete {
		if g.KontoID == kontoID {
			delete(a.geraete, id)
		}
	}
	a.loescheSitzungenVon(kontoID)
	delete(a.backups, kontoID)
	delete(a.konten, kontoID)
	return nil
}

// loescheKennungenVon löscht die Kennungen eines Kontos. Aufruf nur mit mu.
func (a *Arbeitsspeicher) loescheKennungenVon(kontoID string) {
	for kennung, id := range a.kennungen {
		if id == kontoID {
			delete(a.kennungen, kennung)
		}
	}
}

// loescheSitzungenVon löscht alle Sitzungen eines Kontos. Aufruf nur mit mu.
func (a *Arbeitsspeicher) loescheSitzungenVon(kontoID string) {
	for hash, s := range a.sitzungen {
		if s.KontoID == kontoID {
			delete(a.sitzungen, hash)
		}
	}
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
