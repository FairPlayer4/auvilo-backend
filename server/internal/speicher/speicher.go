// Package speicher legt fest, was der Server dauerhaft speichert, und enthält
// zwei Umsetzungen: Firestore (Betrieb) und Arbeitsspeicher (schnelle Tests der
// Dienste). Beide müssen sich gleich verhalten; das prüft derselbe Vertragstest
// (vertrag_test.go) für beide.
//
// Gespeichert werden nur Metadaten (Anforderung 4.2): Kennung (HMAC der
// Google-Konto-ID), Konto- und Geräte-IDs, öffentliche Schlüssel, Zeitpunkte
// (auch der Termin einer beantragten Kontolöschung) und der Tag der letzten
// Aktivität eines Kontos, nur für die Frist aus P5.
// Bewusst NICHT: E-Mail, Name, IP-Adressen, Uhrzeit der letzten Aktivität.
package speicher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
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
	// ErrLoeschungLaeuft: Der Termin der Kontolöschung ist erreicht; abbrechen
	// geht nicht mehr, die Löschung kann schon begonnen haben.
	ErrLoeschungLaeuft = errors.New("Löschung des Kontos ist fällig")
	// ErrNichtFaellig: Ein Konto soll gesperrt oder entfernt werden, dessen
	// Löschung nicht beantragt oder noch nicht fällig ist – oder ein Gerät soll
	// begraben werden, das nicht lange genug gesperrt ist.
	ErrNichtFaellig = errors.New("Löschung ist nicht fällig")
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
	// einem Schritt (Transaktion). Von geraet zählen ID, Schluessel und Erstellt;
	// Erstellt ist der Zeitpunkt der Anmeldung. Daraus werden der Erstelltag
	// eines neuen Kontos und Geräts und der Tag der letzten Aktivität des Kontos
	// (zuletztAktiv).
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
	//
	// KRITISCH: Ein begrabenes Gerät (GeraetBegraben) bleibt gesperrt wie
	// vorher: ErrGeraetGesperrt im eigenen Konto, sonst ErrGeraetFremd. Es wird
	// nie als neues Gerät wieder angelegt.
	Anmelden(ctx context.Context, kennung string, geraet Geraet, sitzung NeueSitzung) (AnmeldeErgebnis, error)

	// Geraet liefert ein Gerät oder ErrNichtGefunden. Für ein begrabenes Gerät
	// liefert es ID, Konto und Gesperrt, ohne Schlüssel und Zeitpunkte: Wer
	// nach der Sperre fragt (Interceptor, Erneuern, Tageslimit für neue
	// Geräte), sieht weiter ein gesperrtes Gerät.
	Geraet(ctx context.Context, geraetID string) (Geraet, error)

	// KennungVon liefert die Kennung eines Kontos oder ErrNichtGefunden (kein
	// Konto, oder die Kennung ist schon gelöscht, KontoSperren). Für die
	// Freigabeliste beim Erneuern (ID7, docs/anforderungen.md).
	KennungVon(ctx context.Context, kontoID string) (string, error)

	// SitzungAnlegen stellt eine Sitzung für ein Gerät aus und liefert dessen
	// Konto. jetzt zählt als Aktivität des Kontos (zuletztAktiv).
	//
	// KRITISCH: Prüft in derselben Transaktion, dass das Gerät nicht gesperrt
	// ist. Sonst könnte ein gleichzeitiges Sperren übergangen werden. Ein
	// begrabenes Gerät meldet ErrNichtGefunden.
	SitzungAnlegen(ctx context.Context, geraetID string, sitzung NeueSitzung, jetzt time.Time) (kontoID string, err error)

	// Sitzung liefert eine Sitzung zum Hash oder ErrNichtGefunden. Ob sie
	// abgelaufen ist, prüft der Aufrufer.
	Sitzung(ctx context.Context, hash string) (Sitzung, error)

	// Geraete liefert alle Geräte eines Kontos, sortiert nach Erstellung.
	// Begrabene Geräte gehören nicht dazu; ebenso finden GeraetSperren und
	// GeraetFreischalten sie nicht (ErrNichtGefunden).
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

	// Konto löschen (docs/anforderungen.md, B1 in Kap. 3.5; konto.proto). Ein
	// Konto wird erst zur Löschung markiert (Termin), dann gesperrt, dann
	// entfernt. Bricht das ab, findet FaelligeLoeschungen das Konto wieder.

	// LoeschungBeantragen setzt den Termin, ab dem das Konto gelöscht wird, und
	// liefert den gültigen Termin. ErrNichtGefunden, wenn es das Konto nicht gibt.
	//
	// KRITISCH: Ein früherer Termin bleibt (frueherTermin): Ein zweiter Antrag
	// verschiebt die Löschung nie nach hinten, und das sofortige Löschen eines
	// freigeschalteten Geräts zieht einen offenen Antrag vor.
	LoeschungBeantragen(ctx context.Context, kontoID string, ab time.Time) (time.Time, error)

	// LoeschungAbbrechen entfernt den Termin. Ohne Termin oder Konto ist das
	// kein Fehler.
	//
	// KRITISCH: Ist der Termin erreicht, ErrLoeschungLaeuft, und der Termin
	// bleibt: Die Löschung kann schon begonnen haben (Kennung weg, Geräte
	// gesperrt); ein Abbruch ließe ein halb gelöschtes Konto ohne Termin zurück.
	LoeschungAbbrechen(ctx context.Context, kontoID string, jetzt time.Time) error

	// LoeschenAb liefert den Termin des Kontos; Nullwert = kein Antrag oder
	// kein Konto.
	LoeschenAb(ctx context.Context, kontoID string) (time.Time, error)

	// FaelligeLoeschungen liefert höchstens max Konten, deren Termin erreicht
	// ist (für den regelmäßigen Lauf).
	FaelligeLoeschungen(ctx context.Context, jetzt time.Time, max int) ([]string, error)

	// KontoSperren entzieht einem fälligen Konto den Zugang: Es löscht die
	// Kennung (ein neues Anmelden mit demselben Google-Konto legt dann ein
	// neues Konto an), sperrt alle Geräte und löscht ihre Sitzungen.
	// Wiederholbar; ein Konto, das es nicht mehr gibt, ist kein Fehler.
	//
	// KRITISCH: Nur ein Konto mit erreichtem Termin, sonst ErrNichtFaellig. So
	// kann ein Fehler im Aufrufer kein beliebiges Konto löschen.
	KontoSperren(ctx context.Context, kontoID string, jetzt time.Time) error

	// KontoEntfernen löscht die Geräte, ihre Grabsteine, übrige Sitzungen, das
	// Schlüssel-Backup und zuletzt das Konto selbst. Wiederholbar; ein Konto, das es nicht mehr
	// gibt, ist kein Fehler.
	//
	// KRITISCH: Nur ein Konto mit erreichtem Termin (sonst ErrNichtFaellig), und
	// das Konto zuletzt: Solange es da ist, findet FaelligeLoeschungen den Rest.
	KontoEntfernen(ctx context.Context, kontoID string, jetzt time.Time) error

	// Fristen aus P5 (docs/anforderungen.md Kap. 4.4, datenschutz/README.md K2)
	// für den regelmäßigen Lauf.

	// InaktiveKonten liefert höchstens max Konten, deren letzte Aktivität vor
	// grenze liegt. Konten ohne Tag der letzten Aktivität (aus der Zeit davor)
	// findet sie nicht.
	InaktiveKonten(ctx context.Context, grenze time.Time, max int) ([]string, error)

	// InaktivesKontoVormerken setzt den Löschtermin ab für ein Konto, dessen
	// letzte Aktivität vor grenze liegt, und meldet, ob sich der Termin dadurch
	// geändert hat. Ein Konto, das es nicht gibt, ist kein Fehler (false).
	//
	// KRITISCH: Die Aktivität wird in derselben Transaktion geprüft, in der
	// der Termin entsteht. Meldet sich das Konto zwischen InaktiveKonten und
	// diesem Aufruf an, bleibt es ohne Termin. Ein früherer Termin bleibt
	// (frueherTermin), wie bei LoeschungBeantragen.
	InaktivesKontoVormerken(ctx context.Context, kontoID string, grenze, ab time.Time) (bool, error)

	// LangeGesperrteGeraete liefert höchstens max Geräte, die vor grenze
	// gesperrt wurden.
	LangeGesperrteGeraete(ctx context.Context, grenze time.Time, max int) ([]string, error)

	// GeraetBegraben ersetzt ein Gerät, das vor grenze gesperrt wurde, durch
	// einen Grabstein: Der öffentliche Schlüssel und die Zeitpunkte
	// verschwinden, Geräte-ID und Konto bleiben, bis das Konto entfernt wird.
	// Ein Gerät, das es nicht (mehr) gibt, ist kein Fehler.
	//
	// KRITISCH: Nur ein lange genug gesperrtes Gerät, sonst ErrNichtFaellig –
	// geprüft in derselben Transaktion. Der Grabstein ist nötig, weil die
	// Geräte-ID aus dem Schlüssel folgt: Ohne ihn meldete sich ein gestohlenes
	// Handy mit seinem alten Schlüssel als neues Gerät an und läse mit seinem
	// lokalen Tresorschlüssel neue Sicherungen.
	GeraetBegraben(ctx context.Context, geraetID string, grenze time.Time) error
}

