package com.avlreminder.protokoll

/** Ein Backup, wie der Server es liefert. */
class GeholtesBackup(val chiffrat: ByteArray, val version: Long)

/** Ein anderes Gerät hat das Backup inzwischen geändert (Server: failed_precondition). */
class VersionVeraltet : Exception("Backup wurde inzwischen von einem anderen Gerät geändert")

/** Die Einrichtung ist gescheitert oder wurde abgebrochen. */
class EinrichtungAbgebrochen(grund: String) : Exception(grund)

/**
 * Das Gerät ist noch nicht freigeschaltet und darf das nicht (Server:
 * permission_denied, geraete.proto). Ein freigeschaltetes Gerät des Kontos muss
 * es freischalten, oder es schaltet sich mit dem Wiederherstellungsschlüssel frei.
 */
class GeraetNichtFreigeschaltet : Exception("Dieses Gerät ist noch nicht freigeschaltet")

/** Das Backup auf dem Server (im Test ersetzt; im Test-Client per Connect). */
interface BackupAblage {
    /** Das Backup des Kontos, oder null, wenn es keins gibt. */
    suspend fun holen(): GeholtesBackup?

    /**
     * Speichert das Backup samt Freischaltschlüssel
     * ([Wiederherstellungsschluessel.freischaltschluessel]); wirft
     * [VersionVeraltet], wenn [erwartet] nicht mehr stimmt.
     */
    suspend fun speichern(chiffrat: ByteArray, freischaltschluessel: ByteArray, erwartet: Long): Long
}

/** Die Freischaltung dieses Geräts beim Server (E-B2b-13). */
fun interface GeraeteFreischaltung {
    /**
     * Schaltet dieses Gerät mit einer Signatur aus
     * [Wiederherstellungsschluessel.signiereFreischaltung] frei; wirft
     * [GeraetNichtFreigeschaltet], wenn der Server sie ablehnt.
     */
    suspend fun mitWiederherstellung(signatur: ByteArray)
}

/** Ein Backup, das dieses Gerät erzeugt, aber noch nicht sicher hochgeladen hat. */
class OffenesBackup(val chiffrat: ByteArray, val freischaltschluessel: ByteArray)

/**
 * Was das Gerät selbst für das Konto ablegt.
 *
 * @property offenesBackup null, wenn nichts offen ist.
 */
class LokalerStand(val bund: Schluesselbund, val offenesBackup: OffenesBackup?)

/** Die Ablage auf dem Gerät (im Test-Client eine Datei, in der App später der Keystore). */
interface LokaleAblage {
    /** Der Stand für das Konto, oder null, wenn das Gerät noch keine Schlüssel hat. */
    fun laden(): LokalerStand?

    fun speichern(stand: LokalerStand)
}

/** Ergebnis der Einrichtung. */
sealed interface Eingerichtet {
    val bund: Schluesselbund

    /** Dieses Gerät hat die Schlüssel erzeugt und das Backup hochgeladen. */
    class Neu(override val bund: Schluesselbund) : Eingerichtet

    /** Die Schlüssel kommen aus dem Backup eines anderen Geräts. */
    class AusBackup(override val bund: Schluesselbund) : Eingerichtet

    /**
     * Das Gerät hatte die Schlüssel schon; am Schlüsselbund nichts geändert.
     * War es noch nicht freigeschaltet, hat es sich eventuell dabei freigeschaltet.
     */
    class Vorhanden(override val bund: Schluesselbund) : Eingerichtet
}

/**
 * Richtet die Schlüssel auf einem Gerät ein (E-B2-5): Gibt es auf dem Server ein
 * Backup, entschlüsselt es dieses mit dem Wiederherstellungsschlüssel, den der
 * Mensch eintippt. Gibt es keins, erzeugt es neue Schlüssel und lädt das Backup
 * hoch. Liegt in `protokoll`, weil die App denselben Ablauf braucht.
 *
 * Ein Gerät, das die Schlüssel aus dem Backup holt, schaltet sich dabei mit
 * demselben Wiederherstellungsschlüssel beim Server frei (E-B2b-13).
 *
 * @param geraetId die Geräte-ID dieses Geräts (aus der Anmeldung).
 * @param freigeschaltet ob der Server dieses Gerät schon als freigeschaltet
 *   meldet (Anmelden); dann ist keine Freischaltung nötig.
 * @param frageSchluessel fragt den Menschen nach dem Wiederherstellungsschlüssel;
 *   null heißt Abbruch.
 * @param zeigeWiederherstellung zeigt einen neuen Wiederherstellungsschlüssel;
 *   true heißt, der Mensch hat ihn aufgeschrieben.
 * @param meldung Hinweise an den Menschen.
 */
