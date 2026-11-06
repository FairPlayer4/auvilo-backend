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
	if !s.LaeuftAb.Equal(jetzt.Add(time.Hour)) { // 1 Stunde laut Entscheidung B1
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

	// Eigene Zahl statt Toleranz (Entscheidung ±2 min, anforderungen.md Kap. 11):
	// Sonst blieben die Grenzfälle grün, wenn jemand die Konstante ändert.
	const fenster = 2 * time.Minute
	tests := []struct {
		name      string
		geraet    speicher.Geraet
		zeitpunkt time.Time
		signatur  []byte
		wantErr   error
	}{
		{"erneuert mit gültiger Signatur", aktiv, jetzt, signiere(privat, "geraet-1", jetzt), nil},
		{"erneuert mit Uhr 2 Minuten nach", aktiv, jetzt.Add(-fenster), signiere(privat, "geraet-1", jetzt.Add(-fenster)), nil},
		{"erneuert mit Uhr 2 Minuten vor", aktiv, jetzt.Add(fenster), signiere(privat, "geraet-1", jetzt.Add(fenster)), nil},
		{"lehnt gesperrtes Gerät ab, auch mit gültiger Signatur", gesperrt, jetzt, signiere(privat, "geraet-1", jetzt), ErrGesperrt},
		// Ohne gültige Signatur erfährt niemand, ob das Gerät gesperrt ist.
		{"verrät Sperre nicht bei falscher Signatur", gesperrt, jetzt, signiere(anderer, "geraet-1", jetzt), ErrSignatur},
		{"verrät Sperre nicht bei altem Zeitpunkt", gesperrt, jetzt.Add(-fenster - time.Second), signiere(privat, "geraet-1", jetzt.Add(-fenster-time.Second)), ErrZeitpunkt},
		{"lehnt alten Zeitpunkt ab (abgefangene Erneuerung)", aktiv, jetzt.Add(-fenster - time.Second), signiere(privat, "geraet-1", jetzt.Add(-fenster-time.Second)), ErrZeitpunkt},
		{"lehnt Zeitpunkt in der Zukunft ab", aktiv, jetzt.Add(fenster + time.Second), signiere(privat, "geraet-1", jetzt.Add(fenster+time.Second)), ErrZeitpunkt},
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

func (fehlerSpeicher) Geraet(context.Context, string) (speicher.Geraet, error) {
	return speicher.Geraet{}, errors.New("Firestore nicht erreichbar")
}

// uebrigeSitzung hat genau eine gültige Sitzung (Token "uebrig") für
// "geraet-1" in "konto-1"; das Gerät dazu ist so, wie der Fall es vorgibt. So
// entsteht der Zustand, den GeraetSperren kurz zurücklässt: Gerät gesperrt,
// Sitzung noch da.
type uebrigeSitzung struct {
	geraet speicher.Geraet
	fehler error // Fehler beim Lesen des Geräts
}

func (u uebrigeSitzung) Sitzung(_ context.Context, hash string) (speicher.Sitzung, error) {
	if hash != Hash("uebrig") {
		return speicher.Sitzung{}, speicher.ErrNichtGefunden
	}
	return speicher.Sitzung{Hash: hash, KontoID: "konto-1", GeraetID: "geraet-1", LaeuftAb: jetzt.Add(time.Hour)}, nil
}

func (u uebrigeSitzung) Geraet(context.Context, string) (speicher.Geraet, error) {
	return u.geraet, u.fehler
}

var (
	geraetAktiv    = uebrigeSitzung{geraet: speicher.Geraet{ID: "geraet-1", KontoID: "konto-1"}}
	geraetGesperrt = uebrigeSitzung{geraet: speicher.Geraet{ID: "geraet-1", KontoID: "konto-1", Gesperrt: true}}
	geraetFremd    = uebrigeSitzung{geraet: speicher.Geraet{ID: "geraet-1", KontoID: "konto-2"}}
	geraetFehlt    = uebrigeSitzung{fehler: speicher.ErrNichtGefunden}
	geraetAusfall  = uebrigeSitzung{fehler: errors.New("Firestore nicht erreichbar")}
)

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
	abgelaufen, s2 := Neu(jetzt.Add(-time.Hour)) // läuft genau jetzt ab (Sitzung gilt 1 Stunde)
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
		{"akzeptiert bearer in Kleinschreibung", sp, geschuetzt, "bearer " + gueltig, 0, konto.KontoID},
		{"lehnt Bearer ohne Leerzeichen ab", sp, geschuetzt, "Bearer" + gueltig, connect.CodeUnauthenticated, ""},
		{"lehnt geschützte Prozedur ohne Sitzung ab", sp, geschuetzt, "", connect.CodeUnauthenticated, ""},
		{"lehnt anderes Schema als Bearer ab", sp, geschuetzt, "Basic " + gueltig, connect.CodeUnauthenticated, ""},
		{"lehnt leeres Token ab", sp, geschuetzt, "Bearer ", connect.CodeUnauthenticated, ""},
		{"lehnt unbekanntes Token ab", sp, geschuetzt, "Bearer erfunden", connect.CodeUnauthenticated, ""},
		{"lehnt den Hash als Token ab", sp, geschuetzt, "Bearer " + Hash(gueltig), connect.CodeUnauthenticated, ""},
		{"lehnt abgelaufene Sitzung ab", sp, geschuetzt, "Bearer " + abgelaufen, connect.CodeUnauthenticated, ""},
		{"meldet Speicherausfall als nicht verfügbar, nicht als erlaubt", fehlerSpeicher{}, geschuetzt, "Bearer " + gueltig, connect.CodeUnavailable, ""},
		// Gerät der Sitzung: Gegenprobe und die Ablehnungen (KRITISCH).
		{"übrige Sitzung eines aktiven Geräts gilt", geraetAktiv, geschuetzt, "Bearer uebrig", 0, "konto-1"},
		{"lehnt übrige Sitzung eines gesperrten Geräts ab", geraetGesperrt, geschuetzt, "Bearer uebrig", connect.CodeUnauthenticated, ""},
		{"lehnt Sitzung ab, deren Gerät zu einem anderen Konto gehört", geraetFremd, geschuetzt, "Bearer uebrig", connect.CodeUnauthenticated, ""},
		{"lehnt Sitzung ohne Gerät ab", geraetFehlt, geschuetzt, "Bearer uebrig", connect.CodeUnauthenticated, ""},
		{"meldet Ausfall beim Lesen des Geräts als nicht verfügbar", geraetAusfall, geschuetzt, "Bearer uebrig", connect.CodeUnavailable, ""},
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

// KRITISCH: Gegenstück zu „geschützte Prozedur geht mit gültiger Sitzung“ für die
// Vorprüfung auf HTTP-Ebene: Ohne gültige Sitzung erreicht die Anfrage den Dienst
// nicht (er liest also auch ihren Inhalt nicht), selbst bei einer Prozedur, die der
// Interceptor offen lässt.
func TestVorDemLesen(t *testing.T) {
	const offen = "/test.v1.TestDienst/Offen"
	ctx := context.Background()
	sp := speicher.NeuerArbeitsspeicher()
	gueltig, s := Neu(jetzt.Add(-time.Minute))
	if _, err := sp.Anmelden(ctx, "kennung", speicher.Geraet{ID: "geraet-1"}, s); err != nil {
		t.Fatal(err)
	}
	abgelaufen, s2 := Neu(jetzt.Add(-time.Hour)) // läuft genau jetzt ab
	if _, err := sp.SitzungAnlegen(ctx, "geraet-1", s2); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		lesen    SitzungLesen
		kopf     string // Authorization-Header, "" = keiner
		wantCode connect.Code
	}{
		{"lässt Anfrage mit gültiger Sitzung durch", sp, "Bearer " + gueltig, 0},
		{"lehnt Anfrage ohne Sitzung ab, auch bei offener Prozedur", sp, "", connect.CodeUnauthenticated},
		{"lehnt unbekanntes Token ab", sp, "Bearer erfunden", connect.CodeUnauthenticated},
		{"lehnt abgelaufene Sitzung ab", sp, "Bearer " + abgelaufen, connect.CodeUnauthenticated},
		{"meldet Speicherausfall als nicht verfügbar, nicht als erlaubt", fehlerSpeicher{}, "Bearer " + gueltig, connect.CodeUnavailable},
		{"lehnt übrige Sitzung eines gesperrten Geräts ab", geraetGesperrt, "Bearer uebrig", connect.CodeUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			erreicht := false
			dienst := connect.NewUnaryHandler(offen, func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
				erreicht = true
				return connect.NewResponse(&emptypb.Empty{}), nil
			})
			i := NeuerInterceptor(tt.lesen, []string{offen}, func() time.Time { return jetzt })
			srv := httptest.NewServer(i.VorDemLesen(dienst))
			t.Cleanup(srv.Close)

			client := connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, srv.URL+offen)
			req := connect.NewRequest(&emptypb.Empty{})
			if tt.kopf != "" {
				req.Header().Set("Authorization", tt.kopf)
			}
			_, err := client.CallUnary(ctx, req)
			// Ohne Fehler liefert CodeOf "unknown", daher getrennt prüfen.
			if (tt.wantCode == 0 && err != nil) || (tt.wantCode != 0 && connect.CodeOf(err) != tt.wantCode) {
				t.Fatalf("Code = %v (%v), erwartet %v", connect.CodeOf(err), err, tt.wantCode)
			}
			if erreicht != (tt.wantCode == 0) {
				t.Errorf("Dienst erreicht = %v, erwartet %v", erreicht, tt.wantCode == 0)
			}
		})
	}
}
