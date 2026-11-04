package dienst

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/diagnose"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/sitzung"
)

// MaxDiagnoseanfrage ist die Sendung plus Platz für die Protobuf-Hülle.
const MaxDiagnoseanfrage = diagnose.MaxNutzlast + 1<<10

// DiagnosenJeTag gibt an, wie oft ein Konto am Tag senden darf (diagnose.proto). Schützt
// die Kosten, falls eine App in einer Schleife sendet.
const DiagnosenJeTag = 10

// kontingentDiagnose ist die Art im Tageszähler.
const kontingentDiagnose = "diagnose"

// gzipKopf: Jede gzip-Datei beginnt mit diesen Bytes (Kennung 1f 8b, Verfahren
// 08 = deflate). Mehr prüft der Server nicht; den Inhalt liest er nie.
var gzipKopf = []byte{0x1f, 0x8b, 0x08}

var _ avlreminderv1connect.DiagnoseDienstHandler = (*dienste)(nil)

// nurFreigeschaltet lehnt Konten ohne Freischaltung ab, bevor jemand den Inhalt
// der Anfrage liest (nach sitzung.Interceptor.VorDemLesen, das die Identität in
// den Kontext legt).
//
// KRITISCH: Ohne Identität im Kontext kommt niemand durch.
func nurFreigeschaltet(erlaubt func(kontoID string) bool, h http.Handler) http.Handler {
	fehler := connect.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := sitzung.IdentitaetAus(r.Context())
		if !ok || id.KontoID == "" || !erlaubt(id.KontoID) {
			slog.WarnContext(r.Context(), "Diagnose von nicht freigeschaltetem Konto", "geraet", id.GeraetID)
			_ = fehler.Write(w, r, nichtFreigeschaltet())
			return
		}
		h.ServeHTTP(w, r)
	})
}

func nichtFreigeschaltet() error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("Konto ist für Diagnosen nicht freigeschaltet"))
}

// DiagnoseSenden legt eine Diagnose des eigenen Kontos ab
// (docs/diagnose-entwurf.md).
//
// KRITISCH:
//   - Nur freigeschaltete Konten (E-D-3): Diagnosen sind unverschlüsselt; es
//     sollen keine von Personen entstehen, die nicht eingewilligt haben.
//   - Die Konto-ID kommt aus der Sitzung, nie aus der Anfrage.
//   - Erst alle Prüfungen, dann zählen, dann ablegen: Eine abgelehnte Sendung
//     verbraucht nichts vom Tageslimit.
func (d *dienste) DiagnoseSenden(ctx context.Context, req *connect.Request[v1.DiagnoseSendenRequest]) (*connect.Response[v1.DiagnoseSendenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	// Zweite Prüfung: nurFreigeschaltet hat das schon vor dem Lesen getan.
	if !d.k.DiagnoseErlaubt(id.KontoID) {
		slog.WarnContext(ctx, "Diagnose von nicht freigeschaltetem Konto", "geraet", id.GeraetID)
		return nil, nichtFreigeschaltet()
	}
	nutzlast := req.Msg.GetNutzlast()
	if len(nutzlast) > diagnose.MaxNutzlast {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("Diagnose größer als 256 KiB"))
	}
	if !bytes.HasPrefix(nutzlast, gzipKopf) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("Diagnose leer oder nicht gzip-komprimiert"))
	}

	jetzt := d.k.Jetzt()
	err = d.k.Kontingente.Verbrauche(ctx, kontingentDiagnose, id.KontoID, jetzt, DiagnosenJeTag)
	if errors.Is(err, kontingent.ErrErschoepft) {
		slog.WarnContext(ctx, "Tageslimit für Diagnosen erreicht", "geraet", id.GeraetID)
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("Tageslimit für Diagnosen erreicht – morgen wieder"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	if _, err := d.k.Diagnosen.Ablegen(ctx, id.KontoID, jetzt, nutzlast); err != nil {
		return nil, speicherFehler(ctx, err)
	}
	// Nur Gerät und Größe ins Log, nie die Diagnose selbst.
	slog.InfoContext(ctx, "Diagnose abgelegt", "geraet", id.GeraetID, "bytes", len(nutzlast))
	return connect.NewResponse(&v1.DiagnoseSendenResponse{}), nil
}
