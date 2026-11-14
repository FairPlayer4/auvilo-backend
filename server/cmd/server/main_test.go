package main

import (
	"slices"
	"testing"
)

func TestLeseKonfiguration(t *testing.T) {
	gut := map[string]string{ //nolint:gosec // nur Namen von Secrets, keine Geheimnisse
		"PROJEKT":                "p",
		"GOOGLE_CLIENT_IDS":      "a.apps, b.apps ,",
		"PFEFFER_SECRET":         "projects/p/secrets/pfeffer/versions/latest",
		"SICHERUNG_BUCKET":       "b",
		"DIAGNOSE_BUCKET":        "d",
		"DIAGNOSE_KONTEN":        " 0123456789abcdef0123456789abcdef ,,fedcba9876543210fedcba9876543210",
		"FREIGEGEBENE_KENNUNGEN": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef, fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
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
		if !slices.Equal(k.clientIDs, []string{"a.apps", "b.apps"}) || k.port != "8080" || k.sicherungBucket != "b" ||
			k.diagnoseBucket != "d" || !slices.Equal(k.diagnoseKonten, []string{"0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"}) {
			t.Errorf("Konfiguration = %+v", k)
		}
	})
	t.Run("liest freigegebene Kennungen", func(t *testing.T) {
		k, err := leseKonfiguration(mit(nil))
		if err != nil || k.alleFreigegeben || !slices.Equal(k.kennungen, []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"}) {
			t.Errorf("Kennungen = %q, alle = %v, Fehler %v", k.kennungen, k.alleFreigegeben, err)
		}
	})
	t.Run("Stern gibt alle frei", func(t *testing.T) {
		k, err := leseKonfiguration(mit(map[string]string{"FREIGEGEBENE_KENNUNGEN": " * "}))
		if err != nil || !k.alleFreigegeben || len(k.kennungen) != 0 {
			t.Errorf("Kennungen = %q, alle = %v, Fehler %v", k.kennungen, k.alleFreigegeben, err)
		}
	})
	// KRITISCH: Ohne Liste startet der Server, lässt aber niemanden zu.
	t.Run("ohne freigegebene Kennungen ist niemand freigegeben", func(t *testing.T) {
		k, err := leseKonfiguration(mit(map[string]string{"FREIGEGEBENE_KENNUNGEN": ""}))
		if err != nil || k.alleFreigegeben || len(k.kennungen) != 0 {
			t.Errorf("Kennungen = %q, alle = %v, Fehler %v", k.kennungen, k.alleFreigegeben, err)
		}
	})
	t.Run("liest lokalen Pfeffer als base64", func(t *testing.T) {
		k, err := leseKonfiguration(mit(map[string]string{"PFEFFER_SECRET": "", "PFEFFER": "aGFsbG8="}))
		if err != nil || string(k.pfeffer) != "hallo" {
			t.Errorf("Pfeffer = %q, Fehler %v", k.pfeffer, err)
		}
	})
	t.Run("ohne Konten mit Diagnose-Erlaubnis nimmt der Server keine Diagnosen an", func(t *testing.T) {
		k, err := leseKonfiguration(mit(map[string]string{"DIAGNOSE_KONTEN": ""}))
		if err != nil || len(k.diagnoseKonten) != 0 {
			t.Errorf("Konten = %q, Fehler %v", k.diagnoseKonten, err)
		}
	})

	fehler := []struct {
		name      string
		aenderung map[string]string
	}{
		{"lehnt fehlendes Projekt ab", map[string]string{"PROJEKT": ""}},
		{"lehnt fehlende Client-IDs ab", map[string]string{"GOOGLE_CLIENT_IDS": " , "}},
		{"lehnt fehlenden Bucket für Sicherungen ab", map[string]string{"SICHERUNG_BUCKET": ""}},
		{"lehnt fehlenden Bucket für Diagnosen ab", map[string]string{"DIAGNOSE_BUCKET": ""}},
		{"lehnt fehlenden Pfeffer ab", map[string]string{"PFEFFER_SECRET": ""}},
		{"lehnt beide Pfeffer-Quellen zugleich ab", map[string]string{"PFEFFER": "aGFsbG8="}},
		{"lehnt Pfeffer ab, der kein base64 ist", map[string]string{"PFEFFER_SECRET": "", "PFEFFER": "%%%"}},
		{"lehnt freigeschaltete Konto-ID mit Großbuchstaben ab", map[string]string{"DIAGNOSE_KONTEN": "0123456789ABCDEF0123456789ABCDEF"}},
		{"lehnt zu kurze freigeschaltete Konto-ID ab", map[string]string{"DIAGNOSE_KONTEN": "0123456789abcdef0123456789abcde"}},
		{"lehnt zu lange freigeschaltete Konto-ID ab", map[string]string{"DIAGNOSE_KONTEN": "0123456789abcdef0123456789abcdef0"}},
		{"lehnt Kennung mit Großbuchstaben ab", map[string]string{"FREIGEGEBENE_KENNUNGEN": "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"}},
		{"lehnt zu kurze Kennung ab", map[string]string{"FREIGEGEBENE_KENNUNGEN": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde"}},
		{"lehnt Konto-ID statt Kennung ab", map[string]string{"FREIGEGEBENE_KENNUNGEN": "0123456789abcdef0123456789abcdef"}},
		{"lehnt Stern neben Kennungen ab", map[string]string{"FREIGEGEBENE_KENNUNGEN": "*, 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},
	}
	for _, tt := range fehler {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := leseKonfiguration(mit(tt.aenderung)); err == nil {
				t.Error("kein Fehler, erwartet einen")
			}
		})
	}
}

func TestErlaubtFuer(t *testing.T) {
	erlaubt := erlaubtFuer([]string{"a1b2", "c3"})
	// KRITISCH: Freigeschaltet ist nur, wer genau so in der Liste steht.
	faelle := []struct {
		name  string
		konto string
		will  bool
	}{
		{"erlaubt genanntes Konto", "a1b2", true},
		{"erlaubt zweites genanntes Konto", "c3", true},
		{"lehnt nicht genanntes Konto ab", "d4", false},
		{"lehnt Teil einer Konto-ID ab", "a1", false},
		{"lehnt längere Konto-ID ab", "a1b2c3", false},
		{"lehnt leere Konto-ID ab", "", false},
	}
	for _, f := range faelle {
		t.Run(f.name, func(t *testing.T) {
			if got := erlaubt(f.konto); got != f.will {
				t.Errorf("erlaubt(%q) = %v, erwartet %v", f.konto, got, f.will)
			}
		})
	}
	t.Run("leere Liste erlaubt niemandem etwas", func(t *testing.T) {
		if erlaubtFuer(nil)("a1b2") {
			t.Error("erlaubt")
		}
	})
}

func TestBefehlAus(t *testing.T) {
	erlaubt := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"aufraeumen"}, "aufraeumen"},
	}
	for _, f := range erlaubt {
		got, err := befehlAus(f.args)
		if err != nil || got != f.want || befehle[got] == nil {
			t.Errorf("befehlAus(%q) = %q, %v; erwartet %q", f.args, got, err, f.want)
		}
	}
	// Ein Tippfehler startet weder Server noch Lauf.
	for _, args := range [][]string{{"aufräumen"}, {"aufraeumen", "jetzt"}, {""}, {"server"}} {
		if got, err := befehlAus(args); err == nil {
			t.Errorf("befehlAus(%q) = %q ohne Fehler", args, got)
		}
	}
}

func TestLeseAblageKonfiguration(t *testing.T) {
	gut := map[string]string{"PROJEKT": "p", "SICHERUNG_BUCKET": "b", "DIAGNOSE_BUCKET": "d"}
	a, err := leseAblageKonfiguration(func(k string) string { return gut[k] })
	if err != nil || a != (ablageKonfiguration{"p", "b", "d"}) {
		t.Errorf("Konfiguration = %+v, Fehler %v", a, err)
	}
	// Der Lauf braucht weder Client-IDs noch Pfeffer, aber jeden der drei Werte.
	for name := range gut {
		ohne := func(k string) string {
			if k == name {
				return ""
			}
			return gut[k]
		}
		if _, err := leseAblageKonfiguration(ohne); err == nil {
			t.Errorf("ohne %s kein Fehler", name)
		}
	}
}
