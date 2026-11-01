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
)

// Geraet ist ein angemeldetes Gerät.
type Geraet struct {
	ID         string
	KontoID    string
	Schluessel []byte // öffentlicher Geräteschlüssel, PKIX/DER
	Gesperrt   bool
	Erstellt   time.Time
	GesperrtAm time.Time // Nullwert, solange nicht gesperrt
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
	KontoID    string
	NeuesKonto bool
}

// Backup ist das verschlüsselte Schlüssel-Backup eines Kontos (B2). Der Server
// kann es nicht lesen; er kennt nur Größe, Version und Änderungszeitpunkt.
type Backup struct {
	Chiffrat  []byte
	Version   int64 // zählt ab 1; 0 heißt "es gibt keins"
	Geaendert time.Time
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

	// GeraetSperren sperrt ein Gerät des Kontos und löscht seine Sitzungen.
	//
	// KRITISCH: Nur Geräte des angegebenen Kontos. Ein fremdes Gerät meldet
	// ErrNichtGefunden – so verrät der Server nicht, ob es die ID gibt.
	GeraetSperren(ctx context.Context, kontoID, geraetID string, jetzt time.Time) error

	// BackupSpeichern legt das Backup des Kontos ab oder ersetzt es und
	// liefert die neue Version (erwartet + 1).
	//
	// KRITISCH: Geschrieben wird nur, wenn die gespeicherte Version gleich
	// erwartet ist (0 = es gibt noch keins), geprüft in derselben Transaktion.
	// Sonst ErrVersionVeraltet und nichts ändert sich. So überschreibt ein Gerät
	// nie unbemerkt ein Backup, das ein anderes Gerät inzwischen geändert hat.
	BackupSpeichern(ctx context.Context, kontoID string, chiffrat []byte, erwartet int64, jetzt time.Time) (int64, error)

	// BackupHolen liefert das Backup des Kontos oder ErrNichtGefunden.
	BackupHolen(ctx context.Context, kontoID string) (Backup, error)
}

// neueKontoID erzeugt eine zufällige Konto-ID (128 Bit, hexadezimal).
func neueKontoID() string {
	b := make([]byte, 16)
	// rand.Read liefert seit Go 1.24 nie einen Fehler (bricht notfalls ab).
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
