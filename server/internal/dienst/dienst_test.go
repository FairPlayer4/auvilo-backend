package dienst

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/testhilfe"
)

// Diese Tests laufen über echtes HTTP gegen den vollständigen Handler – so, wie
// später ein Client den Server anspricht. Nur Google, Firestore und Cloud Storage
// sind ersetzt.

const clientID = "test-client.apps.googleusercontent.com"

var start = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

type testServer struct {
	google   *testhilfe.Google
	anmelde  avlreminderv1connect.AnmeldeDienstClient
	geraete  avlreminderv1connect.GeraeteDienstClient
	backup   avlreminderv1connect.SchluesselDienstClient
	sicherer avlreminderv1connect.DatensicherungDienstClient
	diagnose avlreminderv1connect.DiagnoseDienstClient
	speicher *speicher.Arbeitsspeicher
	jetzt    time.Time // die Uhr des Servers; Tests stellen sie vor
	url      string    // Adresse des Testservers (für Anfragen ohne Connect-Client)

	diagnosen      *diagnose.Arbeitsspeicher
	diagnoseKonten map[string]bool // Konten, die Diagnosen senden dürfen
}

func neuerTestServer(t *testing.T) *testServer {
	t.Helper()
	return neuerTestServerMit(t, datensicherung.NeuerArbeitsspeicher(), 0)
}

// neuerTestServerMit erlaubt eine eigene Ablage für Sicherungen und eine eigene
// Grenze für gleichzeitige Sicherungen (0 = Standard); aendern passt die
// Konfiguration weiter an (z. B. ein kaputter Tageszähler).
func neuerTestServerMit(t *testing.T, sicherungen datensicherung.Ablage, gleichzeitig int, aendern ...func(*Konfiguration)) *testServer {
	t.Helper()
	ts := &testServer{
		google:         testhilfe.NeuesGoogle(t, clientID),
		speicher:       speicher.NeuerArbeitsspeicher(),
		jetzt:          start,
		diagnosen:      diagnose.NeuerArbeitsspeicher(),
		diagnoseKonten: map[string]bool{},
	}
	uhr := func() time.Time { return ts.jetzt }
	pruefer, err := anmeldung.NeuerPruefer(ts.google.Schluessel(), []string{clientID}, uhr)
	if err != nil {
		t.Fatal(err)
	}
	k := Konfiguration{
		Pruefer:                  pruefer,
		Speicher:                 ts.speicher,
		Sicherungen:              sicherungen,
		Diagnosen:                ts.diagnosen,
		Kontingente:              kontingent.NeuerArbeitsspeicher(),
		DiagnoseErlaubt:          func(k string) bool { return ts.diagnoseKonten[k] },
		Pfeffer:                  []byte("0123456789abcdef0123456789abcdef"),
		Jetzt:                    uhr,
		GleichzeitigeSicherungen: gleichzeitig,
	}
	for _, a := range aendern {
		a(&k)
	}
	h, err := NeuerHandler(k)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ts.url = srv.URL
	ts.anmelde = avlreminderv1connect.NewAnmeldeDienstClient(http.DefaultClient, srv.URL)
	ts.geraete = avlreminderv1connect.NewGeraeteDienstClient(http.DefaultClient, srv.URL)
	ts.backup = avlreminderv1connect.NewSchluesselDienstClient(http.DefaultClient, srv.URL)
	ts.sicherer = avlreminderv1connect.NewDatensicherungDienstClient(http.DefaultClient, srv.URL)
	ts.diagnose = avlreminderv1connect.NewDiagnoseDienstClient(http.DefaultClient, srv.URL)
	return ts
}

func (ts *testServer) anmeldeAnfrage(t *testing.T, sub string, g *testhilfe.Geraet) *v1.AnmeldenRequest {
	t.Helper()
	zufall := testhilfe.NonceZufall(t)
	token := ts.google.IDToken(t, sub, g, zufall, ts.jetzt)
	return &v1.AnmeldenRequest{
		GoogleIdToken:     token,
		Geraeteschluessel: g.DER,
		Signatur:          g.Signiere(t, geraeteschluessel.AnmeldeNachricht(token)),
		NonceZufall:       zufall,
	}
}

