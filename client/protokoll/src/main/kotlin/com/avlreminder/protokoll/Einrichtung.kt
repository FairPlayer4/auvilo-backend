package com.avlreminder.protokoll

/** Ein Backup, wie der Server es liefert. */
class GeholtesBackup(val chiffrat: ByteArray, val version: Long)

/** Ein anderes Gerät hat das Backup inzwischen geändert (Server: failed_precondition). */
class VersionVeraltet : Exception("Backup wurde inzwischen von einem anderen Gerät geändert")

/** Die Einrichtung ist gescheitert oder wurde abgebrochen. */
class EinrichtungAbgebrochen(grund: String) : Exception(grund)

/** Das Backup auf dem Server (im Test ersetzt; im Test-Client per Connect). */
interface BackupAblage {
    /** Das Backup des Kontos, oder null, wenn es keins gibt. */
    suspend fun holen(): GeholtesBackup?

    /** Speichert das Backup; wirft [VersionVeraltet], wenn [erwartet] nicht mehr stimmt. */
    suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long
}

/**
 * Was das Gerät selbst für das Konto ablegt.
 *
 * @property offenesBackup das Backup, das dieses Gerät erzeugt, aber noch nicht
 *   sicher hochgeladen hat; null, wenn nichts offen ist.
 */
class LokalerStand(val bund: Schluesselbund, val offenesBackup: ByteArray?)

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

    /** Das Gerät hatte die Schlüssel schon; nichts geändert. */
    class Vorhanden(override val bund: Schluesselbund) : Eingerichtet
}

/**
 * Richtet die Schlüssel auf einem Gerät ein (E-B2-5): Gibt es auf dem Server ein
 * Backup, entschlüsselt es dieses mit dem Wiederherstellungsschlüssel, den der
 * Mensch eintippt. Gibt es keins, erzeugt es neue Schlüssel und lädt das Backup
 * hoch. Liegt in `protokoll`, weil die App denselben Ablauf braucht.
 *
 * @param frageSchluessel fragt den Menschen nach dem Wiederherstellungsschlüssel;
 *   null heißt Abbruch.
 * @param zeigeWiederherstellung zeigt einen neuen Wiederherstellungsschlüssel;
 *   true heißt, der Mensch hat ihn aufgeschrieben.
 * @param meldung Hinweise an den Menschen.
 */
class Einrichtung(
    private val kontoId: String,
    private val server: BackupAblage,
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
            val offen = stand.offenesBackup ?: return Eingerichtet.Vorhanden(stand.bund)
            meldung("Das Backup dieses Geräts ist noch nicht sicher hochgeladen - versuche es erneut.")
            return hochladen(stand.bund, offen)
        }
        server.holen()?.let { return wiederherstellen(it) }

        val bund = Schluesselbund.erzeugen()
        val wiederherstellung = Wiederherstellungsschluessel.erzeugen()
        val chiffrat = bund.backupVerschluesseln(wiederherstellung, kontoId)
        if (!zeigeWiederherstellung(wiederherstellung)) {
            throw EinrichtungAbgebrochen("Wiederherstellungsschlüssel nicht bestätigt - es wurde nichts gespeichert")
        }
        lokal.speichern(LokalerStand(bund, offenesBackup = chiffrat))
        return hochladen(bund, chiffrat)
    }

    /**
     * KRITISCH: Ein vorhandenes Backup wird nie überschrieben (erwartete Version 0).
     * Liegt schon genau unser Backup dort, war ein früheres Hochladen erfolgreich und
     * nur die Antwort ging verloren. Liegt ein anderes dort, war ein anderes Gerät
     * schneller; dann übernimmt dieses Gerät dessen Schlüssel. Sonst gäbe es zwei
     * verschiedene Personenschlüssel für dasselbe Konto.
     */
    private suspend fun hochladen(bund: Schluesselbund, chiffrat: ByteArray): Eingerichtet {
        try {
            server.speichern(chiffrat, erwartet = 0)
        } catch (_: VersionVeraltet) {
            val vorhanden = server.holen() ?: throw EinrichtungAbgebrochen("Backup ist verschwunden - später erneut versuchen")
            if (!vorhanden.chiffrat.contentEquals(chiffrat)) {
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
     * ist Schluss.
     */
    private fun wiederherstellen(backup: GeholtesBackup): Eingerichtet {
        meldung("Für dieses Konto gibt es schon Schlüssel. Bitte den Wiederherstellungsschlüssel eingeben.")
        repeat(VERSUCHE) { versuch ->
            val text = frageSchluessel() ?: throw EinrichtungAbgebrochen("abgebrochen")
            try {
                val bund = Schluesselbund.ausBackup(backup.chiffrat, Wiederherstellungsschluessel.ausText(text), kontoId)
                lokal.speichern(LokalerStand(bund, offenesBackup = null))
                return Eingerichtet.AusBackup(bund)
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
