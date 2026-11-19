package dienst

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/emptypb"

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

// KRITISCH: Die Richtlinie gilt auch für Streaming-Prozeduren. Heute gibt es
// keine; käme eine dazu, darf sie nicht an der Prüfung vorbeilaufen. Geprüft
// wird über einen echten kleinen Server, einfach und als Stream.
func TestRichtlinieGiltAuchFuerStreams(t *testing.T) {
	tests := []struct {
		name     string
		prozedur string
		wantCode connect.Code // 0 = Prozedur läuft
	}{
		{"offene Prozedur läuft", "/avlreminder.v1.AnmeldeDienst/Anmelden", 0},
		{"Prozedur mit Sitzungsregel läuft (die Sitzung prüft der andere Interceptor)", "/avlreminder.v1.GeraeteDienst/GeraeteAuflisten", 0},
		{"lehnt unbekannte Prozedur ab", "/avlreminder.v1.GeraeteDienst/Neu", connect.CodeUnimplemented},
		{"lehnt freigabepflichtige Prozedur ohne Identität ab", "/avlreminder.v1.DatensicherungDienst/SicherungSpeichern", connect.CodeUnauthenticated},
	}
	ctx := context.Background()
	for _, stream := range []bool{false, true} {
		art := "einfach"
		if stream {
			art = "Stream"
		}
		for _, tt := range tests {
			t.Run(art+"/"+tt.name, func(t *testing.T) {
				gelaufen := false
				mit := connect.WithInterceptors(richtlinienInterceptor{})
				var h http.Handler
				if stream {
					h = connect.NewServerStreamHandler(tt.prozedur, func(context.Context, *connect.Request[emptypb.Empty], *connect.ServerStream[emptypb.Empty]) error {
						gelaufen = true
						return nil
					}, mit)
				} else {
					h = connect.NewUnaryHandler(tt.prozedur, func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
						gelaufen = true
						return connect.NewResponse(&emptypb.Empty{}), nil
					}, mit)
				}
				srv := httptest.NewServer(h)
				t.Cleanup(srv.Close)
				client := connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, srv.URL+tt.prozedur)

				var err error
				if stream {
					st, e := client.CallServerStream(ctx, connect.NewRequest(&emptypb.Empty{}))
					if err = e; e == nil {
						for st.Receive() {
						}
						err = st.Err()
						_ = st.Close()
					}
				} else {
					_, err = client.CallUnary(ctx, connect.NewRequest(&emptypb.Empty{}))
				}

				switch {
				case tt.wantCode == 0 && err != nil:
					t.Fatalf("unerwarteter Fehler: %v", err)
				case tt.wantCode != 0 && connect.CodeOf(err) != tt.wantCode:
					t.Fatalf("Code = %v (%v), erwartet %v", connect.CodeOf(err), err, tt.wantCode)
				}
				if gelaufen != (tt.wantCode == 0) {
					t.Errorf("Prozedur gelaufen = %v, erwartet %v", gelaufen, tt.wantCode == 0)
				}
			})
		}
	}
}
