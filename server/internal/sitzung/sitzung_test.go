package sitzung

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/FairPlayer4/avlreminder-backend/server/internal/geraeteschluessel"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

var jetzt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestNeu(t *testing.T) {
	token, s := Neu(jetzt)
	anderes, _ := Neu(jetzt)
	if len(token) < 43 {
		t.Errorf("Token zu kurz: %d Zeichen", len(token))
	}
	if token == anderes {
		t.Error("zwei Tokens sind gleich")
	}
	if s.Hash != Hash(token) || s.Hash == token {
		t.Errorf("Hash = %q, gespeichert werden darf nur der Hash", s.Hash)
	}
	if !s.LaeuftAb.Equal(jetzt.Add(Dauer)) {
		t.Errorf("LaeuftAb = %v", s.LaeuftAb)
	}
}

func TestPruefeErneuerung(t *testing.T) {
	privat, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	anderer, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&privat.PublicKey)
	aktiv := speicher.Geraet{ID: "geraet-1", Schluessel: der}
	gesperrt := aktiv
	gesperrt.Gesperrt = true

	signiere := func(schluessel *ecdsa.PrivateKey, geraetID string, zeitpunkt time.Time) []byte {
		summe := sha256.Sum256(geraeteschluessel.SitzungsNachricht(geraetID, zeitpunkt))
		sig, err := ecdsa.SignASN1(rand.Reader, schluessel, summe[:])
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}

	tests := []struct {
		name      string
		geraet    speicher.Geraet
		zeitpunkt time.Time
		signatur  []byte
		wantErr   error
	}{
		{"erneuert mit gültiger Signatur", aktiv, jetzt, signiere(privat, "geraet-1", jetzt), nil},
		{"erneuert mit Uhr 2 Minuten nach", aktiv, jetzt.Add(-Toleranz), signiere(privat, "geraet-1", jetzt.Add(-Toleranz)), nil},
		{"erneuert mit Uhr 2 Minuten vor", aktiv, jetzt.Add(Toleranz), signiere(privat, "geraet-1", jetzt.Add(Toleranz)), nil},
		{"lehnt gesperrtes Gerät ab, auch mit gültiger Signatur", gesperrt, jetzt, signiere(privat, "geraet-1", jetzt), ErrGesperrt},
		{"lehnt alten Zeitpunkt ab (abgefangene Erneuerung)", aktiv, jetzt.Add(-Toleranz - time.Second), signiere(privat, "geraet-1", jetzt.Add(-Toleranz-time.Second)), ErrZeitpunkt},
		{"lehnt Zeitpunkt in der Zukunft ab", aktiv, jetzt.Add(Toleranz + time.Second), signiere(privat, "geraet-1", jetzt.Add(Toleranz+time.Second)), ErrZeitpunkt},
		{"lehnt Signatur eines anderen Schlüssels ab", aktiv, jetzt, signiere(anderer, "geraet-1", jetzt), ErrSignatur},
		{"lehnt Signatur für ein anderes Gerät ab", aktiv, jetzt, signiere(privat, "geraet-2", jetzt), ErrSignatur},
		{"lehnt Signatur über einen anderen Zeitpunkt ab", aktiv, jetzt, signiere(privat, "geraet-1", jetzt.Add(time.Second)), ErrSignatur},
		{"lehnt fehlende Signatur ab", aktiv, jetzt, nil, ErrSignatur},
		{"lehnt Gerät mit kaputtem Schlüssel ab", speicher.Geraet{ID: "geraet-1", Schluessel: []byte("x")}, jetzt, signiere(privat, "geraet-1", jetzt), geraeteschluessel.ErrUngueltigerSchluessel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PruefeErneuerung(tt.geraet, tt.zeitpunkt, tt.signatur, jetzt)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Fehler = %v, erwartet %v", err, tt.wantErr)
			}
		})
	}
}

// fehlerSpeicher simuliert einen Ausfall des Speichers.
type fehlerSpeicher struct{}

func (fehlerSpeicher) Sitzung(context.Context, string) (speicher.Sitzung, error) {
	return speicher.Sitzung{}, errors.New("Firestore nicht erreichbar")
}

