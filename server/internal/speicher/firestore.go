package speicher

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Namen der Sammlungen in Firestore (siehe docs/b1-entwurf.md).
const (
	sammlungKennungen = "google_kennungen" // Dokument-ID: Kennung (HMAC)
	sammlungKonten    = "konten"           // Dokument-ID: Konto-ID
	sammlungGeraete   = "geraete"          // Dokument-ID: Geräte-ID
	sammlungSitzungen = "sitzungen"        // Dokument-ID: Hash des Tokens; TTL auf laeuft_ab
	sammlungBackups   = "backups"          // Dokument-ID: Konto-ID (siehe docs/b2-entwurf.md)
)

// Dokumente, wie sie in Firestore liegen. Die Tags "firestore:..." geben die
// Feldnamen in der Datenbank an (wie [JsonPropertyName] in C#).
type kennungDok struct {
	KontoID string `firestore:"konto_id"`
}

type kontoDok struct {
	Erstellt time.Time `firestore:"erstellt"` // nur der Tag (Datensparsamkeit)
}

type geraetDok struct {
	KontoID    string     `firestore:"konto_id"`
	Schluessel []byte     `firestore:"schluessel"`
	Gesperrt   bool       `firestore:"gesperrt"`
	Erstellt   time.Time  `firestore:"erstellt"`
	GesperrtAm *time.Time `firestore:"gesperrt_am,omitempty"`
}

type sitzungDok struct {
	KontoID  string    `firestore:"konto_id"`
	GeraetID string    `firestore:"geraet_id"`
	LaeuftAb time.Time `firestore:"laeuft_ab"`
}

type backupDok struct {
	Chiffrat  []byte    `firestore:"chiffrat"`
	Version   int64     `firestore:"version"`
	Geaendert time.Time `firestore:"geaendert_am"`
}

// Firestore speichert in der Firestore-Datenbank des Projekts.
type Firestore struct {
	c *firestore.Client
}

// NeuerFirestore nutzt einen fertigen Client (Erzeugen und Schließen macht der
// Aufrufer). Ist FIRESTORE_EMULATOR_HOST gesetzt, spricht der Client mit dem
// lokalen Emulator statt mit GCP.
func NeuerFirestore(c *firestore.Client) *Firestore {
	return &Firestore{c: c}
}

var _ Speicher = (*Firestore)(nil)

// nichtGefunden erkennt den Fehler "Dokument gibt es nicht".
func nichtGefunden(err error) bool {
	return status.Code(err) == codes.NotFound
}

// Anmelden erfüllt Speicher.Anmelden.
//
// KRITISCH: Kennung, Konto, Gerät (samt Sperre und Konto-Zugehörigkeit) werden
// in derselben Transaktion gelesen, in der geschrieben wird. Sonst könnten zwei
// gleichzeitige Erst-Anmeldungen zwei Konten anlegen oder ein gerade gesperrtes
// Gerät eine Sitzung bekommen.
func (f *Firestore) Anmelden(ctx context.Context, kennung string, g Geraet, s NeueSitzung) (AnmeldeErgebnis, error) {
	var ergebnis AnmeldeErgebnis
	// RunTransaction wiederholt die Funktion bei Konflikten mit gleichzeitigen
	// Änderungen. In Firestore-Transaktionen müssen alle Lesezugriffe vor den
	// Schreibzugriffen kommen.
	err := f.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		ergebnis = AnmeldeErgebnis{}
		kennungRef := f.c.Collection(sammlungKennungen).Doc(kennung)
		geraetRef := f.c.Collection(sammlungGeraete).Doc(g.ID)

		// Lesen
		var kd kennungDok
		kennungSnap, err := tx.Get(kennungRef)
		switch {
		case err == nil:
			if err := kennungSnap.DataTo(&kd); err != nil {
				return err
			}
		case nichtGefunden(err):
			kd.KontoID = neueKontoID()
			ergebnis.NeuesKonto = true
		default:
			return err
		}
		ergebnis.KontoID = kd.KontoID

		geraetSnap, err := tx.Get(geraetRef)
		geraetNeu := false
		switch {
		case err == nil:
			var gd geraetDok
			if err := geraetSnap.DataTo(&gd); err != nil {
				return err
			}
			if gd.KontoID != kd.KontoID {
				return ErrGeraetFremd
			}
			if gd.Gesperrt {
				return ErrGeraetGesperrt
			}
		case nichtGefunden(err):
			geraetNeu = true
		default:
			return err
		}

		// Schreiben
		if ergebnis.NeuesKonto {
			if err := tx.Create(kennungRef, kd); err != nil {
				return err
			}
			konto := kontoDok{Erstellt: g.Erstellt.UTC().Truncate(24 * time.Hour)}
			if err := tx.Create(f.c.Collection(sammlungKonten).Doc(kd.KontoID), konto); err != nil {
				return err
			}
		}
		if geraetNeu {
			gd := geraetDok{
				KontoID:    kd.KontoID,
				Schluessel: g.Schluessel,
				Erstellt:   g.Erstellt.UTC().Truncate(time.Millisecond),
			}
			if err := tx.Create(geraetRef, gd); err != nil {
				return err
			}
		}
		return tx.Create(f.c.Collection(sammlungSitzungen).Doc(s.Hash), sitzungDok{
			KontoID:  kd.KontoID,
			GeraetID: g.ID,
			LaeuftAb: s.LaeuftAb.UTC().Truncate(time.Millisecond),
		})
	})
	if err != nil {
		return AnmeldeErgebnis{}, fmt.Errorf("anmelden: %w", err)
	}
	return ergebnis, nil
}

