// Package speicher legt fest, was der Server dauerhaft speichert, und enthält
// zwei Umsetzungen: Firestore (Betrieb) und Arbeitsspeicher (schnelle Tests der
// Dienste). Beide müssen sich gleich verhalten; das prüft derselbe Vertragstest
// (vertrag_test.go) für beide.
//
// Gespeichert werden nur Metadaten (Anforderung 4.2): Kennung (HMAC der
// Google-Konto-ID), Konto- und Geräte-IDs, öffentliche Schlüssel, Zeitpunkte.
// Bewusst NICHT: E-Mail, Name, IP-Adressen, "zuletzt gesehen".
package speicher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// Fehler, die alle Umsetzungen gleich melden.
var (
	ErrNichtGefunden  = errors.New("nicht gefunden")
	ErrGeraetGesperrt = errors.New("Gerät ist gesperrt")
	// ErrGeraetFremd: Der Geräteschlüssel gehört schon zu einem anderen Konto.
	ErrGeraetFremd = errors.New("Gerät gehört zu einem anderen Konto")
	// ErrVersionVeraltet: Das Backup wurde inzwischen von einem anderen Gerät
	// geändert (die erwartete Version stimmt nicht).
	ErrVersionVeraltet = errors.New("Backup-Version passt nicht")
	// ErrZuVieleGeraete: Das Konto hat schon MaxOffeneGeraete neue Geräte, die
	// weder freigeschaltet noch gesperrt sind.
	ErrZuVieleGeraete = errors.New("zu viele nicht freigeschaltete Geräte")
	// ErrKontoGeschuetzt: Ein nicht freigeschaltetes Gerät will das erste
	// Backup anlegen, aber das Konto hat ein freigeschaltetes, ungesperrtes Gerät.
	ErrKontoGeschuetzt = errors.New("Konto hat ein freigeschaltetes Gerät")
)

// MaxOffeneGeraete ist die Zahl neuer Geräte, die ein Konto höchstens hat,
// ohne dass eins davon freigeschaltet oder gesperrt ist (Entscheidung nach
// Review-Lauf 8). Wer nur den Google-Login hat, kann sonst beliebig viele
// Geräte anlegen; die Liste der App liefe voll, und jedes Anmelden liest alle.
const MaxOffeneGeraete = 10

// Geraet ist ein angemeldetes Gerät.
type Geraet struct {
	ID         string
	KontoID    string
	Schluessel []byte // öffentlicher Geräteschlüssel, PKIX/DER
	Gesperrt   bool
	// Freigeschaltet: Das Gerät darf sperren, freischalten, speichern und
	// löschen (E-B2b-13, docs/b2b-entwurf.md). Legt Anmelden fest, danach
	// ändert es nur GeraetFreischalten.
	Freigeschaltet bool
	Erstellt       time.Time
	GesperrtAm     time.Time // Nullwert, solange nicht gesperrt
}

// Sitzung ist eine ausgestellte Sitzung. Das Token selbst wird nie gespeichert,
// nur sein Hash: Wer die Datenbank liest, kann damit keine Anfrage stellen.
type Sitzung struct {
	Hash     string
	KontoID  string
	GeraetID string
	LaeuftAb time.Time
}

// NeueSitzung beschreibt eine Sitzung, die angelegt werden soll; das Konto
// ergibt sich aus dem Gerät.
type NeueSitzung struct {
	Hash     string
	LaeuftAb time.Time
}

// AnmeldeErgebnis ist das Ergebnis von Anmelden.
type AnmeldeErgebnis struct {
	KontoID        string
	NeuesKonto     bool
	Freigeschaltet bool // gilt für das angemeldete Gerät
}

// Backup ist das verschlüsselte Schlüssel-Backup eines Kontos (B2). Der Server
// kann es nicht lesen; er kennt nur Größe, Version und Änderungszeitpunkt.
type Backup struct {
	Chiffrat []byte
	// Freischaltschluessel ist der öffentliche Ed25519-Schlüssel, mit dem sich
	// ein neues Gerät freischaltet (Paket freischaltung). Leer bei Backups aus
	// der Zeit davor.
	Freischaltschluessel []byte
	Version              int64 // zählt ab 1; 0 heißt "es gibt keins"
	Geaendert            time.Time
}