func (ts *testServer) anmelden(t *testing.T, sub string, g *testhilfe.Geraet) *v1.AnmeldenResponse {
	t.Helper()
	res, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(ts.anmeldeAnfrage(t, sub, g)))
	if err != nil {
		t.Fatalf("Anmelden: %v", err)
	}
	return res.Msg
}

func (ts *testServer) erneuerAnfrage(t *testing.T, g *testhilfe.Geraet, zeitpunkt time.Time) *v1.SitzungErneuernRequest {
	t.Helper()
	return &v1.SitzungErneuernRequest{
		GeraetId:  g.ID(),
		Zeitpunkt: timestamppb.New(zeitpunkt),
		Signatur:  g.Signiere(t, geraeteschluessel.SitzungsNachricht(g.ID(), zeitpunkt)),
	}
}

// mitSitzung baut eine Anfrage mit Sitzungs-Token im Header.
func mitSitzung[T any](msg *T, token string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+token)
	return req
}

func erwarteCode(t *testing.T, err error, code connect.Code) {
	t.Helper()
	if connect.CodeOf(err) != code {
		t.Fatalf("Code = %v (%v), erwartet %v", connect.CodeOf(err), err, code)
	}
}

func TestErstesAnmeldenLegtKontoAnUndSitzungGilt(t *testing.T) {
	ts := neuerTestServer(t)
	handy := testhilfe.NeuesGeraet(t)

	res := ts.anmelden(t, "google-kevin", handy)
	if !res.NeuesKonto || res.KontoId == "" || res.GeraetId != handy.ID() {
		t.Fatalf("Antwort = %v", res)
	}
	if !res.Sitzung.LaeuftAb.AsTime().Equal(start.Add(time.Hour)) {
		t.Errorf("Sitzung läuft ab %v, erwartet in einer Stunde", res.Sitzung.LaeuftAb.AsTime())
	}

	liste, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, res.Sitzung.Token))
	if err != nil {
		t.Fatal(err)
	}
	if len(liste.Msg.Geraete) != 1 || !liste.Msg.Geraete[0].DiesesGeraet || liste.Msg.Geraete[0].Gesperrt {
		t.Errorf("Geräte = %v", liste.Msg.Geraete)
	}
}

func TestZweitesGeraetLandetImSelbenKonto(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	erstes := ts.anmelden(t, "google-kevin", handy)
	zweites := ts.anmelden(t, "google-kevin", laptop)
	if zweites.NeuesKonto || zweites.KontoId != erstes.KontoId {
		t.Fatalf("zweites = %v, erstes = %v", zweites, erstes)
	}
	liste, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, zweites.Sitzung.Token))
	if err != nil {
		t.Fatal(err)
	}
	if len(liste.Msg.Geraete) != 2 {
		t.Fatalf("Geräte = %v, erwartet 2", liste.Msg.Geraete)
	}
}

func TestAnmeldenLehntUngueltigeAnfragenAb(t *testing.T) {
	ts := neuerTestServer(t)
	handy, fremd := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)

	tests := []struct {
		name    string
		aendere func(r *v1.AnmeldenRequest)
	}{
		{"lehnt leere Anfrage ab", func(r *v1.AnmeldenRequest) { *r = v1.AnmeldenRequest{} }},
		{"lehnt Signatur eines anderen Geräts ab", func(r *v1.AnmeldenRequest) {
			r.Signatur = fremd.Signiere(t, geraeteschluessel.AnmeldeNachricht(r.GoogleIdToken))
		}},
		{"lehnt Token mit fremdem Geräteschlüssel ab", func(r *v1.AnmeldenRequest) {
			r.Geraeteschluessel = fremd.DER
			r.Signatur = fremd.Signiere(t, geraeteschluessel.AnmeldeNachricht(r.GoogleIdToken))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ts.anmeldeAnfrage(t, "google-kevin", handy)
			tt.aendere(req)
			_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(req))
			erwarteCode(t, err, connect.CodeUnauthenticated)
		})
	}

	t.Run("lehnt veraltetes Token ab", func(t *testing.T) {
		req := ts.anmeldeAnfrage(t, "google-kevin", handy)
		ts.jetzt = start.Add(5*time.Minute + time.Second) // ID-Token gilt 5 Minuten (b1-entwurf.md)
		defer func() { ts.jetzt = start }()
		_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(req))
		erwarteCode(t, err, connect.CodeUnauthenticated)
	})

	t.Run("lehnt Geräteschlüssel ab, der schon zu einem anderen Konto gehört", func(t *testing.T) {
		ts.anmelden(t, "google-kevin", handy)
		_, err := ts.anmelde.Anmelden(context.Background(), connect.NewRequest(ts.anmeldeAnfrage(t, "google-andere", handy)))
		erwarteCode(t, err, connect.CodeUnauthenticated)
	})
}

