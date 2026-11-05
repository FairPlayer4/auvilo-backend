package killswitch

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// cloudRunAttrappe ersetzt Cloud Run und merkt sich, welche Dienste gesperrt wurden.
type cloudRunAttrappe struct {
	dienste    []Dienst
	listFehler error
	fehlerBei  map[string]error // voller Name → Fehler beim Sperren
	gesperrt   []string
}

func (c *cloudRunAttrappe) Dienste(context.Context) ([]Dienst, error) {
	return c.dienste, c.listFehler
}

func (c *cloudRunAttrappe) Sperre(_ context.Context, name string) error {
	if err := c.fehlerBei[name]; err != nil {
		return err
	}
	c.gesperrt = append(c.gesperrt, name)
	return nil
}

func TestSperreAlle(t *testing.T) {
	const pfad = "projects/p/locations/europe-west1/services/"
	api := Dienst{Name: pfad + "api"}
	web := Dienst{Name: pfad + "web"}
	killswitch := Dienst{Name: pfad + "killswitch"}
	schonIntern := Dienst{Name: pfad + "intern", Intern: true}
	listKaputt := errors.New("List-Fehler")
	apiKaputt := errors.New("Update-Fehler")

	tests := []struct {
		name         string
		cr           cloudRunAttrappe
		eigener      string
		wantGesperrt []string
		wantSperrung Sperrung
		wantUrsache  error // nil = kein Fehler erwartet
	}{
		{"sperrt alle offenen Dienste", cloudRunAttrappe{dienste: []Dienst{api, web}}, "killswitch", []string{api.Name, web.Name}, Sperrung{Gefunden: 2, Gesperrt: 2}, nil},
		{"sperrt den eigenen Dienst nicht", cloudRunAttrappe{dienste: []Dienst{killswitch, api}}, "killswitch", []string{api.Name}, Sperrung{Gefunden: 1, Gesperrt: 1}, nil},
		{"fasst schon interne Dienste nicht an", cloudRunAttrappe{dienste: []Dienst{schonIntern, api}}, "killswitch", []string{api.Name}, Sperrung{Gefunden: 2, Gesperrt: 1}, nil},
		{"ohne Dienste: nichts zu tun", cloudRunAttrappe{}, "killswitch", nil, Sperrung{}, nil},
		// KRITISCH: Gefunden zählt den Kill-Switch nicht mit, sonst gäbe es bei
		// falscher REGION keine Warnung, solange er selbst dort läuft.
		{"nur der eigene Dienst: nichts gefunden", cloudRunAttrappe{dienste: []Dienst{killswitch}}, "killswitch", nil, Sperrung{}, nil},
		{"alles schon intern: gefunden, aber nichts neu gesperrt", cloudRunAttrappe{dienste: []Dienst{schonIntern}}, "killswitch", nil, Sperrung{Gefunden: 1}, nil},
		{"Fehler bei einem Dienst hält die anderen nicht auf", cloudRunAttrappe{dienste: []Dienst{api, web}, fehlerBei: map[string]error{api.Name: apiKaputt}}, "killswitch", []string{web.Name}, Sperrung{Gefunden: 2, Gesperrt: 1}, apiKaputt},
		{"scheitert das Auflisten, wird nichts gesperrt", cloudRunAttrappe{dienste: []Dienst{api}, listFehler: listKaputt}, "killswitch", nil, Sperrung{}, listKaputt},
		{"ohne eigenen Namen werden alle offenen gesperrt", cloudRunAttrappe{dienste: []Dienst{api}}, "", []string{api.Name}, Sperrung{Gefunden: 1, Gesperrt: 1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := tt.cr
			sp, err := SperreAlle(context.Background(), &cr, tt.eigener)
			if tt.wantUrsache == nil && err != nil {
				t.Fatalf("unerwarteter Fehler: %v", err)
			}
			if tt.wantUrsache != nil && !errors.Is(err, tt.wantUrsache) {
				t.Fatalf("Fehler = %v, erwartet Ursache %v", err, tt.wantUrsache)
			}
			if !slices.Equal(cr.gesperrt, tt.wantGesperrt) {
				t.Errorf("gesperrt = %v, erwartet %v", cr.gesperrt, tt.wantGesperrt)
			}
			if sp != tt.wantSperrung {
				t.Errorf("Sperrung = %+v, erwartet %+v", sp, tt.wantSperrung)
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
