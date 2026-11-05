package dienst

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
)

// MaxSicherungsanfrage ist die Sicherung plus Platz für die Protobuf-Hülle. Was
// größer ist, lehnt Connect ab, bevor die Anfrage ganz im Speicher liegt.
const MaxSicherungsanfrage = datensicherung.MaxChiffrat + 1<<10

// StandardGleichzeitigeSicherungen ist die Zahl der Aufrufe des
// Datensicherungs-Dienstes, die höchstens gleichzeitig laufen. Cloud Run gibt dem Server 256 MiB und bis zu 80
// Anfragen gleichzeitig; 80 Sicherungen à 8 MiB passen da nicht hinein. Vier
// brauchen gemessen etwa 95 MiB (Anfrage, entpackte Nachricht, Upload); dazu
// kommt der Grundbedarf des Servers.
const StandardGleichzeitigeSicherungen = 4

// SicherungenJeTag gibt an, wie oft ein Konto am Tag (UTC) speichern darf
// (datensicherung.proto, b2b-entwurf.md E-B2b-12): stündlich (24) plus ebenso viel
// Reserve für Sicherungen von Hand und zweite Versuche nach Konflikten.
//
// Schützt Kosten und Rückfallebene: Eine App, die in einer Schleife sichert,
// lädt höchstens 48 × 8 MiB am Tag hoch und verdrängt von den 200 älteren
// Fassungen (infra/datensicherung.tf) höchstens 48, also nie die ganze Woche.
const SicherungenJeTag = 48

// kontingentSicherung ist die Art im Tageszähler.
const kontingentSicherung = "sicherung"

var _ avlreminderv1connect.DatensicherungDienstHandler = (*dienste)(nil)

// begrenze lässt höchstens max Anfragen gleichzeitig durch. Weitere bekommen
// sofort "unavailable" (später wiederholen), statt zu warten: Wartende Anfragen
// hielten ihren Inhalt sonst ebenfalls im Speicher.
func begrenze(max int, h http.Handler) http.Handler {
	plaetze := make(chan struct{}, max) // ein Eintrag je laufender Anfrage
	fehler := connect.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case plaetze <- struct{}{}:
			defer func() { <-plaetze }()
			h.ServeHTTP(w, r)
		default:
			slog.WarnContext(r.Context(), "zu viele Sicherungen gleichzeitig")
			_ = fehler.Write(w, r, connect.NewError(connect.CodeUnavailable, errors.New("gerade zu viele Sicherungen – später wiederholen")))
		}
	})
}

// SicherungSpeichern legt die Sicherung des eigenen Kontos ab.
//
// KRITISCH:
//   - Die Konto-ID kommt aus der Sitzung, nie aus der Anfrage – ein Gerät kann
//     nur die Sicherung seines eigenen Kontos schreiben.
//   - Erst prüfen, dann zählen, dann speichern: Eine abgelehnte Anfrage
//     verbraucht nichts vom Tageslimit; ein Konflikt (veraltete Version) schon,
//     denn ob die Version passt, zeigt erst das Speichern.
func (d *dienste) SicherungSpeichern(ctx context.Context, req *connect.Request[v1.SicherungSpeichernRequest]) (*connect.Response[v1.SicherungSpeichernResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	chiffrat, erwartet := req.Msg.GetChiffrat(), req.Msg.GetErwarteteVersion()
	if len(chiffrat) == 0 || erwartet < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("Sicherung leer oder Version negativ"))
	}
	// Eigener Code für "zu groß", damit die App das eindeutig melden kann
	// (datensicherung.proto); derselbe, den Connect für zu große Anfragen nimmt.
	if len(chiffrat) > datensicherung.MaxChiffrat {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("Sicherung größer als 8 MiB"))
	}
	err = d.k.Kontingente.Verbrauche(ctx, kontingentSicherung, id.KontoID, d.k.Jetzt(), SicherungenJeTag)
	if errors.Is(err, kontingent.ErrErschoepft) {
		slog.WarnContext(ctx, "Tageslimit für Sicherungen erreicht", "geraet", id.GeraetID)
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("Tageslimit für Sicherungen erreicht – morgen wieder"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	version, err := d.k.Sicherungen.Speichern(ctx, id.KontoID, chiffrat, erwartet)
	if errors.Is(err, datensicherung.ErrVersionVeraltet) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Sicherung wurde inzwischen geändert – neu holen"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	// Nur Größe und Version ins Log, nie das Chiffrat.
	slog.InfoContext(ctx, "Sicherung gespeichert", "geraet", id.GeraetID, "version", version, "bytes", len(chiffrat))
	return connect.NewResponse(&v1.SicherungSpeichernResponse{Version: version}), nil
}

// SicherungHolen liefert die neueste Fassung der Sicherung des eigenen Kontos.
func (d *dienste) SicherungHolen(ctx context.Context, _ *connect.Request[v1.SicherungHolenRequest]) (*connect.Response[v1.SicherungHolenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	f, err := d.k.Sicherungen.Holen(ctx, id.KontoID)
	if errors.Is(err, datensicherung.ErrNichtGefunden) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("keine Sicherung"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Sicherung geholt", "geraet", id.GeraetID, "version", f.Version, "bytes", len(f.Chiffrat))
	return connect.NewResponse(&v1.SicherungHolenResponse{Chiffrat: f.Chiffrat, Version: f.Version}), nil
}

// SicherungLoeschen löscht die Sicherung des eigenen Kontos mit allen älteren
// Fassungen.
func (d *dienste) SicherungLoeschen(ctx context.Context, _ *connect.Request[v1.SicherungLoeschenRequest]) (*connect.Response[v1.SicherungLoeschenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.k.Sicherungen.Loeschen(ctx, id.KontoID); err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Sicherung gelöscht", "geraet", id.GeraetID)
	return connect.NewResponse(&v1.SicherungLoeschenResponse{}), nil
}
