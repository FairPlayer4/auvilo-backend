package killswitch

import (
	"context"
	"errors"
	"slices"
	"testing"

	"cloud.google.com/go/iam/apiv1/iampb"
	"google.golang.org/protobuf/proto"
)

// cloudRunAttrappe ersetzt Cloud Run und merkt sich, welche Dienste gesperrt wurden.
type cloudRunAttrappe struct {
	dienste    []Dienst
	listFehler error
	fehlerBei  map[string]error // voller Name → Fehler beim Sperren
	schonZu    map[string]bool  // voller Name → beim Sperren schon gesperrt vorgefunden
	gesperrt   []string
}

func (c *cloudRunAttrappe) Dienste(context.Context) ([]Dienst, error) {
	return c.dienste, c.listFehler
}

func (c *cloudRunAttrappe) Sperre(_ context.Context, name string) (bool, error) {
	if err := c.fehlerBei[name]; err != nil {
		return false, err
	}
	if c.schonZu[name] {
		return false, nil
	}
	c.gesperrt = append(c.gesperrt, name)
	return true, nil
}

func TestSperreAlle(t *testing.T) {
	const pfad = "projects/p/locations/europe-west1/services/"
	api := Dienst{Name: pfad + "api"}
	web := Dienst{Name: pfad + "web"}
	killswitch := Dienst{Name: pfad + "killswitch"}
	schonGesperrt := Dienst{Name: pfad + "gesperrt", Gesperrt: true}
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
		{"fasst schon gesperrte Dienste nicht an", cloudRunAttrappe{dienste: []Dienst{schonGesperrt, api}}, "killswitch", []string{api.Name}, Sperrung{Gefunden: 2, Gesperrt: 1}, nil},
		{"ohne Dienste: nichts zu tun", cloudRunAttrappe{}, "killswitch", nil, Sperrung{}, nil},
		// KRITISCH: Gefunden zählt den Kill-Switch nicht mit, sonst gäbe es bei
		// falscher REGION keine Warnung, solange er selbst dort läuft.
		{"nur der eigene Dienst: nichts gefunden", cloudRunAttrappe{dienste: []Dienst{killswitch}}, "killswitch", nil, Sperrung{}, nil},
		{"alles schon gesperrt: gefunden, aber nichts neu gesperrt", cloudRunAttrappe{dienste: []Dienst{schonGesperrt}}, "killswitch", nil, Sperrung{Gefunden: 1}, nil},
		// Beim Auflisten offen (oder Richtlinie nicht lesbar), beim Sperren schon
		// zu: kein Eingriff, sonst gäbe es eine WARN-Mail ohne Grund.
		{"inzwischen gesperrter Dienst zählt nicht als neu gesperrt", cloudRunAttrappe{dienste: []Dienst{api, web}, schonZu: map[string]bool{api.Name: true}}, "killswitch", []string{web.Name}, Sperrung{Gefunden: 2, Gesperrt: 1}, nil},
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

// bindung baut eine Rolle mit ihren Mitgliedern (Kurzschreibweise für die Tabelle).
func bindung(rolle string, mitglieder ...string) *iampb.Binding {
	return &iampb.Binding{Role: rolle, Members: mitglieder}
}

func TestOhneOeffentlich(t *testing.T) {
	const aufrufen = "roles/run.invoker"
	const killswitch = "serviceAccount:killswitch@p.iam.gserviceaccount.com"
	const deploy = "serviceAccount:deploy@p.iam.gserviceaccount.com"

	tests := []struct {
		name          string
		vorher        []*iampb.Binding
		nachher       []*iampb.Binding
		wantGeaendert bool
	}{
		// KRITISCH: Das ist die eigentliche Sperre.
		{"entzieht allUsers das Aufrufrecht",
			[]*iampb.Binding{bindung(aufrufen, "allUsers")},
			nil, true},
		{"entzieht auch allAuthenticatedUsers das Aufrufrecht",
			[]*iampb.Binding{bindung(aufrufen, "allAuthenticatedUsers")},
			nil, true},
		// KRITISCH: Sonst sperrte sich der Kill-Switch aus oder nähme dem
		// Deploy seine Rechte.
		{"lässt andere Mitglieder und Rollen stehen",
			[]*iampb.Binding{bindung(aufrufen, killswitch, "allUsers"), bindung("roles/run.developer", deploy)},
			[]*iampb.Binding{bindung(aufrufen, killswitch), bindung("roles/run.developer", deploy)}, true},
		{"entfernt das Internet auch aus anderen Rollen",
			[]*iampb.Binding{bindung("roles/run.servicesInvoker", "allUsers"), bindung(aufrufen, killswitch)},
			[]*iampb.Binding{bindung(aufrufen, killswitch)}, true},
		{"ohne öffentliche Mitglieder ändert sich nichts",
			[]*iampb.Binding{bindung(aufrufen, killswitch)},
			[]*iampb.Binding{bindung(aufrufen, killswitch)}, false},
		{"ähnliche Namen bleiben (nur genaue Treffer zählen)",
			[]*iampb.Binding{bindung(aufrufen, "user:allusers@example.com")},
			[]*iampb.Binding{bindung(aufrufen, "user:allusers@example.com")}, false},
		{"leere Richtlinie bleibt leer", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &iampb.Policy{Bindings: tt.vorher, Etag: []byte("etag")}
			warOeffentlich := istOeffentlich(p)
			if got := ohneOeffentlich(p); got != tt.wantGeaendert {
				t.Errorf("geändert = %v, erwartet %v", got, tt.wantGeaendert)
			}
			want := &iampb.Policy{Bindings: tt.nachher, Etag: []byte("etag")}
			if !proto.Equal(p, want) {
				t.Errorf("Richtlinie = %v, erwartet %v", p, want)
			}
			// Was geändert wurde, war öffentlich, und danach ist nichts mehr öffentlich.
			if warOeffentlich != tt.wantGeaendert {
				t.Errorf("istOeffentlich vorher = %v, erwartet %v", warOeffentlich, tt.wantGeaendert)
			}
			if istOeffentlich(p) {
				t.Errorf("nach dem Sperren noch öffentlich: %v", p)
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
