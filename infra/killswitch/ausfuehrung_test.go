package killswitch

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// attrappe ersetzt GCP in Tests und merkt sich, was in welcher Reihenfolge
// aufgerufen wurde.
type attrappe struct {
	dienste     int
	sperrFehler error
	trennFehler error
	aufrufe     []string
}

func (a *attrappe) DiensteSperren(context.Context) (int, error) {
	a.aufrufe = append(a.aufrufe, "sperren")
	return a.dienste, a.sperrFehler
}

func (a *attrappe) AbrechnungTrennen(context.Context) error {
	a.aufrufe = append(a.aufrufe, "trennen")
	return a.trennFehler
}

func (a *attrappe) gesperrt() bool { return slices.Contains(a.aufrufe, "sperren") }
func (a *attrappe) getrennt() bool { return slices.Contains(a.aufrufe, "trennen") }

func TestAusfuehren(t *testing.T) {
	sperrKaputt := errors.New("Sperren-API-Fehler")
	trennKaputt := errors.New("Billing-API-Fehler")
	tests := []struct {
		name         string
		aktion       Aktion
		p            attrappe
		wantAufrufe  []string // genaue Reihenfolge der Eingriffe
		wantErgTrenn bool
		wantUrsachen []error // diese Fehler müssen per errors.Is im Ergebnis stecken
	}{
		{"nichts: keine Eingriffe", Nichts, attrappe{dienste: 2}, nil, false, nil},
		{"sperren: nur Dienste sperren, nicht trennen", DiensteSperren, attrappe{dienste: 2}, []string{"sperren"}, false, nil},
		{"sperren: Fehler wird gemeldet", DiensteSperren, attrappe{sperrFehler: sperrKaputt}, []string{"sperren"}, false, []error{sperrKaputt}},
		{"trennen: sperrt zuerst und trennt dann", AbrechnungTrennen, attrappe{dienste: 1}, []string{"sperren", "trennen"}, true, nil},
		{"trennen: trennt auch, wenn Sperren scheitert", AbrechnungTrennen, attrappe{sperrFehler: sperrKaputt}, []string{"sperren", "trennen"}, true, []error{sperrKaputt}},
		{"trennen: Fehler beim Trennen wird gemeldet", AbrechnungTrennen, attrappe{trennFehler: trennKaputt}, []string{"sperren", "trennen"}, false, []error{trennKaputt}},
		{"trennen: scheitern beide, werden beide Fehler gemeldet", AbrechnungTrennen, attrappe{sperrFehler: sperrKaputt, trennFehler: trennKaputt}, []string{"sperren", "trennen"}, false, []error{sperrKaputt, trennKaputt}},
		{"unbekannte Aktion: Fehler, keine Eingriffe", Aktion(42), attrappe{}, nil, false, []error{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.p
			erg, err := Ausfuehren(context.Background(), tt.aktion, &p)
			wantErr := tt.wantUrsachen != nil
			if (err != nil) != wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, wantErr)
			}
			for _, ursache := range tt.wantUrsachen {
				if !errors.Is(err, ursache) {
					t.Errorf("Ursache %q fehlt im Fehler %q", ursache, err)
				}
			}
			if !slices.Equal(p.aufrufe, tt.wantAufrufe) {
				t.Errorf("Aufrufe = %v, erwartet %v", p.aufrufe, tt.wantAufrufe)
			}
			if erg.AbrechnungGetrennt != tt.wantErgTrenn {
				t.Errorf("Ergebnis getrennt = %v, erwartet %v", erg.AbrechnungGetrennt, tt.wantErgTrenn)
			}
			wantDienste := 0
			if p.gesperrt() {
				wantDienste = tt.p.dienste
			}
			if erg.GesperrteDienste != wantDienste {
				t.Errorf("gesperrte Dienste = %d, erwartet %d", erg.GesperrteDienste, wantDienste)
			}
		})
	}
}
