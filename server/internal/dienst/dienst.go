// Package dienst verbindet die Schnittstelle (Connect) mit Anmeldung, Sitzung
// und Speicher. Die Prüfung von Anmeldung, Sitzung und Versionen liegt in den
// Paketen anmeldung, sitzung und speicher. Hier stehen nur die Regeln, welcher
// Aufruf was darf (Freischaltung neuer Geräte, freischaltung.go; Ersetzen des
// Backups, schluessel.go), sowie die Übersetzung von Anfragen und Fehlern.
package dienst

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/anmeldung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/datensicherung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/diagnose"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/kontingent"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/sitzung"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// MaxAnfrage ist die größte erlaubte Anfrage. Unsere Anfragen sind wenige KB groß;
// die Grenze schützt vor absichtlich riesigen Anfragen.
const MaxAnfrage = 64 << 10 // 64 KiB

// SitzungenJeTag gibt an, wie viele Sitzungen ein Gerät am Tag (UTC) höchstens
// bekommt, über Anmelden und SitzungErneuern zusammen (Entscheidung nach
// Review-Lauf 8). Eine Sitzung gilt 1 Stunde; die App braucht etwa 24 am Tag.
// Ohne Grenze könnte, wer den Geräteschlüssel hat, beliebig viele Sitzungen
// anlegen (Kosten, und das Sperren müsste sie alle löschen).
const SitzungenJeTag = 200

// kontingentSitzung ist die Art im Tageszähler; gezählt wird je Gerät.
const kontingentSitzung = "sitzung"

// NeueGeraeteJeTag gibt an, wie viele neue Geräte sich je Konto und Tag (UTC)
// höchstens anmelden (Entscheidung nach Review-Lauf 9). Die Grenze von 10
// offenen Geräten (speicher.MaxOffeneGeraete) allein hält niemanden auf, der
// den Google-Login hat: Er sperrt ein neues Gerät und legt das nächste an.
const NeueGeraeteJeTag = 10

// kontingentNeuesGeraet ist die Art im Tageszähler; gezählt wird je Konto
// (über die Kennung, das Konto kennt der Dienst vor dem Anmelden noch nicht).
const kontingentNeuesGeraet = "neues_geraet"

// AnmeldePruefer ist der Teil von anmeldung.Pruefer, den die Dienste brauchen.
type AnmeldePruefer interface {
	Pruefe(ctx context.Context, a anmeldung.Anfrage) (anmeldung.Ergebnis, error)
}

// Konfiguration für NeuerHandler.
type Konfiguration struct {
	Pruefer     AnmeldePruefer
	Speicher    speicher.Speicher
	Sicherungen datensicherung.Ablage
	Diagnosen   diagnose.Ablage
	Kontingente kontingent.Zaehler // Tageszähler je Konto (Diagnosen und Sicherungen)
	// DiagnoseErlaubt sagt, ob ein Konto Diagnosen senden darf (freigeschaltete
	// Konten, E-D-3). nil = keines.
	DiagnoseErlaubt func(kontoID string) bool
	Pfeffer         []byte           // geheimer Wert für die Kennung (HMAC der Google-Konto-ID)
	Jetzt           func() time.Time // nil = time.Now
	// Höchstens so viele Aufrufe des Datensicherungs-Dienstes gleichzeitig;
	// 0 = StandardGleichzeitigeSicherungen.
	GleichzeitigeSicherungen int
}