func TestSitzungErneuern(t *testing.T) {
	ts := neuerTestServer(t)
	handy, unbekannt := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	ts.anmelden(t, "google-kevin", handy)
	ctx := context.Background()

	t.Run("erneuert ohne Google-Login, neue Sitzung gilt", func(t *testing.T) {
		ts.jetzt = start.Add(50 * time.Minute)
		res, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, handy, ts.jetzt)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, res.Msg.Sitzung.Token)); err != nil {
			t.Errorf("neue Sitzung gilt nicht: %v", err)
		}
	})

	tests := []struct {
		name    string
		anfrage func() *v1.SitzungErneuernRequest
		code    connect.Code
	}{
		{"lehnt unbekanntes Gerät ab", func() *v1.SitzungErneuernRequest { return ts.erneuerAnfrage(t, unbekannt, ts.jetzt) }, connect.CodeUnauthenticated},
		{"lehnt veralteten Zeitpunkt ab", func() *v1.SitzungErneuernRequest {
			return ts.erneuerAnfrage(t, handy, ts.jetzt.Add(-3*time.Minute))
		}, connect.CodeUnauthenticated},
		{"lehnt Signatur eines anderen Geräts ab", func() *v1.SitzungErneuernRequest {
			r := ts.erneuerAnfrage(t, handy, ts.jetzt)
			r.Signatur = unbekannt.Signiere(t, geraeteschluessel.SitzungsNachricht(handy.ID(), ts.jetzt))
			return r
		}, connect.CodeUnauthenticated},
		{"lehnt Anfrage ohne Zeitpunkt ab", func() *v1.SitzungErneuernRequest {
			r := ts.erneuerAnfrage(t, handy, ts.jetzt)
			r.Zeitpunkt = nil
			return r
		}, connect.CodeInvalidArgument},
		{"lehnt Anfrage ohne Geräte-ID ab", func() *v1.SitzungErneuernRequest {
			r := ts.erneuerAnfrage(t, handy, ts.jetzt)
			r.GeraetId = ""
			return r
		}, connect.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(tt.anfrage()))
			erwarteCode(t, err, tt.code)
		})
	}
}

func TestSitzungLaeuftNachEinerStundeAb(t *testing.T) {
	ts := neuerTestServer(t)
	res := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t))
	ts.jetzt = start.Add(time.Hour)
	_, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, res.Sitzung.Token))
	erwarteCode(t, err, connect.CodeUnauthenticated)
}

func TestGeraeteDienstVerlangtSitzung(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	_, err := ts.geraete.GeraeteAuflisten(ctx, connect.NewRequest(&v1.GeraeteAuflistenRequest{}))
	erwarteCode(t, err, connect.CodeUnauthenticated)
	_, err = ts.geraete.GeraetSperren(ctx, connect.NewRequest(&v1.GeraetSperrenRequest{GeraetId: "x"}))
	erwarteCode(t, err, connect.CodeUnauthenticated)
}