// Speicher ist alles, was die Dienste vom Speicher brauchen.
type Speicher interface {
	// Anmelden sucht das Konto zur Kennung (legt es beim ersten Mal an), legt
	// das Gerät an, falls es neu ist, und stellt die Sitzung aus – alles in
	// einem Schritt (Transaktion). Von geraet zählen ID, Schluessel und Erstellt.
	//
	// KRITISCH: Ein gesperrtes Gerät bleibt gesperrt (ErrGeraetGesperrt), auch
	// wenn der Google-Login stimmt. Ein Geräteschlüssel, der schon zu einem
	// anderen Konto gehört, wird abgelehnt (ErrGeraetFremd).
	//
	// KRITISCH (E-B2b-13): Ein neues Gerät ist genau dann freigeschaltet, wenn
	// das Konto kein freigeschaltetes, ungesperrtes Gerät hat und kein Backup
	// mit Freischaltschlüssel – wenn es also nichts zu schützen gibt. Wer nur
	// den Google-Login hat, bekommt sonst ein Gerät, das nichts ändern darf.
	// Ein vorhandenes Gerät behält sein Merkmal.
	//
	// KRITISCH: Ein neues, nicht freigeschaltetes Gerät wird abgelehnt
	// (ErrZuVieleGeraete), wenn das Konto schon MaxOffeneGeraete Geräte hat, die
	// weder freigeschaltet noch gesperrt sind. Vorhandene Geräte melden sich
	// immer an.
	Anmelden(ctx context.Context, kennung string, geraet Geraet, sitzung NeueSitzung) (AnmeldeErgebnis, error)

	// Geraet liefert ein Gerät oder ErrNichtGefunden.
	Geraet(ctx context.Context, geraetID string) (Geraet, error)

	// SitzungAnlegen stellt eine Sitzung für ein Gerät aus und liefert dessen
	// Konto.
	//
	// KRITISCH: Prüft in derselben Transaktion, dass das Gerät nicht gesperrt
	// ist. Sonst könnte ein gleichzeitiges Sperren übergangen werden.
	SitzungAnlegen(ctx context.Context, geraetID string, sitzung NeueSitzung) (kontoID string, err error)

	// Sitzung liefert eine Sitzung zum Hash oder ErrNichtGefunden. Ob sie
	// abgelaufen ist, prüft der Aufrufer.
	Sitzung(ctx context.Context, hash string) (Sitzung, error)

	// Geraete liefert alle Geräte eines Kontos, sortiert nach Erstellung.
	Geraete(ctx context.Context, kontoID string) ([]Geraet, error)

	// GeraetSperren sperrt ein Gerät des Kontos und löscht danach seine
	// Sitzungen. Wiederholbar: Ein zweiter Aufruf löscht, was beim ersten
	// übrig blieb.
	//
	// KRITISCH: Nur Geräte des angegebenen Kontos. Ein fremdes Gerät meldet
	// ErrNichtGefunden – so verrät der Server nicht, ob es die ID gibt.
	//
	// KRITISCH: Das Sperren selbst ist ein kleiner Schritt (eine Transaktion
	// über das Gerät); die Sitzungen werden danach in Stapeln gelöscht. Bei
	// sehr vielen Sitzungen scheiterte eine einzige Transaktion, und das Gerät
	// bliebe ungesperrt. Bis die Sitzungen weg sind, hält der Interceptor das
	// Gerät draußen, weil er bei jeder Anfrage die Sperre liest. Scheitert das
	// Löschen, ist das Gerät trotzdem gesperrt; der Fehler wird gemeldet.
	GeraetSperren(ctx context.Context, kontoID, geraetID string, jetzt time.Time) error

	// GeraetFreischalten schaltet ein Gerät des Kontos frei. Wiederholbar.
	//
	// KRITISCH: Nur Geräte des angegebenen Kontos (sonst ErrNichtGefunden, wie
	// beim Sperren). Ein gesperrtes Gerät bleibt draußen (ErrGeraetGesperrt).
	GeraetFreischalten(ctx context.Context, kontoID, geraetID string) error

	// BackupSpeichern legt das Backup des Kontos ab oder ersetzt es und
	// liefert die neue Version (erwartet + 1).
	//
	// KRITISCH: Geschrieben wird nur, wenn die gespeicherte Version gleich
	// erwartet ist (0 = es gibt noch keins), geprüft in derselben Transaktion.
	// Sonst ErrVersionVeraltet und nichts ändert sich. So überschreibt ein Gerät
	// nie unbemerkt ein Backup, das ein anderes Gerät inzwischen geändert hat.
	// Der Freischaltschlüssel wird mit dem Backup gespeichert und ersetzt.
	//
	// KRITISCH: Mit ungeschuetztFuer (ID des aufrufenden, nicht
	// freigeschalteten Geräts; leer = das Gerät ist freigeschaltet) wird nur
	// geschrieben, wenn das Konto kein freigeschaltetes, ungesperrtes Gerät hat
	// – geprüft in derselben Transaktion. Sonst ErrKontoGeschuetzt. Wer nur den
	// Google-Login hat, kann so kein erstes Backup mit eigenem
	// Freischaltschlüssel anlegen und sich damit selbst freischalten. Wird das
	// Backup angenommen, ist das Gerät danach freigeschaltet, in derselben
	// Transaktion (Entscheidung nach Review-Lauf 9): Es hat den
	// Freischaltschlüssel gerade selbst gesetzt. Ist es kein ungesperrtes Gerät
	// des Kontos, ErrNichtGefunden bzw. ErrGeraetGesperrt.
	//
	// KRITISCH: Die Version wird vor dem Schutz geprüft. Ein Gerät mit
	// veralteter Version bekommt ErrVersionVeraltet, holt das Backup und
	// schaltet sich mit dem Wiederherstellungsschlüssel frei, statt als
	// „nicht freigeschaltet“ festzuhängen.
	BackupSpeichern(ctx context.Context, kontoID string, chiffrat, freischaltschluessel []byte, erwartet int64, ungeschuetztFuer string, jetzt time.Time) (int64, error)

	// BackupHolen liefert das Backup des Kontos oder ErrNichtGefunden.
	BackupHolen(ctx context.Context, kontoID string) (Backup, error)

	// BackupLoeschen entfernt das Backup des Kontos samt Freischaltschlüssel. Gibt es keins, ist das
	// kein Fehler. Danach beginnt ein neues Backup wieder mit Version 1.
	BackupLoeschen(ctx context.Context, kontoID string) error
}