// NeuerHandler liefert den HTTP-Handler mit allen Diensten. Vor jedem Aufruf
// läuft der Sitzungs-Interceptor; offen sind nur Anmelden und SitzungErneuern.
func NeuerHandler(k Konfiguration) (http.Handler, error) {
	if k.Pruefer == nil || k.Speicher == nil || k.Sicherungen == nil || k.Diagnosen == nil || k.Kontingente == nil {
		return nil, errors.New("Prüfer, Speicher, Sicherungen, Diagnosen und Kontingente sind Pflicht")
	}
	if len(k.Pfeffer) < 32 {
		return nil, errors.New("Pfeffer muss mindestens 32 Bytes lang sein")
	}
	if k.Jetzt == nil {
		k.Jetzt = time.Now
	}
	if k.DiagnoseErlaubt == nil {
		k.DiagnoseErlaubt = func(string) bool { return false }
	}
	if k.GleichzeitigeSicherungen <= 0 {
		k.GleichzeitigeSicherungen = StandardGleichzeitigeSicherungen
	}
	d := &dienste{k: k}

	interceptor := sitzung.NeuerInterceptor(k.Speicher, []string{
		avlreminderv1connect.AnmeldeDienstAnmeldenProcedure,
		avlreminderv1connect.AnmeldeDienstSitzungErneuernProcedure,
	}, k.Jetzt)
	optionen := []connect.HandlerOption{
		connect.WithInterceptors(interceptor),
		connect.WithReadMaxBytes(MaxAnfrage),
		connect.WithRecover(beiPanik),
	}

	mux := http.NewServeMux()
	mux.Handle(avlreminderv1connect.NewAnmeldeDienstHandler(d, optionen...))
	mux.Handle(avlreminderv1connect.NewGeraeteDienstHandler(d, optionen...))
	mux.Handle(avlreminderv1connect.NewSchluesselDienstHandler(d, optionen...))

	// Datensicherungen sind bis zu 8 MiB groß: eigene, größere Grenze für die
	// Anfrage (die spätere Option gilt), dafür nur wenige gleichzeitig. Antworten
	// gehen nie komprimiert hinaus: Das Chiffrat ist zufällig, gzip kostete nur
	// Rechenzeit (komprimierte Anfragen nimmt der Dienst weiter an).
	pfad, sicherungen := avlreminderv1connect.NewDatensicherungDienstHandler(d,
		append(optionen, connect.WithReadMaxBytes(MaxSicherungsanfrage), connect.WithCompressMinBytes(math.MaxInt))...)
	// Reihenfolge: erst die Sitzung (ohne den Inhalt zu lesen), dann die Plätze.
	// So belegen Anfragen ohne Anmeldung weder Plätze noch Speicher.
	mux.Handle(pfad, interceptor.VorDemLesen(begrenze(k.GleichzeitigeSicherungen, sicherungen)))

	// Diagnosen sind bis zu 256 KiB groß; auch hier Sitzung und Freischaltung vor
	// dem Lesen. Sonst könnte jedes angemeldete Konto beliebig oft 256 KiB in den
	// Speicher schieben, ohne je ans Tageslimit zu stoßen.
	pfad, diagnosen := avlreminderv1connect.NewDiagnoseDienstHandler(d,
		append(optionen, connect.WithReadMaxBytes(MaxDiagnoseanfrage))...)
	mux.Handle(pfad, interceptor.VorDemLesen(nurFreigeschaltet(k.DiagnoseErlaubt, diagnosen)))
	return mux, nil
}

// beiPanik fängt einen Absturz in einem Aufruf ab, loggt ihn und antwortet mit
// "internal", statt den ganzen Server zu beenden.
func beiPanik(ctx context.Context, spec connect.Spec, _ http.Header, p any) error {
	slog.ErrorContext(ctx, "Panik im Aufruf", "prozedur", spec.Procedure, "panik", p)
	return connect.NewError(connect.CodeInternal, errors.New("interner Fehler"))
}

// dienste setzt alle Connect-Dienste um (Schlüssel-Dienst in schluessel.go,
// Datensicherung in datensicherung.go).
type dienste struct {
	k Konfiguration
}

// Prüft beim Übersetzen, dass dienste beide Schnittstellen erfüllt.
var (
	_ avlreminderv1connect.AnmeldeDienstHandler = (*dienste)(nil)
	_ avlreminderv1connect.GeraeteDienstHandler = (*dienste)(nil)
)

// Antworten nach außen sind bewusst knapp: Ein Angreifer soll nicht erfahren,
// welche Prüfung gescheitert ist. Der Grund steht im Log. (Funktionen statt
// fester Werte, weil Connect einen Fehler beim Senden verändern darf.)
func errAnmeldungAbgelehnt() error {
	return connect.NewError(connect.CodeUnauthenticated, errors.New("Anmeldung abgelehnt"))
}

func errErneuernAbgelehnt() error {
	return connect.NewError(connect.CodeUnauthenticated, errors.New("Erneuern abgelehnt"))
}

func errGesperrt() error {
	return connect.NewError(connect.CodePermissionDenied, errors.New("Gerät ist gesperrt"))
}