// aktivTag: Von der letzten Aktivität speichern die Ablagen nur den Tag (UTC),
// wie beim Erstelltag (Datensparsamkeit).
func aktivTag(jetzt time.Time) time.Time {
	return jetzt.UTC().Truncate(24 * time.Hour)
}

// mussAktivSchreiben: Geschrieben wird nur, wenn der Tag neuer ist als der
// gespeicherte (Nullwert = keiner) – höchstens einmal je Konto und Tag, und nie
// zurück.
func mussAktivSchreiben(gespeichert, tag time.Time) bool {
	return gespeichert.Before(tag)
}

// istInaktiv entscheidet InaktivesKontoVormerken: Gibt es einen Tag der
// letzten Aktivität, und liegt er vor grenze?
func istInaktiv(zuletztAktiv, grenze time.Time) bool {
	return !zuletztAktiv.IsZero() && zuletztAktiv.Before(grenze)
}

// darfBegraben entscheidet GeraetBegraben für ein vorhandenes Gerät.
func darfBegraben(g Geraet, grenze time.Time) error {
	if !g.Gesperrt || g.GesperrtAm.IsZero() || !g.GesperrtAm.Before(grenze) {
		return ErrNichtFaellig
	}
	return nil
}

// begraben ist, was Geraet für ein begrabenes Gerät liefert.
func begraben(geraetID, kontoID string) Geraet {
	return Geraet{ID: geraetID, KontoID: kontoID, Gesperrt: true}
}

