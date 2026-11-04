package sitzung

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Identitaet sagt, wer anfragt. Der Interceptor legt sie in den Kontext; die
// Dienste lesen sie mit IdentitaetAus.
type Identitaet struct {
	KontoID  string
	GeraetID string
}

// kontextSchluessel ist ein eigener Typ, damit kein anderes Paket denselben
// Schlüssel im Kontext benutzen kann (übliches Go-Muster).
type kontextSchluessel struct{}

// IdentitaetAus liefert die Identität aus dem Kontext. ok ist false, wenn die
// Anfrage nicht durch den Interceptor gelaufen ist.
func IdentitaetAus(ctx context.Context) (Identitaet, bool) {
	id, ok := ctx.Value(kontextSchluessel{}).(Identitaet)
	return id, ok
}

// SitzungLesen ist der Teil des Speichers, den der Interceptor braucht.
type SitzungLesen interface {
	Sitzung(ctx context.Context, hash string) (speicher.Sitzung, error)
}

// Interceptor prüft vor jedem Aufruf die Sitzung. Er ist eine "Vorschaltstufe"
// für alle Dienste (wie eine Middleware in ASP.NET).
//
// KRITISCH: Alles braucht eine gültige Sitzung, AUSSER den ausdrücklich als
// offen genannten Prozeduren (Anmelden, SitzungErneuern). Ein neuer Aufruf ist
// also automatisch geschützt, auch wenn man ihn hier vergisst.
type Interceptor struct {
	sitzungen SitzungLesen
	offen     map[string]bool
	jetzt     func() time.Time
}

// NeuerInterceptor legt den Interceptor an. offen sind die vollen Prozedurnamen
// (z. B. avlreminderv1connect.AnmeldeDienstAnmeldenProcedure). jetzt nil = time.Now.
func NeuerInterceptor(sitzungen SitzungLesen, offen []string, jetzt func() time.Time) *Interceptor {
	if jetzt == nil {
		jetzt = time.Now
	}
	m := map[string]bool{}
	for _, p := range offen {
		m[p] = true
	}
	return &Interceptor{sitzungen: sitzungen, offen: m, jetzt: jetzt}
}

// Der Interceptor erfüllt connect.Interceptor (Prüfung beim Übersetzen).
var _ connect.Interceptor = (*Interceptor)(nil)

// pruefe liefert den Kontext mit Identität oder einen Connect-Fehler.
func (i *Interceptor) pruefe(ctx context.Context, prozedur string, kopf http.Header) (context.Context, error) {
	if i.offen[prozedur] {
		return ctx, nil
	}
	// Aufbau: "<Schema> <Token>". Das Schema ist laut RFC 7235 ohne Groß- und
	// Kleinschreibung zu vergleichen ("bearer" gilt also auch).
	schema, token, ok := strings.Cut(kopf.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(schema, "Bearer") || token == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("Sitzung fehlt"))
	}
	s, err := i.sitzungen.Sitzung(ctx, Hash(token))
	if errors.Is(err, speicher.ErrNichtGefunden) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("Sitzung ungültig"))
	}
	if err != nil {
		slog.ErrorContext(ctx, "Sitzung lesen fehlgeschlagen", "fehler", err.Error())
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("Sitzung konnte nicht geprüft werden"))
	}
	// Firestore löscht abgelaufene Sitzungen erst mit Verzögerung (TTL), deshalb
	// prüfen wir die Ablaufzeit selbst.
	if !i.jetzt().Before(s.LaeuftAb) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("Sitzung abgelaufen"))
	}
	return context.WithValue(ctx, kontextSchluessel{}, Identitaet{KontoID: s.KontoID, GeraetID: s.GeraetID}), nil
}

// WrapUnary prüft normale Aufrufe (eine Anfrage, eine Antwort).
func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req) // nur für Server gedacht
		}
		ctx, err := i.pruefe(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// WrapStreamingHandler prüft Streaming-Aufrufe. Heute gibt es keine; die
// Prüfung ist trotzdem da, damit spätere nicht ungeschützt sind.
func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.pruefe(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// VorDemLesen prüft die Sitzung schon auf HTTP-Ebene, bevor jemand den Inhalt
// der Anfrage liest. Connect liest den Inhalt nämlich vollständig, bevor der
// Interceptor läuft. Für große Anfragen (Datensicherung, bis 8 MiB) heißt das:
// Ohne diese Vorprüfung könnte jeder ohne Anmeldung Speicher und die wenigen
// Plätze für gleichzeitige Sicherungen belegen. h bekommt die Identität im
// Kontext (IdentitaetAus), etwa um die Freischaltung für Diagnosen ebenfalls vor
// dem Lesen zu prüfen. Der Interceptor prüft danach trotzdem noch einmal.
//
// KRITISCH: Ohne gültige Sitzung kommt keine Anfrage zu h durch; es gibt hier
// keine offenen Prozeduren.
func (i *Interceptor) VorDemLesen(h http.Handler) http.Handler {
	fehler := connect.NewErrorWriter()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// "" ist nie eine offene Prozedur: Geprüft wird immer.
		ctx, err := i.pruefe(r.Context(), "", r.Header)
		if err != nil {
			_ = fehler.Write(w, r, err)
			return
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// WrapStreamingClient lässt Client-Aufrufe unverändert (nur Server-Seite).
func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}
