package dienst

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1"
	"github.com/FairPlayer4/avlreminder-backend/server/internal/speicher"
)

// Konto löschen (B1 in docs/anforderungen.md Kap. 3.5, konto.proto).
//
// Ein freigeschaltetes Gerät löscht sofort. Ein nicht freigeschaltetes Gerät
// (nur der Google-Login) stellt einen Antrag; der Server löscht nach
// WartezeitLoeschung, und bis dahin kann jedes freigeschaltete Gerät
// abbrechen. Beide Wege enden im selben Löschweg (Loescher.loeschen), den
// auch der regelmäßige Lauf (Aufraeumen) nimmt.

// WartezeitLoeschung ist die Zeit, die der Server nach einem Antrag eines
// nicht freigeschalteten Geräts mit dem Löschen wartet. Die Zeit gibt dem Besitzer die Gelegenheit, einen
// Antrag aus einem übernommenen Google-Konto zu bemerken und abzubrechen.
const WartezeitLoeschung = 7 * 24 * time.Hour

// Fristen aus P5 (docs/anforderungen.md Kap. 4.4, datenschutz/README.md K2),
// die der regelmäßige Lauf (Aufraeumen) umsetzt.
const (
	// FristInaktivMonate: Ein Konto, das sich so lange weder angemeldet noch
	// eine Sitzung erneuert hat, wird zur Löschung vorgemerkt; gelöscht wird es
	// WartezeitLoeschung später auf dem üblichen Weg.
	FristInaktivMonate = 24
	// FristGesperrt: So lange nach dem Sperren wird ein Gerät begraben
	// (Speicher.GeraetBegraben): Schlüssel und Zeitpunkte verschwinden, die
	// Sperre bleibt.
	FristGesperrt = 90 * 24 * time.Hour
)

// inaktivGrenze: Konten, deren letzte Aktivität vor diesem Tag liegt, sind
// inaktiv. Auf den Tag abgerundet, weil das Konto nur den Tag speichert: So
// liegen immer mindestens volle FristInaktivMonate dazwischen.
func inaktivGrenze(jetzt time.Time) time.Time {
	return jetzt.UTC().AddDate(0, -FristInaktivMonate, 0).Truncate(24 * time.Hour)
}

// loeschSpeicher ist, was der Löschweg vom Speicher braucht.
type loeschSpeicher interface {
	FaelligeLoeschungen(ctx context.Context, jetzt time.Time, max int) ([]string, error)
	KontoSperren(ctx context.Context, kontoID string, jetzt time.Time) error
	KontoEntfernen(ctx context.Context, kontoID string, jetzt time.Time) error
	InaktiveKonten(ctx context.Context, grenze time.Time, max int) ([]string, error)
	InaktivesKontoVormerken(ctx context.Context, kontoID string, grenze, ab time.Time) (bool, error)
	LangeGesperrteGeraete(ctx context.Context, grenze time.Time, max int) ([]string, error)
	GeraetBegraben(ctx context.Context, geraetID string, grenze time.Time) error
}

// kontoSpeicher ist, was der Konto-Dienst vom Speicher braucht.
type kontoSpeicher interface {
	LoeschungBeantragen(ctx context.Context, kontoID string, ab time.Time) (time.Time, error)
	LoeschungAbbrechen(ctx context.Context, kontoID string, jetzt time.Time) error
}

// loeschenAbLesen liest den Termin einer beantragten Löschung (für die
// Antworten von Anmelden, SitzungErneuern und GeraeteAuflisten).
type loeschenAbLesen interface {
	LoeschenAb(ctx context.Context, kontoID string) (time.Time, error)
}

// kontoAblage ist der Teil einer Ablage (Sicherungen, Diagnosen), den der
// Löschweg braucht. datensicherung.Ablage und diagnose.Ablage erfüllen ihn.
type kontoAblage interface {
	Loeschen(ctx context.Context, kontoID string) error
}

// Loescher löscht Konten mit fälligem Termin: im Konto-Dienst (sofort) und im
// regelmäßigen Lauf (Aufraeumen).
type Loescher struct {
	speicher    loeschSpeicher
	sicherungen kontoAblage
	diagnosen   kontoAblage
}

// NeuerLoescher für den regelmäßigen Lauf (cmd/server, Unterbefehl aufraeumen).
func NeuerLoescher(s speicher.Speicher, sicherungen, diagnosen kontoAblage) *Loescher {
	return &Loescher{speicher: s, sicherungen: sicherungen, diagnosen: diagnosen}
}

