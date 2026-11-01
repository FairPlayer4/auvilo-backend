package killswitch

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// attrappe ersetzt GCP in Tests und merkt sich, was in welcher Reihenfolge
// aufgerufen wurde.
type attrappe struct {
	dienste     int
	sperrFehler error
	trennFehler error
	haengt      bool  // Sperren wartet, bis sein Zeitlimit abläuft
	trennCtxErr error // Zustand des Kontexts beim Trennen
	aufrufe     []string
}

func (a *attrappe) DiensteSperren(ctx context.Context) (int, error) {
	a.aufrufe = append(a.aufrufe, "sperren")
	if a.haengt {
		<-ctx.Done() // wartet, bis das Zeitlimit abläuft
		return 0, ctx.Err()
	}
	return a.dienste, a.sperrFehler
}

func (a *attrappe) AbrechnungTrennen(ctx context.Context) error {
	a.aufrufe = append(a.aufrufe, "trennen")
	a.trennCtxErr = ctx.Err()
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

// KRITISCH: Ein hängendes Sperren darf das Trennen nicht verhindern. Das
// Sperren läuft in einen eigenen, kürzeren Zeitlimit; das Trennen bekommt
// danach einen Kontext, der noch gilt.
func TestTrennenTrotzHaengendemSperren(t *testing.T) {
	alt := sperrZeitlimit
	sperrZeitlimit = 50 * time.Millisecond
	t.Cleanup(func() { sperrZeitlimit = alt })

	// Die Funktion selbst hat mehr Zeit als das Sperren.
	ctx, abbrechen := context.WithTimeout(context.Background(), 5*time.Second)
	defer abbrechen()

	p := attrappe{haengt: true}
	erg, err := Ausfuehren(ctx, AbrechnungTrennen, &p)
	if !p.getrennt() || !erg.AbrechnungGetrennt {
		t.Fatalf("nicht getrennt: Aufrufe %v", p.aufrufe)
	}
	if p.trennCtxErr != nil {
		t.Errorf("Trennen bekam einen abgelaufenen Kontext: %v", p.trennCtxErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Fehler = %v, erwartet den Zeitlimit-Fehler des Sperrens", err)
	}
}