// Der wichtigste Ablauf: Ein verlorenes Handy wird vom Laptop aus gesperrt und
// ist sofort draußen – mit der alten Sitzung, beim Erneuern und beim erneuten
// Google-Login.
func TestGesperrtesGeraetIstSofortDraussen(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	handySitzung := ts.anmelden(t, "google-kevin", handy).Sitzung.Token
	laptopSitzung := ts.anmelden(t, "google-kevin", laptop).Sitzung.Token
	ts.geraetFreischaltenOK(t, handySitzung, laptop.ID()) // Sperren anderer Geräte braucht das

	if _, err := ts.geraete.GeraetSperren(ctx, mitSitzung(&v1.GeraetSperrenRequest{GeraetId: handy.ID()}, laptopSitzung)); err != nil {
		t.Fatalf("Sperren: %v", err)
	}

	t.Run("alte Sitzung des Handys gilt nicht mehr", func(t *testing.T) {
		_, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, handySitzung))
		erwarteCode(t, err, connect.CodeUnauthenticated)
	})
	t.Run("Handy bekommt keine neue Sitzung", func(t *testing.T) {
		_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, handy, ts.jetzt)))
		erwarteCode(t, err, connect.CodePermissionDenied)
	})
	t.Run("Handy kann sich auch mit Google nicht wieder anmelden", func(t *testing.T) {
		_, err := ts.anmelde.Anmelden(ctx, connect.NewRequest(ts.anmeldeAnfrage(t, "google-kevin", handy)))
		erwarteCode(t, err, connect.CodePermissionDenied)
	})
	t.Run("Laptop arbeitet weiter und sieht das Handy als gesperrt", func(t *testing.T) {
		liste, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, laptopSitzung))
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range liste.Msg.Geraete {
			if g.GeraetId == handy.ID() && !g.Gesperrt {
				t.Error("Handy nicht als gesperrt gelistet")
			}
		}
	})
}

func TestEigenesGeraetSperrenHeisstAbmelden(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	handy := testhilfe.NeuesGeraet(t)
	token := ts.anmelden(t, "google-kevin", handy).Sitzung.Token
	if _, err := ts.geraete.GeraetSperren(ctx, mitSitzung(&v1.GeraetSperrenRequest{GeraetId: handy.ID()}, token)); err != nil {
		t.Fatal(err)
	}
	_, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, token))
	erwarteCode(t, err, connect.CodeUnauthenticated)
}

func TestFremdesGeraetSperrenWirdAbgelehnt(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	meins, fremdes := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	meineSitzung := ts.anmelden(t, "google-kevin", meins).Sitzung.Token
	fremdeSitzung := ts.anmelden(t, "google-andere", fremdes).Sitzung.Token

	_, err := ts.geraete.GeraetSperren(ctx, mitSitzung(&v1.GeraetSperrenRequest{GeraetId: fremdes.ID()}, meineSitzung))
	erwarteCode(t, err, connect.CodeNotFound)

	// Das fremde Gerät arbeitet unverändert weiter.
	if _, err := ts.geraete.GeraeteAuflisten(ctx, mitSitzung(&v1.GeraeteAuflistenRequest{}, fremdeSitzung)); err != nil {
		t.Errorf("fremdes Gerät betroffen: %v", err)
	}
}

func TestGeraeteListeZeigtNurEigeneGeraete(t *testing.T) {
	ts := neuerTestServer(t)
	meins, fremdes := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	token := ts.anmelden(t, "google-kevin", meins).Sitzung.Token
	ts.anmelden(t, "google-andere", fremdes)
	liste, err := ts.geraete.GeraeteAuflisten(context.Background(), mitSitzung(&v1.GeraeteAuflistenRequest{}, token))
	if err != nil {
		t.Fatal(err)
	}
	if len(liste.Msg.Geraete) != 1 || liste.Msg.Geraete[0].GeraetId != meins.ID() {
		t.Errorf("Geräte = %v, erwartet nur das eigene", liste.Msg.Geraete)
	}
}

func TestNeuerHandlerPrueftDieKonfiguration(t *testing.T) {
	gut := func() Konfiguration {
		return Konfiguration{
			Pruefer:     &anmeldung.Pruefer{},
			Speicher:    speicher.NeuerArbeitsspeicher(),
			Sicherungen: datensicherung.NeuerArbeitsspeicher(),
			Diagnosen:   diagnose.NeuerArbeitsspeicher(),
			Kontingente: kontingent.NeuerArbeitsspeicher(),
			Pfeffer:     []byte("0123456789abcdef0123456789abcdef"),
		}
	}
	t.Run("nimmt vollständige Konfiguration an", func(t *testing.T) {
		if _, err := NeuerHandler(gut()); err != nil {
			t.Fatal(err)
		}
	})
	faelle := []struct {
		name    string
		aendern func(k *Konfiguration)
	}{
		{"lehnt kurzen Pfeffer ab", func(k *Konfiguration) { k.Pfeffer = []byte("kurz") }},
		{"lehnt fehlenden Prüfer ab", func(k *Konfiguration) { k.Pruefer = nil }},
		{"lehnt fehlenden Speicher ab", func(k *Konfiguration) { k.Speicher = nil }},
		{"lehnt fehlende Ablage für Sicherungen ab", func(k *Konfiguration) { k.Sicherungen = nil }},
		{"lehnt fehlende Ablage für Diagnosen ab", func(k *Konfiguration) { k.Diagnosen = nil }},
		{"lehnt fehlenden Tageszähler ab", func(k *Konfiguration) { k.Kontingente = nil }},
	}
	for _, fall := range faelle {
		t.Run(fall.name, func(t *testing.T) {
			k := gut()
			fall.aendern(&k)
			if _, err := NeuerHandler(k); err == nil {
				t.Error("angenommen, erwartet Fehler")
			}
		})
	}
}

