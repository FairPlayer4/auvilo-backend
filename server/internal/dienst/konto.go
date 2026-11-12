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

// loeschSpeicher ist, was der Löschweg vom Speicher braucht.
type loeschSpeicher interface {
	FaelligeLoeschungen(ctx context.Context, jetzt time.Time, max int) ([]string, error)
	KontoSperren(ctx context.Context, kontoID string, jetzt time.Time) error
	KontoEntfernen(ctx context.Context, kontoID string, jetzt time.Time) error
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
//  1. Zugang abschneiden (Kennung, Sitzungen, Geräte gesperrt). Danach lädt
//     niemand mehr etwas hoch.
//  2. Datensicherung (alle Fassungen) und Diagnosen löschen.
//  3. Geräte, Schlüssel-Backup und zuletzt das Konto mit dem Termin entfernen.
//
// Bricht ein Schritt ab, findet der nächste Lauf das Konto über den Termin
// wieder, und alle Schritte sind wiederholbar.
func (l *Loescher) loeschen(ctx context.Context, kontoID string, jetzt time.Time) error {
	if err := l.speicher.KontoSperren(ctx, kontoID, jetzt); err != nil {
		return fmt.Errorf("Zugang sperren: %w", err)
	}
	if err := l.sicherungen.Loeschen(ctx, kontoID); err != nil {
		return fmt.Errorf("Datensicherung löschen: %w", err)
	}
	if err := l.diagnosen.Loeschen(ctx, kontoID); err != nil {
		return fmt.Errorf("Diagnosen löschen: %w", err)
	}
	if err := l.speicher.KontoEntfernen(ctx, kontoID, jetzt); err != nil {
		return fmt.Errorf("Konto entfernen: %w", err)
	}
	return nil
}

// faelligeJeAbfrage: So viele fällige Konten holt Aufraeumen auf einmal.
const faelligeJeAbfrage = 50

// Aufraeumen löscht alle Konten, deren Termin erreicht ist (ein Durchgang des
// regelmäßigen Laufs). Scheitert ein Konto, macht es mit den übrigen weiter;
// die Fehler kommen gesammelt zurück, und das Konto bleibt für den nächsten
// Lauf fällig. Liefert die Zahl der gelöschten Konten.
//
// Später löscht derselbe Lauf auch nach den Fristen aus P5 (lange nicht
// angemeldete Konten, gesperrte Geräte).
func (l *Loescher) Aufraeumen(ctx context.Context, jetzt time.Time) (int, error) {
	versucht := map[string]bool{}
	var fehler []error
	geloescht := 0
	for {
		// Gescheiterte Konten bleiben fällig und kommen wieder; daher so viele
		// mehr holen, dass immer neue dabei sein können.
		ids, err := l.speicher.FaelligeLoeschungen(ctx, jetzt, len(versucht)+faelligeJeAbfrage)
		if err != nil {
			return geloescht, errors.Join(append(fehler, err)...)
		}
		neu := 0
		for _, id := range ids {
			if versucht[id] {
				continue
			}
			versucht[id] = true
			neu++
			if err := l.loeschen(ctx, id, jetzt); err != nil {
				fehler = append(fehler, fmt.Errorf("Konto %s: %w", id, err))
				continue
			}
			geloescht++
			slog.InfoContext(ctx, "Konto gelöscht (Lauf)", "konto", id)
		}
		if neu == 0 {
			return geloescht, errors.Join(fehler...)
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
	if err := d.loescher.loeschen(context.WithoutCancel(ctx), id.KontoID, jetzt); err != nil {
		return nil, speicherFehler(ctx, err)
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
