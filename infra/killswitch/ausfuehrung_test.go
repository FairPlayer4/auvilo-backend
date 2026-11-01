package killswitch

import (
	"context"
	"errors"
	"testing"
)

// attrappe ersetzt GCP in Tests und merkt sich, was aufgerufen wurde.
type attrappe struct {
	dienste     int
	sperrFehler error
	trennFehler error
	gesperrt    bool
	getrennt    bool
}

func (a *attrappe) DiensteSperren(context.Context) (int, error) {
	a.gesperrt = true
	return a.dienste, a.sperrFehler
}

func (a *attrappe) AbrechnungTrennen(context.Context) error {
	a.getrennt = true
	return a.trennFehler
}

func TestAusfuehren(t *testing.T) {
	kaputt := errors.New("API-Fehler")
	tests := []struct {
		name         string
		aktion       Aktion
		p            attrappe
		wantGesperrt bool
		wantGetrennt bool
		wantErgTrenn bool
		wantErr      bool
	}{
		{"nichts: keine Eingriffe", Nichts, attrappe{dienste: 2}, false, false, false, false},
		{"sperren: nur Dienste sperren, nicht trennen", DiensteSperren, attrappe{dienste: 2}, true, false, false, false},
		{"sperren: Fehler wird gemeldet", DiensteSperren, attrappe{sperrFehler: kaputt}, true, false, false, true},
		{"trennen: sperrt zuerst und trennt dann", AbrechnungTrennen, attrappe{dienste: 1}, true, true, true, false},
		{"trennen: trennt auch, wenn Sperren scheitert", AbrechnungTrennen, attrappe{sperrFehler: kaputt}, true, true, true, true},
		{"trennen: Fehler beim Trennen wird gemeldet", AbrechnungTrennen, attrappe{trennFehler: kaputt}, true, true, false, true},
		{"unbekannte Aktion: Fehler, keine Eingriffe", Aktion(42), attrappe{}, false, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p
			erg, err := Ausfuehren(context.Background(), tt.aktion, &p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if err != nil && tt.p.sperrFehler != nil && !errors.Is(err, kaputt) {
				t.Errorf("Ursache geht verloren: %v", err)
			}
			if p.gesperrt != tt.wantGesperrt {
				t.Errorf("gesperrt = %v, erwartet %v", p.gesperrt, tt.wantGesperrt)
			}
			if p.getrennt != tt.wantGetrennt {
				t.Errorf("Trennen aufgerufen = %v, erwartet %v", p.getrennt, tt.wantGetrennt)
			}
			if erg.AbrechnungGetrennt != tt.wantErgTrenn {
				t.Errorf("Ergebnis getrennt = %v, erwartet %v", erg.AbrechnungGetrennt, tt.wantErgTrenn)
			}
			wantDienste := 0
			if tt.wantGesperrt {
				wantDienste = tt.p.dienste
			}
			if erg.GesperrteDienste != wantDienste {
				t.Errorf("gesperrte Dienste = %d, erwartet %d", erg.GesperrteDienste, wantDienste)
			}
		})
	}
}