// frueherTermin entscheidet LoeschungBeantragen: Der frühere Termin gilt
// (Nullwert = es gab keinen).
func frueherTermin(alt, neu time.Time) time.Time {
	if !alt.IsZero() && !alt.After(neu) {
		return alt
	}
	return neu
}

// istFaellig: Gibt es einen Termin, und ist er erreicht?
func istFaellig(ab, jetzt time.Time) bool {
	return !ab.IsZero() && !ab.After(jetzt)
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

// Entscheidungen, die Arbeitsspeicher und Firestore gleich treffen (E10): Sie
// stehen hier einmal als reine Funktionen; die Ablagen lesen nur die Daten und
// schreiben das Ergebnis.

// darfFreischalten entscheidet GeraetFreischalten für das gelesene Gerät
// (gefunden = es gibt ein Gerät mit der ID).
//
// KRITISCH: Nur im eigenen Konto (sonst ErrNichtGefunden, wie unbekannt), und
// ein gesperrtes Gerät wird nie wieder freigeschaltet.
func darfFreischalten(g Geraet, gefunden bool, kontoID string) error {
	if !gefunden || g.KontoID != kontoID {
		return ErrNichtGefunden
	}
	if g.Gesperrt {
		return ErrGeraetGesperrt
	}
	return nil
}

// darfBackupSpeichern entscheidet BackupSpeichern: aktuell ist die gespeicherte
// Version (0 = keins), geraete sind die Geräte des Kontos (nur mit
// ungeschuetztFuer nötig).
//
// KRITISCH: Erst die Version, dann der Schutz (siehe Speicher.BackupSpeichern).
// Mit ungeschuetztFuer nur, wenn das Konto kein freigeschaltetes Gerät hat und
// das Gerät zum Konto gehört und nicht gesperrt ist.
func darfBackupSpeichern(aktuell, erwartet int64, geraete []Geraet, ungeschuetztFuer string) error {
	if aktuell != erwartet {
		return ErrVersionVeraltet
	}
	if ungeschuetztFuer == "" {
		return nil
	}
	if gibtFreigeschaltetes(geraete) {
		return ErrKontoGeschuetzt
	}
	i := slices.IndexFunc(geraete, func(g Geraet) bool { return g.ID == ungeschuetztFuer })
	if i < 0 {
		return ErrNichtGefunden
	}
	if geraete[i].Gesperrt {
		return ErrGeraetGesperrt
	}
	return nil
}