class Einrichtung(
    private val kontoId: String,
    private val geraetId: String,
    private val freigeschaltet: Boolean,
    private val server: BackupAblage,
    private val freischaltung: GeraeteFreischaltung,
    private val lokal: LokaleAblage,
    private val frageSchluessel: () -> String?,
    private val zeigeWiederherstellung: (Wiederherstellungsschluessel) -> Boolean,
    private val meldung: (String) -> Unit,
) {
    /**
     * KRITISCH: Kein Backup auf dem Server, dessen Wiederherstellungsschlüssel
     * niemand hat. Deshalb in dieser Reihenfolge: erzeugen, Schlüssel anzeigen und
     * bestätigen lassen, lokal ablegen (mit offenem Backup), erst dann hochladen.
     * Geht beim Hochladen etwas schief, setzt der nächste Lauf dort fort.
     */
    suspend fun ausfuehren(): Eingerichtet {
        lokal.laden()?.let { stand ->
            stand.offenesBackup?.let { offen ->
                meldung("Das Backup dieses Geräts ist noch nicht sicher hochgeladen - versuche es erneut.")
                return hochladen(stand.bund, offen)
            }
            // Schlüssel da, aber nicht freigeschaltet (z. B. neu angemeldet mit
            // neuem Geräteschlüssel): ohne diesen Schritt bliebe das Gerät
            // ohne zweites Gerät für immer nur lesend.
            if (!freigeschaltet) server.holen()?.let { freischalten(it) }
            return Eingerichtet.Vorhanden(stand.bund)
        }
        server.holen()?.let { return wiederherstellen(it) }

        val bund = Schluesselbund.erzeugen()
        val wiederherstellung = Wiederherstellungsschluessel.erzeugen()
        val offen = OffenesBackup(bund.backupVerschluesseln(wiederherstellung, kontoId), wiederherstellung.freischaltschluessel())
        if (!zeigeWiederherstellung(wiederherstellung)) {
            throw EinrichtungAbgebrochen("Wiederherstellungsschlüssel nicht bestätigt - es wurde nichts gespeichert")
        }
        lokal.speichern(LokalerStand(bund, offenesBackup = offen))
        return hochladen(bund, offen)
    }

    /**
     * KRITISCH: Ein vorhandenes Backup wird nie überschrieben (erwartete Version 0).
     * Liegt schon genau unser Backup dort, war ein früheres Hochladen erfolgreich und
     * nur die Antwort ging verloren. Liegt ein anderes dort, war ein anderes Gerät
     * schneller; dann übernimmt dieses Gerät dessen Schlüssel. Sonst gäbe es zwei
     * verschiedene Personenschlüssel für dasselbe Konto.
     */
    private suspend fun hochladen(bund: Schluesselbund, offen: OffenesBackup): Eingerichtet {
        try {
            server.speichern(offen.chiffrat, offen.freischaltschluessel, erwartet = 0)
        } catch (_: VersionVeraltet) {
            val vorhanden = server.holen() ?: throw EinrichtungAbgebrochen("Backup ist verschwunden - später erneut versuchen")
            if (!vorhanden.chiffrat.contentEquals(offen.chiffrat)) {
                meldung("Ein anderes Gerät hat die Schlüssel gerade eingerichtet - der angezeigte Wiederherstellungsschlüssel gilt nicht. Hole sein Backup.")
                return wiederherstellen(vorhanden)
            }
        }
        lokal.speichern(LokalerStand(bund, offenesBackup = null))
        return Eingerichtet.Neu(bund)
    }

    /**
     * KRITISCH: Nur ein Wiederherstellungsschlüssel, der das Backup dieses
     * Kontos entschlüsselt, führt zum Schlüsselbund. Nach [VERSUCHE] Fehlern
     * ist Schluss. Erst freischalten, dann lokal ablegen: Scheitert die
     * Freischaltung, fragt der nächste Lauf wieder nach dem Schlüssel, statt
     * mit einem Gerät weiterzumachen, das nichts speichern darf.
     */
    private suspend fun wiederherstellen(backup: GeholtesBackup): Eingerichtet {
        meldung("Für dieses Konto gibt es schon Schlüssel. Bitte den Wiederherstellungsschlüssel eingeben.")
        val bund = mitWiederherstellungsschluessel(backup) ?: throw EinrichtungAbgebrochen("abgebrochen")
        lokal.speichern(LokalerStand(bund, offenesBackup = null))
        return Eingerichtet.AusBackup(bund)
    }

    /**
     * Ein Gerät mit Schlüsseln, aber ohne Freischaltung, schaltet sich mit dem
     * Wiederherstellungsschlüssel frei. Ohne Eingabe bleibt es nur lesend; ein
     * freigeschaltetes Gerät kann es dann noch bestätigen. Der lokale
     * Schlüsselbund bleibt, wie er ist.
     */
    private suspend fun freischalten(backup: GeholtesBackup) {
        meldung(
            "Dieses Gerät ist noch nicht freigeschaltet. Zum Freischalten den Wiederherstellungsschlüssel " +
                "eingeben (leer = später; ein freigeschaltetes Gerät kann es auch bestätigen).",
        )
        if (mitWiederherstellungsschluessel(backup) == null) {
            meldung("Nicht freigeschaltet - dieses Gerät kann Daten holen, aber nichts ändern oder löschen.")
        }
    }

    /**
     * Fragt nach dem Wiederherstellungsschlüssel, bis er das Backup öffnet, und
     * schaltet das Gerät damit frei, falls nötig. Null heißt: Der Mensch hat
     * abgebrochen.
     */
    private suspend fun mitWiederherstellungsschluessel(backup: GeholtesBackup): Schluesselbund? {
        repeat(VERSUCHE) { versuch ->
            val text = frageSchluessel() ?: return null
            try {
                val schluessel = Wiederherstellungsschluessel.ausText(text)
                val bund = Schluesselbund.ausBackup(backup.chiffrat, schluessel, kontoId)
                if (!freigeschaltet) {
                    freischaltung.mitWiederherstellung(schluessel.signiereFreischaltung(geraetId))
                    meldung("Dieses Gerät ist jetzt freigeschaltet.")
                }
                return bund
            } catch (e: UngueltigerWiederherstellungsschluessel) {
                meldung("Ungültig: ${e.message}")
            } catch (_: EntschluesselnFehlgeschlagen) {
                // Prüfzeichen stimmen, aber der Schlüssel passt nicht zu diesem Backup.
                meldung("Dieser Schlüssel passt nicht zum Backup dieses Kontos.")
            }
            if (versuch < VERSUCHE - 1) meldung("Noch ${VERSUCHE - 1 - versuch} Versuch(e).")
        }
        throw EinrichtungAbgebrochen("$VERSUCHE Fehlversuche")
    }

    companion object {
        const val VERSUCHE = 3
    }
}