func (f *Firestore) Geraet(ctx context.Context, geraetID string) (Geraet, error) {
	snap, err := f.c.Collection(sammlungGeraete).Doc(geraetID).Get(ctx)
	if nichtGefunden(err) {
		return Geraet{}, ErrNichtGefunden
	}
	if err != nil {
		return Geraet{}, err
	}
	return geraetAus(snap)
}

// SitzungAnlegen erfüllt Speicher.SitzungAnlegen.
//
// KRITISCH: Die Sperre wird in derselben Transaktion gelesen, in der die
// Sitzung entsteht. Läse man das Gerät vorher (außerhalb), könnte ein
// gleichzeitiges Sperren die neue Sitzung übersehen.
func (f *Firestore) SitzungAnlegen(ctx context.Context, geraetID string, s NeueSitzung) (string, error) {
	var kontoID string
	err := f.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(f.c.Collection(sammlungGeraete).Doc(geraetID))
		if nichtGefunden(err) {
			return ErrNichtGefunden
		}
		if err != nil {
			return err
		}
		g, err := geraetAus(snap)
		if err != nil {
			return err
		}
		if g.Gesperrt {
			return ErrGeraetGesperrt
		}
		kontoID = g.KontoID
		return tx.Create(f.c.Collection(sammlungSitzungen).Doc(s.Hash), sitzungDok{
			KontoID:  g.KontoID,
			GeraetID: geraetID,
			LaeuftAb: s.LaeuftAb.UTC().Truncate(time.Millisecond),
		})
	})
	if err != nil {
		return "", fmt.Errorf("Sitzung anlegen: %w", err)
	}
	return kontoID, nil
}

func (f *Firestore) Sitzung(ctx context.Context, hash string) (Sitzung, error) {
	snap, err := f.c.Collection(sammlungSitzungen).Doc(hash).Get(ctx)
	if nichtGefunden(err) {
		return Sitzung{}, ErrNichtGefunden
	}
	if err != nil {
		return Sitzung{}, err
	}
	var sd sitzungDok
	if err := snap.DataTo(&sd); err != nil {
		return Sitzung{}, err
	}
	return Sitzung{Hash: hash, KontoID: sd.KontoID, GeraetID: sd.GeraetID, LaeuftAb: sd.LaeuftAb.UTC()}, nil
}

func (f *Firestore) Geraete(ctx context.Context, kontoID string) ([]Geraet, error) {
	// Nur ein Gleichheitsfilter: braucht keinen zusammengesetzten Index.
	// Sortiert wird danach in Go.
	snaps, err := f.c.Collection(sammlungGeraete).Where("konto_id", "==", kontoID).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var liste []Geraet
	for _, snap := range snaps {
		g, err := geraetAus(snap)
		if err != nil {
			return nil, err
		}
		liste = append(liste, g)
	}
	sortiereGeraete(liste)
	return liste, nil
}

