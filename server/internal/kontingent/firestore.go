package kontingent

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sammlung: Dokument-ID "<art>-<tag>-<konto>"; TTL auf laeuft_ab (infra/diagnose.tf).
const sammlung = "kontingente"

// Wie lange ein Zähler nach seinem Tag liegen bleibt, bis Firestore ihn löscht
// (TTL). Gelesen wird nur der heutige; ein Tag Luft reicht.
const aufbewahrung = 48 * time.Hour

type zaehlerDok struct {
	Anzahl   int       `firestore:"anzahl"`
	LaeuftAb time.Time `firestore:"laeuft_ab"`
}

// Firestore zählt in der Firestore-Datenbank des Projekts.
type Firestore struct {
	c *firestore.Client
}

var _ Zaehler = (*Firestore)(nil)

// NeuerFirestore nutzt einen fertigen Client (Erzeugen und Schließen macht der
// Aufrufer).
func NeuerFirestore(c *firestore.Client) *Firestore { return &Firestore{c: c} }

// dokID: art zuerst und der Tag mit fester Länge in der Mitte, damit sich zwei
// Kombinationen nie denselben Namen teilen.
func dokID(art, kontoID string, jetzt time.Time) string {
	return art + "-" + tag(jetzt) + "-" + kontoID
}

// Verbrauche erfüllt Zaehler.Verbrauche.
//
// KRITISCH: Lesen und Erhöhen in einer Transaktion. Zwei gleichzeitige Aufrufe
// beim Stand max-1 dürfen nicht beide durchkommen; Firestore wiederholt dann
// einen von beiden, und der sieht den neuen Stand.
func (f *Firestore) Verbrauche(ctx context.Context, art, kontoID string, jetzt time.Time, max int) error {
	if art == "" || kontoID == "" {
		return errors.New("Art und Konto-ID sind Pflicht")
	}
	ref := f.c.Collection(sammlung).Doc(dokID(art, kontoID, jetzt))
	return f.c.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		var d zaehlerDok
		snap, err := tx.Get(ref)
		switch {
		case err == nil:
			if err := snap.DataTo(&d); err != nil {
				return err
			}
		case status.Code(err) == codes.NotFound:
			// noch nichts gezählt: d bleibt bei 0
		default:
			return err
		}
		if d.Anzahl >= max {
			return ErrErschoepft
		}
		tagesbeginn, _ := time.Parse(time.DateOnly, tag(jetzt)) // tag liefert immer ein gültiges Datum
		return tx.Set(ref, zaehlerDok{Anzahl: d.Anzahl + 1, LaeuftAb: tagesbeginn.Add(aufbewahrung)})
	})
}
