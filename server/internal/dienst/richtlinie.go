package dienst

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/FairPlayer4/avlreminder-backend/server/gen/avlreminder/v1/avlreminderv1connect"
)

// Richtlinie je Prozedur (Review-Lauf 8, E2): Was ein Aufruf mindestens
// braucht, steht für alle Prozeduren an dieser einen Stelle. Früher rief jeder
// ändernde Dienst die Prüfung selbst auf; eine neue Prozedur ohne Aufruf wäre
// offen gewesen, ohne dass ein Test es merkt. Jetzt gilt:
//   - Eine Prozedur ohne Eintrag lehnt der Server ab (TestJedeProzedurHatEineRichtlinie
//     prüft alle Prozeduren aus den erzeugten Deskriptoren).
//   - Der Sitzungs-Interceptor lässt nur durch, was hier "offen" ist.
//   - "freigeschaltet" prüft richtlinienInterceptor, bevor der Dienst läuft.
type richtlinie int

const (
	// offen: ohne Sitzung (nur Anmelden und Erneuern).
	offen richtlinie = iota + 1
	// nurSitzung: gültige Sitzung eines ungesperrten Geräts; nur Lesen oder
	// Schritte, die sich selbst absichern (Freischalten mit Signatur).
	nurSitzung
	// freigeschaltet: zusätzlich ein freigeschaltetes Gerät (E-B2b-13).
	freigeschaltet
	// eigeneRegel: Sitzung; der Dienst prüft die Freischaltung selbst, weil
	// es Ausnahmen gibt (sich selbst sperren, erstes Backup). Jede Ausnahme
	// hat ihren Test in freischaltung_test.go.
	eigeneRegel
)

// richtlinien ordnet jeder Prozedur ihre Richtlinie zu.
//
// KRITISCH: Neue Prozeduren hier eintragen; im Zweifel "freigeschaltet".
var richtlinien = map[string]richtlinie{
	avlreminderv1connect.AnmeldeDienstAnmeldenProcedure:        offen,
	avlreminderv1connect.AnmeldeDienstSitzungErneuernProcedure: offen,

	avlreminderv1connect.GeraeteDienstGeraeteAuflistenProcedure:                 nurSitzung,
	avlreminderv1connect.GeraeteDienstMitWiederherstellungFreischaltenProcedure: nurSitzung,
	avlreminderv1connect.GeraeteDienstGeraetFreischaltenProcedure:               freigeschaltet,
	avlreminderv1connect.GeraeteDienstGeraetSperrenProcedure:                    eigeneRegel,

	avlreminderv1connect.SchluesselDienstBackupHolenProcedure:     nurSitzung,
	avlreminderv1connect.SchluesselDienstBackupLoeschenProcedure:  freigeschaltet,
	avlreminderv1connect.SchluesselDienstBackupSpeichernProcedure: eigeneRegel,

	avlreminderv1connect.DatensicherungDienstSicherungHolenProcedure:     nurSitzung,
	avlreminderv1connect.DatensicherungDienstFassungenAuflistenProcedure: nurSitzung,
	avlreminderv1connect.DatensicherungDienstSicherungSpeichernProcedure: freigeschaltet,
	avlreminderv1connect.DatensicherungDienstSicherungLoeschenProcedure:  freigeschaltet,

	// Dazu prüft nurDiagnoseErlaubt vor dem Lesen die Diagnose-Erlaubnis des Kontos.
	avlreminderv1connect.DiagnoseDienstDiagnoseSendenProcedure: freigeschaltet,
}

// offeneProzeduren liefert die Prozeduren ohne Sitzung (für den Sitzungs-Interceptor).
func offeneProzeduren() []string {
	var p []string
	for name, r := range richtlinien {
		if r == offen {
			p = append(p, name)
		}
	}
	return p
}

// richtlinienInterceptor setzt die Richtlinien durch. Er läuft nach dem
// Sitzungs-Interceptor, die Identität liegt also schon im Kontext.
type richtlinienInterceptor struct{ frei freischaltPruefer }

// pruefe lehnt ab, was die Richtlinie der Prozedur nicht erfüllt.
//
// KRITISCH: Unbekannte Prozeduren werden abgelehnt, nie durchgelassen.
func (ri richtlinienInterceptor) pruefe(ctx context.Context, prozedur string) error {
	r, ok := richtlinien[prozedur]
	if !ok {
		slog.ErrorContext(ctx, "Prozedur ohne Richtlinie abgelehnt", "prozedur", prozedur)
		return connect.NewError(connect.CodeUnimplemented, errors.New("unbekannte Prozedur"))
	}
	if r != freigeschaltet {
		return nil
	}
	id, err := identitaet(ctx)
	if err != nil {
		return err
	}
	return ri.frei.nurFreigeschaltetesGeraet(ctx, id)
}

func (ri richtlinienInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if err := ri.pruefe(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

// Der Server ruft keine anderen Dienste auf.
func (ri richtlinienInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// Streams gibt es nicht; käme einer dazu, gilt dieselbe Prüfung.
func (ri richtlinienInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if err := ri.pruefe(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}