// loeschen löscht ein Konto, dessen Termin erreicht ist.
//
// KRITISCH: Die Reihenfolge sorgt dafür, dass ein Abbruch nie ein Konto ohne
// Termin, aber mit Daten hinterlässt:
//  1. Zugang abschneiden (Kennung, Sitzungen, Geräte gesperrt). Danach nimmt
//     der Server nichts mehr an; nur eine Anfrage, die schon vorher
//     angenommen war, kann noch ankommen (der Lauf räumt sie als verwaist auf).
//  2. Datensicherung (alle Fassungen) und Diagnosen löschen.
//  3. Geräte, Schlüssel-Backup und zuletzt das Konto mit dem Termin entfernen.
//
// Bricht ein Schritt ab, findet der nächste Lauf das Konto über den Termin
// wieder, und alle Schritte sind wiederholbar. gesperrt sagt, ob Schritt 1
// gelungen ist: Dann ist das Konto für die App gelöscht, auch wenn err einen
// späteren Schritt meldet.
func (l *Loescher) loeschen(ctx context.Context, kontoID string, jetzt time.Time) (gesperrt bool, err error) {
	if err := l.speicher.KontoSperren(ctx, kontoID, jetzt); err != nil {
		return false, fmt.Errorf("Zugang sperren: %w", err)
	}
	if err := l.sicherungen.Loeschen(ctx, kontoID); err != nil {
		return true, fmt.Errorf("Datensicherung löschen: %w", err)
	}
	if err := l.diagnosen.Loeschen(ctx, kontoID); err != nil {
		return true, fmt.Errorf("Diagnosen löschen: %w", err)
	}
	if err := l.speicher.KontoEntfernen(ctx, kontoID, jetzt); err != nil {
		return true, fmt.Errorf("Konto entfernen: %w", err)
	}
	return true, nil
}

// jeAbfrage: So viele Treffer holt Aufraeumen je Abfrage auf einmal.
const jeAbfrage = 50

// AufraeumErgebnis zählt, was ein Durchgang von Aufraeumen getan hat.
type AufraeumErgebnis struct {
	Begraben   int // gesperrte Geräte, durch Grabsteine ersetzt
	Vorgemerkt int // inaktive Konten, die einen Löschtermin bekommen haben
	Geloescht  int // Konten mit erreichtem Termin, gelöscht
}

// Aufraeumen ist ein Durchgang des regelmäßigen Laufs, in drei Schritten:
//  1. Geräte, die länger als FristGesperrt gesperrt sind, begraben.
//  2. Konten, die länger als FristInaktivMonate inaktiv sind, zur Löschung
//     vormerken (Termin in WartezeitLoeschung). Die Geräte sehen den Termin wie
//     bei einem Antrag, und ein freigeschaltetes Gerät kann abbrechen.
//  3. Alle Konten löschen, deren Termin erreicht ist.
//
// Scheitert ein Gerät oder Konto, macht jeder Schritt mit den übrigen weiter,
// und auch die folgenden Schritte laufen; die Fehler kommen gesammelt zurück.
// Was scheiterte, findet der nächste Lauf wieder.
func (l *Loescher) Aufraeumen(ctx context.Context, jetzt time.Time) (AufraeumErgebnis, error) {
	var e AufraeumErgebnis
	var fehler []error

	gesperrtVor := jetzt.Add(-FristGesperrt)
	fehler = append(fehler, jeEinmal(func(max int) ([]string, error) {
		return l.speicher.LangeGesperrteGeraete(ctx, gesperrtVor, max)
	}, func(id string) error {
		if err := l.speicher.GeraetBegraben(ctx, id, gesperrtVor); err != nil {
			return fmt.Errorf("Gerät %s begraben: %w", id, err)
		}
		e.Begraben++
		slog.InfoContext(ctx, "Gesperrtes Gerät begraben (Lauf)", "geraet", id)
		return nil
	})...)

	grenze := inaktivGrenze(jetzt)
	ab := jetzt.Add(WartezeitLoeschung)
	fehler = append(fehler, jeEinmal(func(max int) ([]string, error) {
		return l.speicher.InaktiveKonten(ctx, grenze, max)
	}, func(id string) error {
		geaendert, err := l.speicher.InaktivesKontoVormerken(ctx, id, grenze, ab)
		if err != nil {
			return fmt.Errorf("Konto %s vormerken: %w", id, err)
		}
		if geaendert {
			e.Vorgemerkt++
			slog.InfoContext(ctx, "Inaktives Konto zur Löschung vorgemerkt (Lauf)", "konto", id, "ab", ab)
		}
		return nil
	})...)

	fehler = append(fehler, jeEinmal(func(max int) ([]string, error) {
		return l.speicher.FaelligeLoeschungen(ctx, jetzt, max)
	}, func(id string) error {
		if _, err := l.loeschen(ctx, id, jetzt); err != nil {
			return fmt.Errorf("Konto %s: %w", id, err)
		}
		e.Geloescht++
		slog.InfoContext(ctx, "Konto gelöscht (Lauf)", "konto", id)
		return nil
	})...)

	return e, errors.Join(fehler...)
}

// jeEinmal ruft tu für jede ID auf, die holen liefert, für jede höchstens
// einmal, und sammelt die Fehler. Was scheitert (oder, wie ein schon
// vorgemerktes Konto, ein Treffer bleibt), liefert holen wieder; daher holt es
// jedes Mal so viele mehr, dass immer neue dabei sein können. Es endet, wenn
// holen nichts Neues mehr liefert, oder beim ersten Fehler von holen.
func jeEinmal(holen func(max int) ([]string, error), tu func(id string) error) []error {
	versucht := map[string]bool{}
	var fehler []error
	for {
		ids, err := holen(len(versucht) + jeAbfrage)
		if err != nil {
			return append(fehler, err)
		}
		neu := 0
		for _, id := range ids {
			if versucht[id] {
				continue
			}
			versucht[id] = true
			neu++
			if err := tu(id); err != nil {
				fehler = append(fehler, err)
			}
		}
		if neu == 0 {
			return fehler
		}
	}
}

