package main

import (
	"slices"
	"testing"
)

func TestLeseKonfiguration(t *testing.T) {
	gut := map[string]string{ //nolint:gosec // nur Namen von Secrets, keine Geheimnisse
		"PROJEKT":           "p",
		"GOOGLE_CLIENT_IDS": "a.apps, b.apps ,",
		"PFEFFER_SECRET":    "projects/p/secrets/pfeffer/versions/latest",
	}
	mit := func(aenderungen map[string]string) func(string) string {
		m := map[string]string{}
		for k, v := range gut {
			m[k] = v
		}
		for k, v := range aenderungen {
			m[k] = v
		}
		return func(k string) string { return m[k] }
	}

	t.Run("liest gültige Konfiguration", func(t *testing.T) {
		k, err := leseKonfiguration(mit(nil))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(k.clientIDs, []string{"a.apps", "b.apps"}) || k.port != "8080" {
			t.Errorf("Konfiguration = %+v", k)
		}
	})
	t.Run("liest lokalen Pfeffer als base64", func(t *testing.T) {
		k, err := leseKonfiguration(mit(map[string]string{"PFEFFER_SECRET": "", "PFEFFER": "aGFsbG8="}))
		if err != nil || string(k.pfeffer) != "hallo" {
			t.Errorf("Pfeffer = %q, Fehler %v", k.pfeffer, err)
		}
	})

	fehler := []struct {
		name      string
		aenderung map[string]string
	}{
		{"lehnt fehlendes Projekt ab", map[string]string{"PROJEKT": ""}},
		{"lehnt fehlende Client-IDs ab", map[string]string{"GOOGLE_CLIENT_IDS": " , "}},
		{"lehnt fehlenden Pfeffer ab", map[string]string{"PFEFFER_SECRET": ""}},
		{"lehnt beide Pfeffer-Quellen zugleich ab", map[string]string{"PFEFFER": "aGFsbG8="}},
		{"lehnt Pfeffer ab, der kein base64 ist", map[string]string{"PFEFFER_SECRET": "", "PFEFFER": "%%%"}},
	}
	for _, tt := range fehler {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := leseKonfiguration(mit(tt.aenderung)); err == nil {
				t.Error("kein Fehler, erwartet einen")
			}
		})
	}
}
