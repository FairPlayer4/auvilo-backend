package dienst

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/freischaltung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/sitzung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Freischaltung neuer Geräte (E-B2b-13, docs/b2b-entwurf.md): Wer nur den
// Google-Login hat, meldet sich als neues Gerät an, darf damit aber nichts
// ändern, bis ein vertrautes Gerät zustimmt oder es den
// Wiederherstellungsschlüssel kennt. Die Prüfung der Signatur liegt im Paket
// freischaltung; hier stehen nur die Aufrufe und die Prüfung in den Diensten.

func errNichtFreigeschaltet() error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("Gerät ist noch nicht freigeschaltet"))
}

// nurFreigeschaltetesGeraet lehnt ab, wenn das anfragende Gerät nicht
// freigeschaltet ist. Aufruf über die Richtlinie "freigeschaltet" (richtlinie.go)
// und in Diensten mit eigener Regel.
//
// KRITISCH: Das Merkmal kommt aus dem Speicher, nicht aus der Sitzung. So
// wirkt ein Freischalten sofort, ohne neue Sitzung.
func (d *freischaltPruefer) nurFreigeschaltetesGeraet(ctx context.Context, id sitzung.Identitaet) error {
	frei, err := d.istFreigeschaltet(ctx, id)
	if err != nil {
		return err
	}
	if !frei {
		slog.WarnContext(ctx, "Aufruf eines nicht freigeschalteten Geräts abgelehnt", "geraet", id.GeraetID)
		return errNichtFreigeschaltet()
	}
	return nil
}

// istFreigeschaltet liest, ob das anfragende Gerät freigeschaltet ist. Ein
// fehlendes Gerät ist es nicht; nur ein Speicherfehler ist ein Fehler.
func (d *freischaltPruefer) istFreigeschaltet(ctx context.Context, id sitzung.Identitaet) (bool, error) {
	g, err := d.geraete.Geraet(ctx, id.GeraetID)
	if errors.Is(err, speicher.ErrNichtGefunden) {
		slog.ErrorContext(ctx, "Sitzung zu einem unbekannten Gerät", "geraet", id.GeraetID)
		return false, nil
	}
	if err != nil {
		return false, speicherFehler(ctx, err)
	}
	// Ein gesperrtes Gerät lässt schon der Interceptor nicht durch; die
	// Prüfung schadet nicht.
	return g.Freigeschaltet && !g.Gesperrt && g.KontoID == id.KontoID, nil
}

// GeraetFreischalten: Ein freigeschaltetes Gerät schaltet ein anderes Gerät
// des eigenen Kontos frei.
//
// KRITISCH: Nur ein freigeschaltetes Gerät darf das, und nur im eigenen Konto
// (die Konto-ID kommt aus der Sitzung, nie aus der Anfrage).
func (d *geraeteDienst) GeraetFreischalten(ctx context.Context, req *connect.Request[v1.GeraetFreischaltenRequest]) (*connect.Response[v1.GeraetFreischaltenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	ziel := req.Msg.GetGeraetId()
	if !geraeteschluessel.IstGeraetID(ziel) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	}
	err = d.speicher.GeraetFreischalten(ctx, id.KontoID, ziel)
	switch {
	case errors.Is(err, speicher.ErrNichtGefunden):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	case errors.Is(err, speicher.ErrGeraetGesperrt):
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Gerät ist gesperrt"))
	case err != nil:
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Gerät freigeschaltet", "geraet", ziel, "durch", id.GeraetID)
	return connect.NewResponse(&v1.GeraetFreischaltenResponse{}), nil
}

// MitWiederherstellungFreischalten: Das anfragende Gerät schaltet sich selbst
// frei, indem es mit dem aus dem Wiederherstellungsschlüssel abgeleiteten
// Schlüssel seine Geräte-ID signiert.
//
// KRITISCH:
//   - Geprüft wird gegen den Freischaltschlüssel am Backup des eigenen Kontos
//     und über die Geräte-ID aus der Sitzung. Eine Signatur für ein anderes
//     Gerät oder mit einem anderen Schlüssel schaltet nichts frei.
//   - Ohne Backup mit Freischaltschlüssel gibt es nichts zu prüfen: abgelehnt.
func (d *geraeteDienst) MitWiederherstellungFreischalten(ctx context.Context, req *connect.Request[v1.MitWiederherstellungFreischaltenRequest]) (*connect.Response[v1.MitWiederherstellungFreischaltenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	b, err := d.speicher.BackupHolen(ctx, id.KontoID)
	if err != nil && !errors.Is(err, speicher.ErrNichtGefunden) {
		return nil, speicherFehler(ctx, err)
	}
	if len(b.Freischaltschluessel) == 0 { // auch, wenn es kein Backup gibt
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("kein Backup mit Freischaltschlüssel"))
	}
	if err := freischaltung.Pruefe(b.Freischaltschluessel, id.GeraetID, req.Msg.GetSignatur()); err != nil {
		slog.WarnContext(ctx, "Freischaltung mit falscher Signatur", "geraet", id.GeraetID)
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("Signatur passt nicht zum Wiederherstellungsschlüssel"))
	}
	err = d.speicher.GeraetFreischalten(ctx, id.KontoID, id.GeraetID)
	if errors.Is(err, speicher.ErrGeraetGesperrt) {
		return nil, errGesperrt()
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Gerät mit Wiederherstellungsschlüssel freigeschaltet", "geraet", id.GeraetID)
	return connect.NewResponse(&v1.MitWiederherstellungFreischaltenResponse{}), nil
}
