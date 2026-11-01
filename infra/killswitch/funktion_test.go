package killswitch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestVerarbeite(t *testing.T) {
	tests := []struct {
		name         string
		daten        string
		wantAktion   Aktion
		wantGetrennt bool
		wantErr      bool
	}{
		{"echte Meldung unter Budget: nichts", `{"budgetDisplayName":"b","costAmount":3.5,"budgetAmount":20,"currencyCode":"EUR","alertThresholdExceeded":0.25}`, Nichts, false, false},
		{"Meldung über Budget: sperren", `{"costAmount":21,"budgetAmount":20}`, DiensteSperren, false, false},
		{"Meldung weit über Budget: trennen", `{"costAmount":31,"budgetAmount":20}`, AbrechnungTrennen, true, false},
		{"lehnt kaputtes JSON ab", `{kaputt`, Nichts, false, true},
		{"lehnt Meldung ohne Budget ab", `{"costAmount":21}`, Nichts, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &attrappe{}
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
			if tt.wantErr && (p.gesperrt() || p.getrennt()) {
				t.Error("bei ungültiger Meldung darf nichts passieren")
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

func TestKurzname(t *testing.T) {
	tests := map[string]string{
		"projects/p/locations/r/services/api": "api",
		"api":                                 "api",
		"":                                    "",
	}
	for ein, want := range tests {
		if got := kurzname(ein); got != want {
			t.Errorf("kurzname(%q) = %q, erwartet %q", ein, got, want)
		}
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