type kontoDienst struct {
	speicher kontoSpeicher
	loescher *Loescher
	frei     freischaltPruefer
	jetzt    func() time.Time
}

// KontoLoeschen löscht das Konto sofort (freigeschaltetes Gerät) oder
// beantragt die Löschung (nicht freigeschaltetes Gerät).
//
// KRITISCH: Ohne Freischaltung nie sofort: Wer nur den Google-Login hat,
// bekommt einen Termin WartezeitLoeschung später, den die echten Geräte sehen
// und abbrechen können. Nur ein Termin, der schon erreicht ist (früherer
// Antrag), führt auch hier zum sofortigen Löschen.
func (d *kontoDienst) KontoLoeschen(ctx context.Context, _ *connect.Request[v1.KontoLoeschenRequest]) (*connect.Response[v1.KontoLoeschenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	frei, err := d.frei.istFreigeschaltet(ctx, id)
	if err != nil {
		return nil, err
	}
	jetzt := d.jetzt()
	ab := jetzt
	if !frei {
		ab = jetzt.Add(WartezeitLoeschung)
	}
	// Erst den Termin speichern, dann löschen: Bricht das Löschen ab, macht
	// der regelmäßige Lauf weiter.
	termin, err := d.speicher.LoeschungBeantragen(ctx, id.KontoID, ab)
	if errors.Is(err, speicher.ErrNichtGefunden) {
		// Das Konto ist schon weg (gleichzeitiges Löschen); die Sitzung gilt nicht mehr.
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("Sitzung fehlt"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	if termin.After(jetzt) {
		slog.WarnContext(ctx, "Löschung des Kontos beantragt", "konto", id.KontoID, "geraet", id.GeraetID, "ab", termin)
		return connect.NewResponse(&v1.KontoLoeschenResponse{LoeschenAb: timestamppb.New(termin)}), nil
	}
	// Ohne Abbruch, wenn die App die Verbindung schließt: Ein halb gelöschtes
	// Konto wäre zwar sicher (der Lauf macht weiter), aber unnötig.
	gesperrt, err := d.loescher.loeschen(context.WithoutCancel(ctx), id.KontoID, jetzt)
	if !gesperrt {
		return nil, speicherFehler(ctx, err)
	}
	// KRITISCH: Ist der Zugang weg, ist das Konto für die App gelöscht, auch
	// wenn ein späterer Schritt scheitert. Sonst stünde die App mit einem
	// Fehler da und könnte nichts mehr tun: Jede weitere Anfrage, auch ein
	// zweites KontoLoeschen, endet mit unauthenticated. Den Rest löscht der
	// regelmäßige Lauf über den Termin.
	if err != nil {
		slog.ErrorContext(ctx, "Konto gesperrt, Rest löscht der Lauf", "konto", id.KontoID, "fehler", err)
	}
	slog.InfoContext(ctx, "Konto gelöscht", "konto", id.KontoID, "geraet", id.GeraetID)
	return connect.NewResponse(&v1.KontoLoeschenResponse{Geloescht: true}), nil
}

// KontoLoeschenAbbrechen bricht einen offenen Antrag ab. Die Freischaltung
// prüft die Richtlinie (richtlinie.go).
func (d *kontoDienst) KontoLoeschenAbbrechen(ctx context.Context, _ *connect.Request[v1.KontoLoeschenAbbrechenRequest]) (*connect.Response[v1.KontoLoeschenAbbrechenResponse], error) {
	id, err := identitaet(ctx)
	if err != nil {
		return nil, err
	}
	err = d.speicher.LoeschungAbbrechen(ctx, id.KontoID, d.jetzt())
	if errors.Is(err, speicher.ErrLoeschungLaeuft) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Löschung ist schon fällig"))
	}
	if err != nil {
		return nil, speicherFehler(ctx, err)
	}
	slog.InfoContext(ctx, "Löschung des Kontos abgebrochen", "konto", id.KontoID, "geraet", id.GeraetID)
	return connect.NewResponse(&v1.KontoLoeschenAbbrechenResponse{}), nil
}

// loeschenAb liefert den Termin einer beantragten Löschung für die Antwort
// (nil = keiner).
//
// Ein Lesefehler lässt den Aufruf nicht scheitern: Das Feld ist ein Hinweis,
// und der nächste Aufruf zeigt ihn wieder.
func loeschenAb(ctx context.Context, s loeschenAbLesen, kontoID string) *timestamppb.Timestamp {
	ab, err := s.LoeschenAb(ctx, kontoID)
	if err != nil {
		slog.ErrorContext(ctx, "Termin der Löschung nicht lesbar", "konto", kontoID, "fehler", err.Error())
		return nil
	}
	if ab.IsZero() {
		return nil
	}
	return timestamppb.New(ab)
}