// GeraetSperren erfüllt Speicher.GeraetSperren.
//
// KRITISCH: Gerät sperren und seine Sitzungen suchen und löschen geschieht in
// einer Transaktion. Der Interceptor prüft nur die Sitzung, nicht das Gerät:
// Bliebe eine Sitzung übrig, wäre das gesperrte Gerät nicht draußen.
func (f *Firestore) GeraetSperren(ctx context.Context, kontoID, geraetID string, jetzt time.Time) error {
	err := f.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		geraetRef := f.c.Collection(sammlungGeraete).Doc(geraetID)
		snap, err := tx.Get(geraetRef)
		if nichtGefunden(err) {
			return ErrNichtGefunden
		}
		if err != nil {
			return err
		}
		g, err := geraetAus(snap)
		if err != nil {
			return err
		}
		if g.KontoID != kontoID {
			return ErrNichtGefunden
		}
		sitzungen, err := tx.Documents(f.c.Collection(sammlungSitzungen).Where("geraet_id", "==", geraetID)).GetAll()
		if err != nil {
			return err
		}

		if !g.Gesperrt {
			gesperrtAm := jetzt.UTC().Truncate(time.Millisecond)
			if err := tx.Update(geraetRef, []firestore.Update{
				{Path: "gesperrt", Value: true},
				{Path: "gesperrt_am", Value: gesperrtAm},
			}); err != nil {
				return err
			}
		}
		for _, s := range sitzungen {
			if err := tx.Delete(s.Ref); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// errors.Is funktioniert weiter, weil %w den Fehler einpackt.
		return fmt.Errorf("Gerät sperren: %w", err)
	}
	return nil
}

// BackupSpeichern erfüllt Speicher.BackupSpeichern.
//
// KRITISCH: Version lesen und Backup schreiben in einer Transaktion. Ändert
// ein anderes Gerät das Backup dazwischen, wiederholt Firestore die Funktion,
// und die Versionsprüfung schlägt dann fehl – nie gewinnen beide.
func (f *Firestore) BackupSpeichern(ctx context.Context, kontoID string, chiffrat []byte, erwartet int64, jetzt time.Time) (int64, error) {
	ref := f.c.Collection(sammlungBackups).Doc(kontoID)
	var neu int64
	err := f.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var aktuell int64 // 0, falls es noch keins gibt
		snap, err := tx.Get(ref)
		switch {
		case err == nil:
			var bd backupDok
			if err := snap.DataTo(&bd); err != nil {
				return err
			}
			aktuell = bd.Version
		case !nichtGefunden(err):
			return err
		}
		if aktuell != erwartet {
			return ErrVersionVeraltet
		}
		neu = erwartet + 1
		return tx.Set(ref, backupDok{Chiffrat: chiffrat, Version: neu, Geaendert: jetzt.UTC().Truncate(time.Millisecond)})
	})
	if err != nil {
		return 0, fmt.Errorf("Backup speichern: %w", err)
	}
	return neu, nil
}

func (f *Firestore) BackupHolen(ctx context.Context, kontoID string) (Backup, error) {
	snap, err := f.c.Collection(sammlungBackups).Doc(kontoID).Get(ctx)
	if nichtGefunden(err) {
		return Backup{}, ErrNichtGefunden
	}
	if err != nil {
		return Backup{}, err
	}
	var bd backupDok
	if err := snap.DataTo(&bd); err != nil {
		return Backup{}, err
	}
	return Backup{Chiffrat: bd.Chiffrat, Version: bd.Version, Geaendert: bd.Geaendert.UTC()}, nil
}

func geraetAus(snap *firestore.DocumentSnapshot) (Geraet, error) {
	var gd geraetDok
	if err := snap.DataTo(&gd); err != nil {
		return Geraet{}, err
	}
	g := Geraet{
		ID:         snap.Ref.ID,
		KontoID:    gd.KontoID,
		Schluessel: gd.Schluessel,
		Gesperrt:   gd.Gesperrt,
		Erstellt:   gd.Erstellt.UTC(),
	}
	if gd.GesperrtAm != nil {
		g.GesperrtAm = gd.GesperrtAm.UTC()
	}
	return g, nil
}