// Die Regeln zur Freischaltung als reine Funktionen: Beide Umsetzungen lesen
// die Geräte des Kontos (und das Backup) in ihrer Transaktion und entscheiden
// dann hier – so können sie nicht auseinanderlaufen.

// gibtFreigeschaltetes: Hat das Konto ein freigeschaltetes, ungesperrtes Gerät?
func gibtFreigeschaltetes(geraete []Geraet) bool {
	for _, g := range geraete {
		if g.Freigeschaltet && !g.Gesperrt {
			return true
		}
	}
	return false
}

// neuesGeraetFreigeschaltet wendet die Regeln aus Speicher.Anmelden auf ein
// neues Gerät an. geraete sind die übrigen Geräte des Kontos.
//
// KRITISCH: Freigeschaltet nur, wenn es nichts zu schützen gibt (kein
// freigeschaltetes, ungesperrtes Gerät, kein Backup mit Freischaltschlüssel).
// Sonst höchstens MaxOffeneGeraete offene Geräte.
func neuesGeraetFreigeschaltet(geraete []Geraet, backupMitSchluessel bool) (bool, error) {
	if !gibtFreigeschaltetes(geraete) && !backupMitSchluessel {
		return true, nil
	}
	offen := 0
	for _, g := range geraete {
		if !g.Freigeschaltet && !g.Gesperrt {
			offen++
		}
	}
	if offen >= MaxOffeneGeraete {
		return false, ErrZuVieleGeraete
	}
	return false, nil
}

// neueKontoID erzeugt eine zufällige Konto-ID (128 Bit, hexadezimal).
func neueKontoID() string {
	b := make([]byte, 16)
	// rand.Read liefert seit Go 1.24 nie einen Fehler (bricht notfalls ab).
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