func TestInterceptor(t *testing.T) {
	const (
		offen      = "/test.v1.TestDienst/Offen"
		geschuetzt = "/test.v1.TestDienst/Geschuetzt"
	)
	ctx := context.Background()
	sp := speicher.NeuerArbeitsspeicher()
	gueltig, s := Neu(jetzt.Add(-time.Minute))
	if _, err := sp.Anmelden(ctx, "kennung", speicher.Geraet{ID: "geraet-1"}, s); err != nil {
		t.Fatal(err)
	}
	abgelaufen, s2 := Neu(jetzt.Add(-Dauer)) // läuft genau jetzt ab
	if _, err := sp.SitzungAnlegen(ctx, "geraet-1", s2); err != nil {
		t.Fatal(err)
	}
	konto, _ := sp.Geraet(ctx, "geraet-1")

	// starte baut einen kleinen Server mit einer offenen und einer geschützten
	// Prozedur; die Prozeduren merken sich die Identität aus dem Kontext.
	starte := func(t *testing.T, lesen SitzungLesen) (string, *Identitaet) {
		var gesehen Identitaet
		merke := func(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			gesehen, _ = IdentitaetAus(ctx)
			return connect.NewResponse(&emptypb.Empty{}), nil
		}
		i := NeuerInterceptor(lesen, []string{offen}, func() time.Time { return jetzt })
		mux := http.NewServeMux()
		mux.Handle(offen, connect.NewUnaryHandler(offen, merke, connect.WithInterceptors(i)))
		mux.Handle(geschuetzt, connect.NewUnaryHandler(geschuetzt, merke, connect.WithInterceptors(i)))
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return srv.URL, &gesehen
	}

	tests := []struct {
		name      string
		lesen     SitzungLesen
		prozedur  string
		kopf      string // Authorization-Header, "" = keiner
		wantCode  connect.Code
		wantKonto string
	}{
		{"offene Prozedur geht ohne Sitzung", sp, offen, "", 0, ""},
		{"geschützte Prozedur geht mit gültiger Sitzung", sp, geschuetzt, "Bearer " + gueltig, 0, konto.KontoID},
		{"lehnt geschützte Prozedur ohne Sitzung ab", sp, geschuetzt, "", connect.CodeUnauthenticated, ""},
		{"lehnt anderes Schema als Bearer ab", sp, geschuetzt, "Basic " + gueltig, connect.CodeUnauthenticated, ""},
		{"lehnt leeres Token ab", sp, geschuetzt, "Bearer ", connect.CodeUnauthenticated, ""},
		{"lehnt unbekanntes Token ab", sp, geschuetzt, "Bearer erfunden", connect.CodeUnauthenticated, ""},
		{"lehnt den Hash als Token ab", sp, geschuetzt, "Bearer " + Hash(gueltig), connect.CodeUnauthenticated, ""},
		{"lehnt abgelaufene Sitzung ab", sp, geschuetzt, "Bearer " + abgelaufen, connect.CodeUnauthenticated, ""},
		{"meldet Speicherausfall als nicht verfügbar, nicht als erlaubt", fehlerSpeicher{}, geschuetzt, "Bearer " + gueltig, connect.CodeUnavailable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, gesehen := starte(t, tt.lesen)
			client := connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, url+tt.prozedur)
			req := connect.NewRequest(&emptypb.Empty{})
			if tt.kopf != "" {
				req.Header().Set("Authorization", tt.kopf)
			}
			_, err := client.CallUnary(ctx, req)
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("unerwarteter Fehler: %v", err)
				}
				if gesehen.KontoID != tt.wantKonto {
					t.Errorf("Konto im Kontext = %q, erwartet %q", gesehen.KontoID, tt.wantKonto)
				}
				return
			}
			if connect.CodeOf(err) != tt.wantCode {
				t.Errorf("Code = %v (%v), erwartet %v", connect.CodeOf(err), err, tt.wantCode)
			}
			if *gesehen != (Identitaet{}) {
				t.Error("Prozedur wurde trotz Ablehnung ausgeführt")
			}
		})
	}
}
