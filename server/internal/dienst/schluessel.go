package dienst

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// MaxBackup ist die größte erlaubte Backup-Größe. Ein echtes Backup hat etwa
// 1 KiB (Personen- und Tresor-Keyset); der Rest ist Luft für Schlüsselwechsel.
const MaxBackup = 16 << 10 // 16 KiB

var _ avlreminderv1connect.SchluesselDienstHandler = (*dienste)(nil)

// BackupSpeichern legt das Backup des eigenen Kontos ab.
//
// KRITISCH: Die Konto-ID kommt aus der Sitzung, nie aus der Anfrage – ein
// Gerät kann nur das Backup seines eigenen Kontos schreiben.
func (d *dienste) BackupSpeichern(ctx context.Context, req *connect.Request[v1.BackupSpeichernRequest]) (*connect.Response[v1.BackupSpeichernResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	chiffrat, erwartet := req.Msg.GetChiffrat(), req.Msg.GetErwarteteVersion()
	if len(chiffrat) == 0 || len(chiffrat) > MaxBackup || erwartet < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("Backup leer, größer als 16 KiB oder Version negativ"))
	}
	version, err := d.k.Speicher.BackupSpeichern(ctx, id.KontoID, chiffrat, erwartet, d.k.Jetzt())
	if errors.Is(err, speicher.ErrVersionVeraltet) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Backup wurde inzwischen geändert – neu holen"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	// Nur Größe und Version ins Log, nie das Chiffrat.
	slog.InfoContext(ctx, "Backup gespeichert", "geraet", id.GeraetID, "version", version, "bytes", len(chiffrat))
	return connect.NewResponse(&v1.BackupSpeichernResponse{Version: version}), nil
}

// BackupHolen liefert das Backup des eigenen Kontos.
func (d *dienste) BackupHolen(ctx context.Context, _ *connect.Request[v1.BackupHolenRequest]) (*connect.Response[v1.BackupHolenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	b, err := d.k.Speicher.BackupHolen(ctx, id.KontoID)
	if errors.Is(err, speicher.ErrNichtGefunden) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("noch kein Backup"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	return connect.NewResponse(&v1.BackupHolenResponse{Chiffrat: b.Chiffrat, Version: b.Version}), nil
}

// BackupLoeschen löscht das Backup des eigenen Kontos.
func (d *dienste) BackupLoeschen(ctx context.Context, _ *connect.Request[v1.BackupLoeschenRequest]) (*connect.Response[v1.BackupLoeschenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.k.Speicher.BackupLoeschen(ctx, id.KontoID); err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Backup gelöscht", "geraet", id.GeraetID)
	return connect.NewResponse(&v1.BackupLoeschenResponse{}), nil
}