func (d *dienste) Anmelden(ctx context.Context, req *connect.Request[v1.AnmeldenRequest]) (*connect.Response[v1.AnmeldenResponse], error) {
	erg, err := d.k.Pruefer.Pruefe(ctx, anmeldung.Anfrage{
		IDToken:           req.Msg.GetGoogleIdToken(),
		Geraeteschluessel: req.Msg.GetGeraeteschluessel(),
		Signatur:          req.Msg.GetSignatur(),
		NonceZufall:       req.Msg.GetNonceZufall(),
	})
	if errors.Is(err, anmeldung.ErrSchluesselNichtLadbar) {
		// KRITISCH: Kein „abgelehnt“: Die App würde sonst die Anmeldung für
		// gescheitert halten, obwohl nur Google gerade nicht erreichbar ist (E14).
		slog.ErrorContext(ctx, "Google-Schlüssel nicht ladbar", "grund", err.Error())
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("Anmeldung gerade nicht prüfbar – bitte später erneut"))
	}
	if err != nil {
		slog.WarnContext(ctx, "Anmeldung abgelehnt", "grund", err.Error())
		return nil, errAnmeldungAbgelehnt()
	}
	// Erst nach der Prüfung zählen: Nur wer den Geräteschlüssel hat, verbraucht
	// etwas vom Limit dieses Geräts.
	if err := d.verbrauche(ctx, kontingentSitzung, erg.GeraetID, SitzungenJeTag, "Tageslimit für Sitzungen dieses Geräts erreicht – morgen wieder"); err != nil {
		return nil, err
	}
	kennung := anmeldung.Kennung(d.k.Pfeffer, erg.GoogleSub)
	if err := d.neuesGeraetZaehlen(ctx, kennung, erg.GeraetID); err != nil {
		return nil, err
	}

	jetzt := d.k.Jetzt()
	token, neueSitzung := sitzung.Neu(jetzt)
	geraet := speicher.Geraet{ID: erg.GeraetID, Schluessel: req.Msg.GetGeraeteschluessel(), Erstellt: jetzt}
	ergebnis, err := d.k.Speicher.Anmelden(ctx, kennung, geraet, neueSitzung)
	switch {
	case errors.Is(err, speicher.ErrZuVieleGeraete):
		slog.WarnContext(ctx, "Anmeldung abgelehnt: zu viele nicht freigeschaltete Geräte", "geraet", erg.GeraetID)
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("zu viele neue Geräte – erst eins freischalten oder sperren"))
	case errors.Is(err, speicher.ErrGeraetGesperrt):
		slog.WarnContext(ctx, "Anmeldung eines gesperrten Geräts", "geraet", erg.GeraetID)
		return nil, errGesperrt()
	case errors.Is(err, speicher.ErrGeraetFremd):
		slog.WarnContext(ctx, "Anmeldung mit Geräteschlüssel eines anderen Kontos", "geraet", erg.GeraetID)
		return nil, errAnmeldungAbgelehnt()
	case err != nil:
		return nil, speicherFehler(ctx, err)
	}

	slog.InfoContext(ctx, "Gerät angemeldet", "geraet", erg.GeraetID, "neues_konto", ergebnis.NeuesKonto, "freigeschaltet", ergebnis.Freigeschaltet)
	return connect.NewResponse(&v1.AnmeldenResponse{
		KontoId:        ergebnis.KontoID,
		GeraetId:       erg.GeraetID,
		Sitzung:        sitzungNachAussen(token, neueSitzung),
		NeuesKonto:     ergebnis.NeuesKonto,
		Freigeschaltet: ergebnis.Freigeschaltet,
	}), nil
}

// neuesGeraetZaehlen zählt ein Gerät, das der Server noch nicht kennt, im
// Tageszähler des Kontos; ein bekanntes Gerät zählt nicht.
//
// KRITISCH: Gezählt wird vor dem Anlegen, auch wenn das Anmelden danach noch
// scheitert. Zwei gleichzeitige Anmeldungen desselben neuen Geräts zählen
// doppelt; das ist billiger als eine Transaktion über Zähler und Speicher und
// irrt nur zur sicheren Seite.
func (d *dienste) neuesGeraetZaehlen(ctx context.Context, kennung, geraetID string) error {
	_, err := d.k.Speicher.Geraet(ctx, geraetID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, speicher.ErrNichtGefunden) {
		return speicherFehler(ctx, err)
	}
	return d.verbrauche(ctx, kontingentNeuesGeraet, kennung, NeueGeraeteJeTag, "Tageslimit für neue Geräte dieses Kontos erreicht – morgen wieder")
}

