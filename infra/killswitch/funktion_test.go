package killswitch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// stufenRekorder merkt sich die Log-Stufe jeder Meldung. Der E-Mail-Alarm
// (infra/alarm.tf) hängt an der Stufe: ab WARNING kommt eine Mail.
type stufenRekorder struct{ stufen *[]slog.Level }

func (r stufenRekorder) Enabled(context.Context, slog.Level) bool { return true }
func (r stufenRekorder) Handle(_ context.Context, rec slog.Record) error {
	*r.stufen = append(*r.stufen, rec.Level)
	return nil
}
func (r stufenRekorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r stufenRekorder) WithGroup(string) slog.Handler      { return r }

func TestVerarbeite(t *testing.T) {
	sperrKaputt := errors.New("Sperren-API-Fehler")
	tests := []struct {
		name         string
		daten        string
		sperrFehler  error
		neuGesperrt  int // so viele Dienste sperrt die Attrappe neu
		wantAktion   Aktion
		wantGetrennt bool
		wantErr      bool
		// KRITISCH: Eingriffe und Fehler müssen mindestens WARN sein, sonst
		// kommt keine Alarm-Mail.
		wantStufe slog.Level
	}{
		{"echte Meldung unter Budget: nichts", `{"budgetDisplayName":"b","costAmount":3.5,"budgetAmount":20,"currencyCode":"EUR","alertThresholdExceeded":0.25}`, nil, 0, Nichts, false, false, slog.LevelInfo},
		{"Meldung über Budget: sperren", `{"costAmount":21,"budgetAmount":20}`, nil, 2, DiensteSperren, false, false, slog.LevelWarn},
		// Budget meldet mehrmals am Tag. Ist schon alles gesperrt, soll nicht jede
		// Meldung eine Mail auslösen.
		{"erneute Meldung über Budget, alles schon gesperrt: nur INFO", `{"costAmount":21,"budgetAmount":20}`, nil, 0, DiensteSperren, false, false, slog.LevelInfo},
		{"Meldung weit über Budget: trennen, auch ohne neu gesperrte Dienste", `{"costAmount":31,"budgetAmount":20}`, nil, 0, AbrechnungTrennen, true, false, slog.LevelWarn},
		{"Fehler beim Sperren wird als ERROR geloggt", `{"costAmount":21,"budgetAmount":20}`, sperrKaputt, 0, DiensteSperren, false, true, slog.LevelError},
		{"lehnt kaputtes JSON ab", `{kaputt`, nil, 0, Nichts, false, true, slog.LevelError},
		{"lehnt Meldung ohne Budget ab", `{"costAmount":21}`, nil, 0, Nichts, false, true, slog.LevelError},
		{"lehnt Meldung ohne Kosten ab", `{"budgetAmount":20}`, nil, 0, Nichts, false, true, slog.LevelError},
		{"Kosten 0 sind gültig, wenn angegeben", `{"costAmount":0,"budgetAmount":20}`, nil, 0, Nichts, false, false, slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verarbeite loggt über den globalen Logger; für den Test austauschen.
			var stufen []slog.Level
			alt := slog.Default()
			slog.SetDefault(slog.New(stufenRekorder{&stufen}))
			t.Cleanup(func() { slog.SetDefault(alt) })

			p := &attrappe{sperrFehler: tt.sperrFehler, dienste: tt.neuGesperrt}
			erg, err := Verarbeite(context.Background(), []byte(tt.daten), StandardSchwellen, p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if erg.Aktion != tt.wantAktion {
				t.Errorf("Aktion = %v, erwartet %v", erg.Aktion, tt.wantAktion)
			}
			if p.getrennt() != tt.wantGetrennt {
				t.Errorf("getrennt = %v, erwartet %v", p.getrennt(), tt.wantGetrennt)
			}
			if tt.name == "lehnt Meldung ohne Kosten ab" && !errors.Is(err, ErrKostenFehlen) {
				t.Errorf("Fehler = %v, erwartet ErrKostenFehlen", err)
			}
			if tt.wantErr && tt.sperrFehler == nil && (p.gesperrt() || p.getrennt()) {
				t.Error("bei ungültiger Meldung darf nichts passieren")
			}
			if len(stufen) != 1 || stufen[0] != tt.wantStufe {
				t.Errorf("Log-Stufen = %v, erwartet genau eine Meldung mit %v", stufen, tt.wantStufe)
			}
		})
	}
}

func TestSchwellenAusUmgebung(t *testing.T) {
	tests := []struct {
		name    string
		sperren string
		trennen string
		want    Schwellen
		wantErr bool
	}{
		{"ohne Variablen: Standard", "", "", StandardSchwellen, false},
		{"eigene Werte", "0.8", "1.2", Schwellen{0.8, 1.2}, false},
		{"lehnt Text ab", "viel", "", StandardSchwellen, true},
		{"lehnt NaN ab", "NaN", "", StandardSchwellen, true},
		{"lehnt Sperren über Trennen ab", "2", "1", Schwellen{2, 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv setzt die Variable nur für diesen Test und räumt danach auf.
			t.Setenv("SCHWELLE_SPERREN", tt.sperren)
			t.Setenv("SCHWELLE_TRENNEN", tt.trennen)
			got, err := schwellenAusUmgebung()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Schwellen = %+v, erwartet %+v", got, tt.want)
			}
		})
	}
}

func TestCloudLoggingFelder(t *testing.T) {
	tests := []struct {
		ein, want string
	}{
		{slog.LevelKey, "severity"},
		{slog.MessageKey, "message"},
		{"kosten", "kosten"},
	}
	for _, tt := range tests {
		if got := cloudLoggingFelder(nil, slog.String(tt.ein, "x")).Key; got != tt.want {
			t.Errorf("Feld %q wird zu %q, erwartet %q", tt.ein, got, tt.want)
		}
	}
}

func TestFehlerText(t *testing.T) {
	if fehlerText(nil) != "" || fehlerText(errors.New("x")) != "x" {
		t.Error("fehlerText falsch")
	}
}