// Geräte-IDs aus Anfragen haben ein festes Format. Alles andere wird abgelehnt,
// bevor der Speicher sie sieht (Firestore lehnt z. B. "a/b" mit einem Fehler ab,
// der sonst als Speicherfehler im Log landet).
func TestUngueltigeGeraeteIDWirdOhneSpeicherAbgelehnt(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	handy := testhilfe.NeuesGeraet(t)
	token := ts.anmelden(t, "google-kevin", handy).Sitzung.Token

	tests := []struct {
		name string
		id   string
	}{
		{"Schrägstrich", "a/b"},
		{"reservierter Name", "__x__"},
		{"Großbuchstaben", strings.ToUpper(handy.ID())},
		{"zu kurz", handy.ID()[:31]},
		{"zu lang", handy.ID() + "0"},
	}
	for _, tt := range tests {
		t.Run("Erneuern: "+tt.name, func(t *testing.T) {
			req := ts.erneuerAnfrage(t, handy, ts.jetzt)
			req.GeraetId = tt.id
			_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(req))
			erwarteCode(t, err, connect.CodeInvalidArgument)
		})
		t.Run("Sperren: "+tt.name, func(t *testing.T) {
			_, err := ts.geraete.GeraetSperren(ctx, mitSitzung(&v1.GeraetSperrenRequest{GeraetId: tt.id}, token))
			erwarteCode(t, err, connect.CodeNotFound)
		})
	}
}

// Bewusst erlaubt (Entscheidung 2026-11-01, anforderungen.md Kap. 11): Dieselbe
// Anfrage gilt innerhalb ihres Zeitfensters mehrmals. Schutz kommt von TLS, der
// kurzen Gültigkeit und der Sitzungsdauer von 1 Stunde. Wird dieser Test rot,
// weil Wiederholungen abgelehnt werden, ist das eine Änderung der Entscheidung.
func TestWiederholteAnfrageImZeitfensterGiltBewusst(t *testing.T) {
	ts := neuerTestServer(t)
	ctx := context.Background()
	handy := testhilfe.NeuesGeraet(t)

	anmelden := ts.anmeldeAnfrage(t, "google-kevin", handy)
	erste, err := ts.anmelde.Anmelden(ctx, connect.NewRequest(anmelden))
	if err != nil {
		t.Fatal(err)
	}
	zweite, err := ts.anmelde.Anmelden(ctx, connect.NewRequest(anmelden))
	if err != nil {
		t.Fatalf("Wiederholung der Anmeldung abgelehnt: %v", err)
	}
	if erste.Msg.Sitzung.Token == zweite.Msg.Sitzung.Token {
		t.Error("Wiederholung lieferte dasselbe Token")
	}

	erneuern := ts.erneuerAnfrage(t, handy, ts.jetzt)
	for i := range 2 {
		if _, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(erneuern)); err != nil {
			t.Fatalf("Erneuern Nr. %d abgelehnt: %v", i+1, err)
		}
	}
}

// Die Grenzen aus anmeldung.proto als eigene Zahlen (nicht SitzungenJeTag,
// speicher.MaxOffeneGeraete).
const (
	sitzungenAmTag = 200
	offeneGeraete  = 10
)

