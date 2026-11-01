// testanmeldung spielt ein Gerät gegenüber dem Server: echte Google-Anmeldung
// im Browser, Geräteschlüssel in einer Datei. Damit lässt sich B1 von Hand
// ausprobieren, bevor es den Kotlin-Client gibt (B2).
//
//	go run ./cmd/testanmeldung [-server URL] [-geraet NAME] anmelden
//	go run ./cmd/testanmeldung [-geraet NAME] erneuern
//	go run ./cmd/testanmeldung [-geraet NAME] geraete
//	go run ./cmd/testanmeldung [-geraet NAME] sperren <geraet-id>
//
// Schlüssel, Sitzungen und die Client-Datei von Google liegen in
// %APPDATA%\avlreminder (Schalter -ordner), nie im Repo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
)

const hilfe = `Befehle:
  anmelden          mit Google anmelden (Browser) und Sitzung speichern
  erneuern          Sitzung mit dem Geräteschlüssel erneuern
  geraete           Geräte des Kontos auflisten
  sperren <id>      Gerät sperren (eigenes Gerät = abmelden)
Schalter:`

func main() {
	if err := ausfuehren(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func ausfuehren(ctx context.Context, args []string, aus io.Writer) error {
	standardOrdner := "avlreminder"
	if d, err := os.UserConfigDir(); err == nil { // unter Windows %APPDATA%
		standardOrdner = filepath.Join(d, "avlreminder")
	}
	flags := flag.NewFlagSet("testanmeldung", flag.ContinueOnError)
	flags.SetOutput(aus)
	server := flags.String("server", "http://localhost:8080", "Adresse des Servers")
	name := flags.String("geraet", "pc", "Name des simulierten Geräts (mehrere Namen = mehrere Geräte)")
	ordner := flags.String("ordner", standardOrdner, "Ordner für Schlüssel, Sitzungen und Client-Datei")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(aus, hilfe)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // -h: Hilfe ist schon ausgegeben
	} else if err != nil {
		return err
	}

	w := neuesWerkzeug(*server, ablage{ordner: *ordner}, aus)
	rest := flags.Args()
	if len(rest) == 0 {
		flags.Usage()
		return errors.New("Befehl fehlt")
	}
	switch {
	case rest[0] == "anmelden" && len(rest) == 1:
		return w.anmelden(ctx, *name)
	case rest[0] == "erneuern" && len(rest) == 1:
		return w.erneuern(ctx, *name)
	case rest[0] == "geraete" && len(rest) == 1:
		return w.geraeteAuflisten(ctx, *name)
	case rest[0] == "sperren" && len(rest) == 2:
		return w.sperren(ctx, *name, rest[1])
	default:
		flags.Usage()
		return fmt.Errorf("unbekannter Aufruf: %v", rest)
	}
}

// werkzeug bündelt, was alle Befehle brauchen. idToken ist austauschbar,
// damit Tests ohne Browser und echtes Google auskommen.
type werkzeug struct {
	anmelde avlreminderv1connect.AnmeldeDienstClient
	geraete avlreminderv1connect.GeraeteDienstClient
	ablage  ablage
	aus     io.Writer
	jetzt   func() time.Time
	idToken func(ctx context.Context, nonce string) (string, error)
}

func neuesWerkzeug(server string, a ablage, aus io.Writer) *werkzeug {
	c := &http.Client{Timeout: 30 * time.Second}
	w := &werkzeug{
		anmelde: avlreminderv1connect.NewAnmeldeDienstClient(c, server),
		geraete: avlreminderv1connect.NewGeraeteDienstClient(c, server),
		ablage:  a,
		aus:     aus,
		jetzt:   time.Now,
	}
	w.idToken = func(ctx context.Context, nonce string) (string, error) {
		pfad, err := findeClientDatei(a.ordner)
		if err != nil {
			return "", err
		}
		daten, err := os.ReadFile(pfad) //nolint:gosec // Datei im eigenen Ordner
		if err != nil {
			return "", err
		}
		cfg, err := leseClient(daten)
		if err != nil {
			return "", err
		}
		return googleIDToken(ctx, cfg, nonce, oeffneBrowser)
	}
	return w
}

func (w *werkzeug) anmelden(ctx context.Context, name string) error {
	g, err := w.ablage.geraet(name)
	if err != nil {
		return err
	}
	// Die Nonce bindet das ID-Token an genau diesen Geräteschlüssel.
	token, err := w.idToken(ctx, geraeteschluessel.Nonce(g.der))
	if err != nil {
		return err
	}
	sig, err := g.signiere(geraeteschluessel.AnmeldeNachricht(token))
	if err != nil {
		return err
	}
	res, err := w.anmelde.Anmelden(ctx, connect.NewRequest(&v1.AnmeldenRequest{
		GoogleIdToken:     token,
		Geraeteschluessel: g.der,
		Signatur:          sig,
	}))
	if err != nil {
		return err
	}
	m := res.Msg
	if err := w.merke(name, m.GetGeraetId(), m.GetSitzung()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w.aus, "angemeldet: Konto %s, Gerät %s, neues Konto: %v, Sitzung bis %s\n",
		m.GetKontoId(), m.GetGeraetId(), m.GetNeuesKonto(), zeit(m.GetSitzung().GetLaeuftAb()))
	return nil
}

func (w *werkzeug) erneuern(ctx context.Context, name string) error {
	g, err := w.ablage.geraet(name)
	if err != nil {
		return err
	}
	// Ganze Sekunden: signiert werden Unix-Sekunden, und der Server rechnet mit
	// genau dem Zeitpunkt aus der Anfrage.
	zeitpunkt := w.jetzt().Truncate(time.Second)
	sig, err := g.signiere(geraeteschluessel.SitzungsNachricht(g.id(), zeitpunkt))
	if err != nil {
		return err
	}
	res, err := w.anmelde.SitzungErneuern(ctx, connect.NewRequest(&v1.SitzungErneuernRequest{
		GeraetId:  g.id(),
		Zeitpunkt: timestamppb.New(zeitpunkt),
		Signatur:  sig,
	}))
	if err != nil {
		return err
	}
	if err := w.merke(name, g.id(), res.Msg.GetSitzung()); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w.aus, "erneuert: Sitzung bis %s\n", zeit(res.Msg.GetSitzung().GetLaeuftAb()))
	return nil
}

