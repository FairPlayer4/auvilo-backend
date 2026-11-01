package killswitch

import (
	"errors"
	"testing"
)

func TestEntscheide(t *testing.T) {
	budget := 20.0
	tests := []struct {
		name    string
		kosten  float64
		budget  float64
		want    Aktion
		wantErr error
	}{
		{"keine Kosten: nichts tun", 0, budget, Nichts, nil},
		{"knapp unter 100 %: nichts tun", 19.99, budget, Nichts, nil},
		{"genau 100 %: Dienste sperren", 20, budget, DiensteSperren, nil},
		{"zwischen 100 und 150 %: Dienste sperren", 25, budget, DiensteSperren, nil},
		{"knapp unter 150 %: noch nicht trennen", 29.99, budget, DiensteSperren, nil},
		{"genau 150 %: Abrechnung trennen", 30, budget, AbrechnungTrennen, nil},
		{"weit darüber: Abrechnung trennen", 1000, budget, AbrechnungTrennen, nil},
		{"lehnt Budget 0 ab", 5, 0, Nichts, ErrUngueltigesBudget},
		{"lehnt negatives Budget ab", 5, -1, Nichts, ErrUngueltigesBudget},
		{"lehnt negative Kosten ab", -1, budget, Nichts, ErrUngueltigeKosten},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BudgetMeldung{CostAmount: tt.kosten, BudgetAmount: tt.budget}
			got, err := Entscheide(m, StandardSchwellen)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Fehler = %v, erwartet %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Aktion = %v, erwartet %v", got, tt.want)
			}
		})
	}
}

func TestSchwellenPruefe(t *testing.T) {
	tests := []struct {
		name    string
		s       Schwellen
		wantErr bool
	}{
		{"Standard ist gültig", StandardSchwellen, false},
		{"gleiche Schwellen sind gültig", Schwellen{1, 1}, false},
		{"lehnt Sperren über Trennen ab", Schwellen{2, 1}, true},
		{"lehnt Sperren 0 ab", Schwellen{0, 1}, true},
		{"lehnt negatives Trennen ab", Schwellen{1, -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Pruefe()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Fehler = %v, erwartet Fehler: %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrUngueltigeSchwellen) {
				t.Errorf("falscher Fehlertyp: %v", err)
			}
		})
	}
}

func TestEntscheideMitUngueltigenSchwellenTutNichts(t *testing.T) {
	got, err := Entscheide(BudgetMeldung{CostAmount: 100, BudgetAmount: 20}, Schwellen{2, 1})
	if !errors.Is(err, ErrUngueltigeSchwellen) {
		t.Fatalf("Fehler = %v, erwartet ErrUngueltigeSchwellen", err)
	}
	if got != Nichts {
		t.Errorf("Aktion = %v, erwartet nichts", got)
	}
}

func TestAktionString(t *testing.T) {
	if got := Aktion(99).String(); got != "Aktion(99)" {
		t.Errorf("String = %q", got)
	}
	for _, a := range []Aktion{Nichts, DiensteSperren, AbrechnungTrennen} {
		if a.String() == "" {
			t.Errorf("leerer Name für %d", int(a))
		}
	}
}
