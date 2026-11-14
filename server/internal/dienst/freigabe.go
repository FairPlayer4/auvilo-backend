package dienst

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Freigabe legt fest, welche Google-Konten den Server nutzen dürfen (ID7 in
// docs/anforderungen.md): Solange der Server offen erreichbar ist, soll nicht
// jeder mit einem Google-Konto Speicher und Budget verbrauchen. Erkannt wird
// ein Google-Konto an seiner Kennung (anmeldung.Kennung); sie bleibt gleich,
// auch wenn das Konto gelöscht und neu angelegt wird.
//
// Der Nullwert lässt niemanden zu. So kann eine vergessene Einstellung den
// Server nicht versehentlich für alle öffnen.
type Freigabe struct {
	alle      bool
	kennungen map[string]bool
}

// FreigabeAlle lässt jedes Google-Konto zu (lokale Tests, später ein
// öffentlicher Betrieb).
func FreigabeAlle() Freigabe { return Freigabe{alle: true} }

// FreigabeFuer lässt genau die genannten Kennungen zu.
func FreigabeFuer(kennungen []string) Freigabe {
	f := Freigabe{kennungen: make(map[string]bool, len(kennungen))}
	for _, k := range kennungen {
		f.kennungen[k] = true
	}
	return f
}

// erlaubt sagt, ob die Kennung zugelassen ist.
//
// KRITISCH: Nur genau die genannten Kennungen, keine Teilstrings; eine leere
// Kennung nie (außer bei FreigabeAlle).
func (f Freigabe) erlaubt(kennung string) bool {
	return f.alle || (kennung != "" && f.kennungen[kennung])
}

// errNichtFreigegeben meldet ein Google-Konto, das nicht auf der Liste steht.
// Anders als sonst nennt der Server hier den Grund und die Kennung: Der
// Nutzer braucht sie, um die Freigabe bei Kevin zu erfragen. Verraten wird
// damit nichts, denn die Prüfung des Google-Logins ist vorher bestanden.
func errNichtFreigegeben(kennung string) error {
	err := connect.NewError(connect.CodeFailedPrecondition, errors.New("Google-Konto ist nicht freigegeben"))
	if detail, e := connect.NewErrorDetail(&v1.NichtFreigegeben{Kennung: kennung}); e == nil {
		err.AddDetail(detail)
	}
	return err
}

// kennungImLog kürzt die Kennung für das Log: genug, um eine Anfrage
// wiederzufinden, ohne die ganze Kennung in Cloud Logging abzulegen.
func kennungImLog(kennung string) string {
	if len(kennung) > 8 {
		return kennung[:8]
	}
	return kennung
}

// pruefeFreigabe prüft beim Anmelden, ob das Google-Konto zugelassen ist.
//
// KRITISCH: Aufruf direkt nach der Prüfung des Google-Logins und vor jedem
// Zugriff auf Firestore oder den Tageszähler. Ein nicht freigegebenes Konto
// kostet so nur die Prüfung des ID-Tokens und legt nichts an.
func (d *anmeldeDienst) pruefeFreigabe(ctx context.Context, kennung string) error {
	if d.freigabe.erlaubt(kennung) {
		return nil
	}
	slog.WarnContext(ctx, "Anmeldung abgelehnt: Google-Konto nicht freigegeben", "kennung_anfang", kennungImLog(kennung))
	return errNichtFreigegeben(kennung)
}

// pruefeFreigabeDesKontos prüft beim Erneuern, ob das Konto noch zugelassen
// ist. Wer von der Liste genommen wird, verliert den Zugang so spätestens mit
// dem Ablauf der laufenden Sitzung (1 Stunde), nicht erst beim nächsten
// Anmelden mit Google.
//
// KRITISCH: Gibt es zum Konto keine Kennung mehr (Löschung läuft,
// KontoSperren), wird wie bei einem unbekannten Gerät abgelehnt.
func (d *anmeldeDienst) pruefeFreigabeDesKontos(ctx context.Context, kontoID string) error {
	if d.freigabe.alle {
		return nil // spart das Lesen der Kennung
	}
	kennung, err := d.speicher.KennungVon(ctx, kontoID)
	if errors.Is(err, speicher.ErrNichtGefunden) {
		slog.WarnContext(ctx, "Erneuern abgelehnt: Konto ohne Kennung", "konto", kontoID)
		return errErneuernAbgelehnt()
	}
	if err != nil {
		return speicherFehler(ctx, err)
	}
	if !d.freigabe.erlaubt(kennung) {
		slog.WarnContext(ctx, "Erneuern abgelehnt: Google-Konto nicht mehr freigegeben", "kennung_anfang", kennungImLog(kennung))
		return errNichtFreigegeben(kennung)
	}
	return nil
}
