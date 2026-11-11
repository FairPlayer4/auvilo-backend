package dienst

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	// Lädt alle Deskriptoren des Pakets avlreminder.v1 in die Registry.
	_ "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
)

// alleProzeduren liest alle Prozeduren aus den erzeugten Deskriptoren, also
// auch solche, die später dazukommen.
func alleProzeduren(t *testing.T) []string {
	t.Helper()
	var p []string
	protoregistry.GlobalFiles.RangeFilesByPackage("avlreminder.v1", func(f protoreflect.FileDescriptor) bool {
		for i := 0; i < f.Services().Len(); i++ {
			s := f.Services().Get(i)
			for j := 0; j < s.Methods().Len(); j++ {
				p = append(p, "/"+string(s.FullName())+"/"+string(s.Methods().Get(j).Name()))
			}
		}
		return true
	})
	if len(p) == 0 {
		t.Fatal("keine Prozeduren in den Deskriptoren gefunden")
	}
	return p
}

// KRITISCH: Jede Prozedur hat eine Richtlinie, und ohne Sitzung kommt man nur
// an Anmelden und SitzungErneuern. Eine neue Prozedur ohne Eintrag macht diesen
// Test rot (und der Server lehnt sie ab).
func TestJedeProzedurHatEineRichtlinie(t *testing.T) {
	bekannt := map[string]bool{}
	for _, p := range alleProzeduren(t) {
		bekannt[p] = true
		if _, ok := richtlinien[p]; !ok {
			t.Errorf("%s hat keine Richtlinie", p)
		}
	}
	for p := range richtlinien {
		if !bekannt[p] {
			t.Errorf("Richtlinie für unbekannte Prozedur %s", p)
		}
	}
	offenErwartet := map[string]bool{
		"/avlreminder.v1.AnmeldeDienst/Anmelden":        true,
		"/avlreminder.v1.AnmeldeDienst/SitzungErneuern": true,
	}
	offene := offeneProzeduren()
	if len(offene) != len(offenErwartet) {
		t.Errorf("offen sind %v, erwartet nur Anmelden und SitzungErneuern", offene)
	}
	for _, p := range offene {
		if !offenErwartet[p] {
			t.Errorf("%s ist ohne Sitzung erreichbar", p)
		}
	}
}

func TestUnbekannteProzedurWirdAbgelehnt(t *testing.T) {
	err := richtlinienInterceptor{}.pruefe(context.Background(), "/avlreminder.v1.GeraeteDienst/Neu")
	erwarteCode(t, err, connect.CodeUnimplemented)
}
