package dienst

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Geräte-Dienst: Liste und Sperren hier, Freischalten in freischaltung.go.

func (d *geraeteDienst) GeraeteAuflisten(ctx context.Context, _ *connect.Request[v1.GeraeteAuflistenRequest]) (*connect.Response[v1.GeraeteAuflistenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	liste, err := d.speicher.Geraete(ctx, id.KontoID)
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	antwort := &v1.GeraeteAuflistenResponse{LoeschenAb: loeschenAb(ctx, d.speicher, id.KontoID)}
	for _, g := range liste {
		antwort.Geraete = append(antwort.Geraete, &v1.Geraet{
			GeraetId:       g.ID,
			AngemeldetAm:   timestamppb.New(g.Erstellt),
			Gesperrt:       g.Gesperrt,
			DiesesGeraet:   g.ID == id.GeraetID,
			Freigeschaltet: g.Freigeschaltet,
		})
	}
	return connect.NewResponse(antwort), nil
}

func (d *geraeteDienst) GeraetSperren(ctx context.Context, req *connect.Request[v1.GeraetSperrenRequest]) (*connect.Response[v1.GeraetSperrenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	// KRITISCH (E-B2b-13): Sich selbst sperren (abmelden) darf jedes Gerät, ein
	// anderes nur ein freigeschaltetes. Sonst sperrte ein Dieb mit Google-Login
	// als Erstes die echten Geräte aus.
	if req.Msg.GetGeraetId() != id.GeraetID {
		if err := d.frei.nurFreigeschaltetesGeraet(ctx, id); err != nil {
			return nil, err
		}
	}
	// Eine ID im falschen Format kann es nicht geben (wie unbekannt behandeln).
	if !geraeteschluessel.IstGeraetID(req.Msg.GetGeraetId()) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	}
	// Gesperrt wird nur im eigenen Konto: Die Konto-ID kommt aus der Sitzung,
	// nie aus der Anfrage.
	err = d.speicher.GeraetSperren(ctx, id.KontoID, req.Msg.GetGeraetId(), d.jetzt())
	if errors.Is(err, speicher.ErrNichtGefunden) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Gerät gesperrt", "geraet", req.Msg.GetGeraetId(), "durch", id.GeraetID)
	return connect.NewResponse(&v1.GeraetSperrenResponse{}), nil
}