// KRITISCH: Ein Gerät bekommt am Tag (UTC) nur begrenzt viele Sitzungen,
// über Anmelden und SitzungErneuern zusammen (Entscheidung nach Review-Lauf 8).
// Abgelehnte Anfragen zählen nicht; jedes Gerät hat sein eigenes Limit.
func TestSitzungenHabenEinTageslimitJeGeraet(t *testing.T) {
	ts := neuerTestServer(t)
	handy, laptop := testhilfe.NeuesGeraet(t), testhilfe.NeuesGeraet(t)
	ctx := context.Background()
	erneuern := func(g *testhilfe.Geraet) error {
		_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, g, ts.jetzt)))
		return err
	}
	anmelden := func(g *testhilfe.Geraet) error {
		_, err := ts.anmelde.Anmelden(ctx, connect.NewRequest(ts.anmeldeAnfrage(t, "google-kevin", g)))
		return err
	}

	ts.anmelden(t, "google-kevin", handy)
	for i := range sitzungenAmTag - 2 {
		if err := erneuern(handy); err != nil {
			t.Fatalf("Erneuern %d: %v", i+1, err)
		}
	}
	// Abgelehnt, bevor gezählt wird: falsche Signatur, veralteter Zeitpunkt.
	for range 3 {
		r := ts.erneuerAnfrage(t, handy, ts.jetzt)
		r.Signatur = laptop.Signiere(t, geraeteschluessel.SitzungsNachricht(handy.ID(), ts.jetzt))
		_, err := ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(r))
		erwarteCode(t, err, connect.CodeUnauthenticated)
		_, err = ts.anmelde.SitzungErneuern(ctx, connect.NewRequest(ts.erneuerAnfrage(t, handy, ts.jetzt.Add(-3*time.Minute))))
		erwarteCode(t, err, connect.CodeUnauthenticated)
	}
	if err := anmelden(handy); err != nil {
		t.Fatalf("letzte erlaubte Sitzung: %v", err)
	}

	t.Run("danach lehnt der Server Erneuern und Anmelden ab", func(t *testing.T) {
		for _, err := range []error{erneuern(handy), anmelden(handy)} {
			erwarteCode(t, err, connect.CodeResourceExhausted)
			if !strings.Contains(err.Error(), "Tageslimit") {
				t.Errorf("Meldung nennt das Tageslimit nicht: %v", err)
			}
		}
	})
	t.Run("ein anderes Gerät desselben Kontos hat sein eigenes Limit", func(t *testing.T) {
		if err := anmelden(laptop); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("am nächsten Tag (UTC) geht es wieder", func(t *testing.T) {
		ts.jetzt = time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 1, 0, time.UTC)
		if err := erneuern(handy); err != nil {
			t.Fatal(err)
		}
	})
}

// KRITISCH: Ein Konto hat höchstens 10 nicht freigeschaltete, ungesperrte
// Geräte (Entscheidung nach Review-Lauf 8). Sonst könnte, wer nur den
// Google-Login hat, beliebig viele Geräte anlegen (jedes mit eigenem
// Sitzungslimit, und die Geräteliste der App liefe voll). Die Zählung im
// Einzelnen prüft der Vertragstest des Speichers.
func TestZuVieleNeueGeraeteWerdenAbgewiesen(t *testing.T) {
	ts := neuerTestServer(t)
	handySitzung := ts.anmelden(t, "google-kevin", testhilfe.NeuesGeraet(t)).Sitzung.Token
	neue := make([]*testhilfe.Geraet, offeneGeraete)
	for i := range neue {
		neue[i] = testhilfe.NeuesGeraet(t)
		ts.anmelden(t, "google-kevin", neue[i])
	}
	ctx := context.Background()
	weiteres := testhilfe.NeuesGeraet(t)

	_, err := ts.anmelde.Anmelden(ctx, connect.NewRequest(ts.anmeldeAnfrage(t, "google-kevin", weiteres)))
	erwarteCode(t, err, connect.CodeResourceExhausted)
	if _, err := ts.speicher.Geraet(ctx, weiteres.ID()); !errors.Is(err, speicher.ErrNichtGefunden) {
		t.Errorf("abgewiesenes Gerät ist angelegt (Fehler %v)", err)
	}
	t.Run("ein vorhandenes Gerät meldet sich weiter an", func(t *testing.T) {
		ts.anmelden(t, "google-kevin", neue[0])
	})
	t.Run("nach dem Freischalten eines Geräts ist wieder Platz", func(t *testing.T) {
		ts.geraetFreischaltenOK(t, handySitzung, neue[0].ID())
		ts.anmelden(t, "google-kevin", weiteres)
	})
}
