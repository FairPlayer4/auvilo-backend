package killswitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"
)

// init läuft automatisch beim Laden des Pakets (wie ein statischer Konstruktor
// in C#). Hier melden wir die Funktion beim Functions Framework an; der Name
// "KillSwitch" ist der Einstiegspunkt in der Cloud-Function-Konfiguration.
func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: cloudLoggingFelder})))
	functions.CloudEvent("KillSwitch", behandleEreignis)
}

// cloudLoggingFelder benennt die slog-Felder so um, wie Cloud Logging sie
// erwartet: "severity" statt "level" und "message" statt "msg". Dann zeigt die
// Console Schweregrad und Text richtig an.
func cloudLoggingFelder(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// pubsubNachricht ist die Hülle, in der Eventarc eine Pub/Sub-Nachricht liefert.
// Data ist im JSON base64-kodiert; Go dekodiert das für []byte automatisch.
type pubsubNachricht struct {
	Message struct {
		Data []byte `json:"data"`
	} `json:"message"`
}

func behandleEreignis(ctx context.Context, e event.Event) error {
	var huelle pubsubNachricht
	if err := e.DataAs(&huelle); err != nil {
		return fmt.Errorf("Pub/Sub-Hülle lesen: %w", err)
	}

	s, err := schwellenAusUmgebung()
	if err != nil {
		return err
	}
	p, err := neueGCPPlattform(ctx, os.Getenv("PROJEKT"), os.Getenv("REGION"), os.Getenv("K_SERVICE"))
	if err != nil {
		return err
	}
	_, err = Verarbeite(ctx, huelle.Message.Data, s, p)
	// Fehler beim Schließen werden an den eigentlichen Fehler angehängt.
	return errors.Join(err, p.Schliessen())
}

// ErrKostenFehlen meldet eine Budget-Meldung ohne Feld costAmount. Ohne diese
// Prüfung würde json.Unmarshal 0 einsetzen, und die Meldung ginge still als
// "nichts tun" durch (etwa wenn Google das Feld umbenennt).
var ErrKostenFehlen = errors.New("Budget-Meldung ohne costAmount")

// Verarbeite ist der testbare Kern: Meldung lesen, entscheiden, ausführen, loggen.
// Geloggt werden nur Kennzahlen und Fehlertexte, keine Inhalte aus der App.
// Jeder Fehler – auch ein früher beim Lesen der Meldung – landet als ERROR im
// Log, damit er in der Console auffällt.
func Verarbeite(ctx context.Context, daten []byte, s Schwellen, p Plattform) (Ergebnis, error) {
	m, err := leseMeldung(daten)
	if err != nil {
		slog.ErrorContext(ctx, "Budget-Meldung abgelehnt", "fehler", err.Error())
		return Ergebnis{}, err
	}
	a, err := Entscheide(m, s)
	if err != nil {
		slog.ErrorContext(ctx, "Budget-Meldung abgelehnt", "fehler", err.Error())
		return Ergebnis{}, err
	}
	erg, err := Ausfuehren(ctx, a, p)
	stufe := slog.LevelInfo
	if err != nil {
		stufe = slog.LevelError
	} else if erg.Aktion != Nichts {
		stufe = slog.LevelWarn // ein Eingriff soll in der Console auffallen
	}
	slog.Log(ctx, stufe, "Budget-Meldung verarbeitet",
		"budget", m.BudgetDisplayName,
		"kosten", m.CostAmount,
		"betrag", m.BudgetAmount,
		"waehrung", m.CurrencyCode,
		"aktion", erg.Aktion.String(),
		"gesperrteDienste", erg.GesperrteDienste,
		"abrechnungGetrennt", erg.AbrechnungGetrennt,
		"fehler", fehlerText(err),
	)
	return erg, err
}

// leseMeldung liest das JSON und prüft, dass costAmount vorhanden ist. Ein
// Zeiger (*float64) unterscheidet "fehlt" (nil) von "ist 0".
func leseMeldung(daten []byte) (BudgetMeldung, error) {
	var m BudgetMeldung
	if err := json.Unmarshal(daten, &m); err != nil {
		return m, fmt.Errorf("Budget-Meldung lesen: %w", err)
	}
	var vorhanden struct {
		CostAmount *float64 `json:"costAmount"`
	}
	if err := json.Unmarshal(daten, &vorhanden); err != nil {
		return m, fmt.Errorf("Budget-Meldung lesen: %w", err)
	}
	if vorhanden.CostAmount == nil {
		return m, ErrKostenFehlen
	}
	return m, nil
}

func fehlerText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// schwellenAusUmgebung liest die Schwellen aus Umgebungsvariablen (gesetzt von
// OpenTofu). Fehlt eine Variable, gilt der Standardwert.
func schwellenAusUmgebung() (Schwellen, error) {
	s := StandardSchwellen
	for name, ziel := range map[string]*float64{
		"SCHWELLE_SPERREN": &s.Sperren,
		"SCHWELLE_TRENNEN": &s.Trennen,
	} {
		wert := os.Getenv(name)
		if wert == "" {
			continue
		}
		f, err := strconv.ParseFloat(wert, 64)
		if err != nil {
			return s, fmt.Errorf("%s=%q ist keine Zahl: %w", name, wert, err)
		}
		*ziel = f
	}
	return s, s.Pruefe()
}
