package com.avlreminder.testclient

import com.avlreminder.protokoll.EntschluesselnFehlgeschlagen
import com.avlreminder.protokoll.Schluesselbund
import com.avlreminder.protokoll.UngueltigerWiederherstellungsschluessel
import com.avlreminder.protokoll.Wiederherstellungsschluessel

/** Ein Backup, wie der Server es liefert. */
class GeholtesBackup(val chiffrat: ByteArray, val version: Long)

/** Ein anderes Gerät hat das Backup inzwischen geändert (Server: failed_precondition). */
class VersionVeraltet : Exception("Backup wurde inzwischen von einem anderen Gerät geändert")

/** Die Einrichtung ist gescheitert oder wurde abgebrochen; es wurde nichts gespeichert. */
class EinrichtungAbgebrochen(grund: String) : Exception(grund)

/** Was die Einrichtung vom Server braucht (im Test ersetzt). */
interface BackupAblage {
    /** Das Backup des Kontos, oder null, wenn es keins gibt. */
    suspend fun holen(): GeholtesBackup?

    /** Speichert das Backup; wirft [VersionVeraltet], wenn [erwartet] nicht mehr stimmt. */
    suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long
}

/** Ergebnis der Einrichtung. */
sealed interface Eingerichtet {
    val bund: Schluesselbund

    /** Dieses Gerät hat die Schlüssel erzeugt. Der Mensch muss [wiederherstellung] aufschreiben. */
    class Neu(override val bund: Schluesselbund, val wiederherstellung: Wiederherstellungsschluessel) : Eingerichtet

    /** Die Schlüssel kommen aus dem Backup eines anderen Geräts. */
    class AusBackup(override val bund: Schluesselbund) : Eingerichtet
}

/**
 * Richtet die Schlüssel auf einem Gerät ein, das noch keine hat (E-B2-5):
 * Gibt es ein Backup, entschlüsselt es dieses mit dem Wiederherstellungsschlüssel,
 * den der Mensch eintippt. Gibt es keins, erzeugt es neue Schlüssel und lädt das
 * Backup hoch.
 *
 * @param frageSchluessel fragt den Menschen nach dem Wiederherstellungsschlüssel;
 *   null heißt Abbruch.
 * @param meldung Hinweise an den Menschen.
 */
class Einrichtung(
    private val kontoId: String,
    private val ablage: BackupAblage,
    private val frageSchluessel: () -> String?,
    private val meldung: (String) -> Unit,
) {
    /**
     * KRITISCH: Ein vorhandenes Backup wird nie überschrieben – weder beim
     * ersten Blick (dann wird es geholt) noch, wenn ein anderes Gerät zwischen
     * Holen und Speichern schneller war (erwartete Version 0 schlägt dann fehl,
     * und dieses Gerät holt stattdessen dessen Backup). Sonst gäbe es zwei
     * verschiedene Personenschlüssel für dasselbe Konto.
     */
    suspend fun ausfuehren(): Eingerichtet {
        ablage.holen()?.let { return wiederherstellen(it) }

        val bund = Schluesselbund.erzeugen()
        val wiederherstellung = Wiederherstellungsschluessel.erzeugen()
        return try {
            ablage.speichern(bund.backupVerschluesseln(wiederherstellung, kontoId), erwartet = 0)
            Eingerichtet.Neu(bund, wiederherstellung)
        } catch (_: VersionVeraltet) {
            meldung("Ein anderes Gerät hat die Schlüssel gerade eingerichtet - hole sein Backup.")
            val backup = ablage.holen() ?: throw EinrichtungAbgebrochen("Backup ist verschwunden")
            wiederherstellen(backup)
        }
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
                val schluessel = Wiederherstellungsschluessel.ausText(text)
                return Eingerichtet.AusBackup(Schluesselbund.ausBackup(backup.chiffrat, schluessel, kontoId))
            } catch (e: UngueltigerWiederherstellungsschluessel) {
                meldung("Ungültig: ${e.message}")
            } catch (_: EntschluesselnFehlgeschlagen) {
                // Prüfzeichen stimmen, aber es ist der Schlüssel eines anderen Kontos.
                meldung("Dieser Schlüssel passt nicht zu diesem Konto.")
            }
            if (versuch < VERSUCHE - 1) meldung("Noch ${VERSUCHE - 1 - versuch} Versuch(e).")
        }
        throw EinrichtungAbgebrochen("$VERSUCHE Fehlversuche")
    }

    companion object {
        const val VERSUCHE = 3
    }
}
