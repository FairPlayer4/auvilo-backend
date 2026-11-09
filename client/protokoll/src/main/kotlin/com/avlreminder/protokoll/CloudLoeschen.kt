package com.avlreminder.protokoll

/**
 * Ein Gerät des Kontos, wie der Server es auflistet.
 *
 * @property diesesGeraet das Gerät, das gerade anfragt.
 */
data class KontoGeraet(val geraetId: String, val gesperrt: Boolean, val diesesGeraet: Boolean)

/**
 * Was der Server zum Löschen aller Cloud-Daten eines Kontos braucht. Fehlt, was gelöscht
 * werden soll, ist das kein Fehler. Ist dieses Gerät nicht freigeschaltet, wirft alles
 * außer [geraete] und dem Sperren dieses Geräts [GeraetNichtFreigeschaltet].
 */
interface KontoAufDemServer {
    suspend fun geraete(): List<KontoGeraet>

    /** Sperrt ein Gerät endgültig; dieses Gerät darf sich immer selbst sperren. */
    suspend fun sperren(geraetId: String)

    /** Löscht die Datensicherung mit allen Fassungen. */
    suspend fun datensicherungLoeschen()

    /** Löscht das Schlüssel-Backup. */
    suspend fun schluesselBackupLoeschen()
}

/** Kein Gerät der Liste ist dieses: Der Server antwortet nicht wie erwartet. */
class DiesesGeraetFehlt : Exception("Der Server listet dieses Gerät nicht")

/**
 * „Cloud-Daten löschen“ (Kap. 11, 2026-11-09): löscht Datensicherung und Schlüssel-Backup
 * und sperrt alle Geräte des Kontos. App und Test-Client nutzen denselben Ablauf.
 */
object CloudLoeschen {
    /**
     * Sperrt erst alle anderen Geräte, löscht dann beide Backups und sperrt zuletzt dieses
     * Gerät.
     *
     * KRITISCH: Die Reihenfolge ist die Sicherung des Ablaufs.
     * - Die anderen Geräte zuerst: Sonst legt ein anderes Gerät die Datensicherung zwischen
     *   Löschen und Sperren neu an, ohne Schlüssel-Backup, und ein neues Gerät scheitert
     *   an der Einrichtung.
     * - Dieses Gerät zuletzt: Gesperrt darf es nichts mehr löschen oder sperren.
     * - Bricht es mittendrin ab (Netz), lässt es sich wiederholen: Gesperrte Geräte werden
     *   übersprungen, fehlende Backups sind kein Fehler.
     *
     * @return wie viele andere Geräte jetzt gesperrt wurden.
     */
    suspend fun alles(konto: KontoAufDemServer): Int {
        val geraete = konto.geraete()
        val dieses = geraete.singleOrNull { it.diesesGeraet } ?: throw DiesesGeraetFehlt()
        val andere = geraete.filter { !it.diesesGeraet && !it.gesperrt }
        for (g in andere) konto.sperren(g.geraetId)
        konto.datensicherungLoeschen()
        konto.schluesselBackupLoeschen()
        konto.sperren(dieses.geraetId)
        return andere.size
    }
}