func (d *dienste) SitzungErneuern(ctx context.Context, req *connect.Request[v1.SitzungErneuernRequest]) (*connect.Response[v1.SitzungErneuernResponse], error) {
	geraetID := req.Msg.GetGeraetId()
	if !geraeteschluessel.IstGeraetID(geraetID) || req.Msg.GetZeitpunkt().CheckValid() != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("Geräte-ID und Zeitpunkt sind Pflicht"))
	}
	g, err := d.k.Speicher.Geraet(ctx, geraetID)
	if errors.Is(err, speicher.ErrNichtGefunden) {
		slog.WarnContext(ctx, "Erneuern für unbekanntes Gerät", "geraet", geraetID)
		return nil, errErneuernAbgelehnt()
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}

	jetzt := d.k.Jetzt()
	if err := sitzung.PruefeErneuerung(g, req.Msg.GetZeitpunkt().AsTime(), req.Msg.GetSignatur(), jetzt); err != nil {
		slog.WarnContext(ctx, "Erneuern abgelehnt", "geraet", geraetID, "grund", err.Error())
		if errors.Is(err, sitzung.ErrGesperrt) {
			return nil, errGesperrt()
		}
		return nil, errErneuernAbgelehnt()
	}
	if err := d.verbrauche(ctx, kontingentSitzung, geraetID, SitzungenJeTag, "Tageslimit für Sitzungen dieses Geräts erreicht – morgen wieder"); err != nil {
		return nil, err
	}

	token, neueSitzung := sitzung.Neu(jetzt)
	// SitzungAnlegen prüft die Sperre noch einmal in derselben Transaktion –
	// falls das Gerät gerade eben gesperrt wurde.
	if _, err := d.k.Speicher.SitzungAnlegen(ctx, geraetID, neueSitzung); err != nil {
		if errors.Is(err, speicher.ErrGeraetGesperrt) {
			return nil, errGesperrt()
		}
		return nil, speicherFehler(ctx, err)
	}
	return connect.NewResponse(&v1.SitzungErneuernResponse{Sitzung: sitzungNachAussen(token, neueSitzung)}), nil
}

func (d *dienste) GeraeteAuflisten(ctx context.Context, _ *connect.Request[v1.GeraeteAuflistenRequest]) (*connect.Response[v1.GeraeteAuflistenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	liste, err := d.k.Speicher.Geraete(ctx, id.KontoID)
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	antwort := &v1.GeraeteAuflistenResponse{}
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

func (d *dienste) GeraetSperren(ctx context.Context, req *connect.Request[v1.GeraetSperrenRequest]) (*connect.Response[v1.GeraetSperrenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	// KRITISCH (E-B2b-13): Sich selbst sperren (abmelden) darf jedes Gerät, ein
	// anderes nur ein freigeschaltetes. Sonst sperrte ein Dieb mit Google-Login
	// als Erstes die echten Geräte aus.
	if req.Msg.GetGeraetId() != id.GeraetID {
		if err := d.nurFreigeschaltetesGeraet(ctx, id); err != nil {
			return nil, err
		}
	}
	// Eine ID im falschen Format kann es nicht geben (wie unbekannt behandeln).
	if !geraeteschluessel.IstGeraetID(req.Msg.GetGeraetId()) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	}
	// Gesperrt wird nur im eigenen Konto: Die Konto-ID kommt aus der Sitzung,
	// nie aus der Anfrage.
	err = d.k.Speicher.GeraetSperren(ctx, id.KontoID, req.Msg.GetGeraetId(), d.k.Jetzt())
	if errors.Is(err, speicher.ErrNichtGefunden) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("Gerät nicht gefunden"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Gerät gesperrt", "geraet", req.Msg.GetGeraetId(), "durch", id.GeraetID)
	return connect.NewResponse(&v1.GeraetSperrenResponse{}), nil
}

// identitaet holt die vom Interceptor geprüfte Identität. Fehlt sie, ist der
// Handler falsch verdrahtet – dann lieber ablehnen als ohne Konto weitermachen.
func identitaet(ctx context.Context) (sitzung.Identitaet, error) {
	id, ok := sitzung.IdentitaetAus(ctx)
	if !ok || id.KontoID == "" {
		slog.ErrorContext(ctx, "Aufruf ohne Identität – Interceptor fehlt?")
		return sitzung.Identitaet{}, connect.NewError(connect.CodeUnauthenticated, errors.New("Sitzung fehlt"))
	}
	return id, nil
}

func speicherFehler(ctx context.Context, err error) error {
	slog.ErrorContext(ctx, "Speicherfehler", "fehler", err.Error())
	return connect.NewError(connect.CodeUnavailable, errors.New("vorübergehend nicht verfügbar"))
}

func sitzungNachAussen(token string, s speicher.NeueSitzung) *v1.Sitzung {
	return &v1.Sitzung{Token: token, LaeuftAb: timestamppb.New(s.LaeuftAb)}
}