func (w *werkzeug) geraeteAuflisten(ctx context.Context, name string) error {
	req, err := mitSitzung(w, name, &v1.GeraeteAuflistenRequest{})
	if err != nil {
		return err
	}
	res, err := w.geraete.GeraeteAuflisten(ctx, req)
	if err != nil {
		return err
	}
	for _, g := range res.Msg.GetGeraete() {
		markierung := ""
		if g.GetDiesesGeraet() {
			markierung = "  (dieses Gerät)"
		}
		if g.GetGesperrt() {
			markierung += "  GESPERRT"
		}
		_, _ = fmt.Fprintf(w.aus, "%s  angemeldet %s%s\n", g.GetGeraetId(), zeit(g.GetAngemeldetAm()), markierung)
	}
	return nil
}

func (w *werkzeug) sperren(ctx context.Context, name, geraetID string) error {
	req, err := mitSitzung(w, name, &v1.GeraetSperrenRequest{GeraetId: geraetID})
	if err != nil {
		return err
	}
	if _, err := w.geraete.GeraetSperren(ctx, req); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w.aus, "gesperrt: %s\n", geraetID)
	return nil
}

// mitSitzung baut eine Anfrage mit der gespeicherten Sitzung im Kopf. Eine
// Funktion statt Methode, weil Go keine generischen Methoden kennt.
func mitSitzung[T any](w *werkzeug, name string, msg *T) (*connect.Request[T], error) {
	s, err := w.ablage.sitzung(name)
	if err != nil {
		return nil, err
	}
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+s.Token)
	return req, nil
}

func (w *werkzeug) merke(name, geraetID string, s *v1.Sitzung) error {
	return w.ablage.speichereSitzung(name, gespeicherteSitzung{
		GeraetID: geraetID,
		Token:    s.GetToken(),
		LaeuftAb: s.GetLaeuftAb().AsTime(),
	})
}

func zeit(t *timestamppb.Timestamp) string {
	return t.AsTime().Local().Format("02.01.2006 15:04:05")
}

// oeffneBrowser öffnet die Adresse im Standardbrowser.
func oeffneBrowser(adresse string) error {
	programm, args := "xdg-open", []string{adresse}
	switch runtime.GOOS {
	case "windows":
		programm, args = "rundll32", []string{"url.dll,FileProtocolHandler", adresse}
	case "darwin":
		programm = "open"
	}
	// Die Adresse baut googleIDToken selbst aus festen Teilen; kein Shell-Aufruf.
	return exec.Command(programm, args...).Start() //nolint:gosec // siehe oben
}
