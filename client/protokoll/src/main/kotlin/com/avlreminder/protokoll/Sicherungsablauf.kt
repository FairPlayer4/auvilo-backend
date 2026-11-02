package com.avlreminder.protokoll

import java.time.Instant

/** Eine Datensicherung, wie der Server sie liefert. */
class GeholteSicherung(val chiffrat: ByteArray, val version: Long)

/** Die Datensicherung auf dem Server (im Test ersetzt; im Test-Client per Connect). */
interface SicherungsAblage {
    /** Die neueste Sicherung des Kontos, oder null, wenn es keine gibt. */
    suspend fun holen(): GeholteSicherung?

    /** Speichert die Sicherung; wirft [VersionVeraltet], wenn [erwartet] nicht mehr stimmt (0 = noch keine). */
    suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long

    /** Löscht die Sicherung mit allen älteren Fassungen. */
    suspend fun loeschen()
}

/**
 * Was ein Gerät sich über die Sicherungen merkt (E-B2b-3).
 *
 * @property zaehler der höchste Zähler, den dieses Gerät gesichert oder eingespielt hat.
 * @property version die Server-Version dieser Sicherung; stimmt sie noch, hat seitdem
 *   kein anderes Gerät gesichert.
 */
data class GemerkteSicherung(val zaehler: Long, val version: Long)

/** Die Ablage auf dem Gerät für [GemerkteSicherung]. */
interface SicherungsStand {
    fun laden(): GemerkteSicherung?

    fun speichern(stand: GemerkteSicherung)
}

/** Die Sicherung auf dem Server ist älter als eine, die dieses Gerät schon kennt. */
class AeltereSicherung(gemerkt: Long, geliefert: Long) :
    Exception("Der Server liefert eine ältere Sicherung (Zähler $geliefert, dieses Gerät kennt schon $gemerkt) - nichts geändert")

/** Ergebnis von [Sicherungsablauf.sichern]. */
sealed interface Gesichert {
    /** Hochgeladen; [version] ist die neue Server-Version. */
    data class Ja(val version: Long, val zaehler: Long) : Gesichert

    /** Ein anderes Gerät sichert; der Mensch wollte nicht übernehmen. Nichts geändert. */
    data class AnderesGeraet(val kopf: Sicherungskopf) : Gesichert
}

/** Ergebnis von [Sicherungsablauf.wiederherstellen]. */
sealed interface Wiederhergestellt {
    data class Ja(val kopf: Sicherungskopf) : Wiederhergestellt

    data object KeineSicherung : Wiederhergestellt

    /** Der Mensch hat nach Anzeige des Kopfs abgelehnt. Nichts geändert. */
    data object Abgelehnt : Wiederhergestellt
}

/**
 * Sichert die Daten eines Geräts verschlüsselt auf dem Server und spielt sie
 * wieder ein (Etappe B2b). Liegt in `protokoll`, weil die App denselben Ablauf
 * braucht; die Nutzlast (bei der App die Datenbank) ist hier nur Bytes.
 *
 * @param uebernehmen fragt den Menschen, ob dieses Gerät die Sicherung von dem
 *   Gerät im gezeigten Kopf übernehmen soll (E-B2b-6).
 */
class Sicherungsablauf(
    private val kontoId: String,
    private val geraetId: String,
    private val tresor: SymmetrischerSchluessel,
    private val server: SicherungsAblage,
    private val stand: SicherungsStand,
    private val uebernehmen: (Sicherungskopf) -> Boolean,
    private val uhr: () -> Instant = Instant::now,
) {
    /**
     * KRITISCH: Überschreibt nie unbemerkt die Sicherung eines anderen Geräts und nie
     * eine neuere, als dieses Gerät kennt. Ohne Konflikt lädt es mit der gemerkten
     * Version hoch. Meldet der Server eine andere Version, holt es die Sicherung und
     * entscheidet nach ihrem Kopf:
     *  - von diesem Gerät (die Antwort ging verloren, oder der Stand fehlt): weiter
     *    mit der Server-Version,
     *  - von einem anderen Gerät: nur nach Rückfrage,
     *  - Zähler kleiner als gemerkt: [AeltereSicherung], nichts hochladen.
     */
    suspend fun sichern(nutzlast: ByteArray, schemaVersion: Int): Gesichert {
        val gemerkt = stand.laden()
        var zaehler = (gemerkt?.zaehler ?: 0) + 1
        var erwartet = gemerkt?.version ?: 0
        // Höchstens ein zweiter Versuch: nach dem Abgleich mit der Server-Fassung.
        repeat(2) { versuch ->
            val chiffrat = Datensicherung.verschluesseln(
                Sicherung(Sicherungskopf(zaehler, uhr(), geraetId, schemaVersion), nutzlast),
                tresor,
                kontoId,
            )
            require(chiffrat.size <= Datensicherung.MAX_CHIFFRAT) {
                "Sicherung zu groß: ${chiffrat.size} Bytes, erlaubt ${Datensicherung.MAX_CHIFFRAT}"
            }
            try {
                val version = server.speichern(chiffrat, erwartet)
                stand.speichern(GemerkteSicherung(zaehler, version))
                return Gesichert.Ja(version, zaehler)
            } catch (e: VersionVeraltet) {
                if (versuch == 1) throw e // erneut überholt: später wieder versuchen
            }
            val vorhanden = server.holen()
            if (vorhanden == null) {
                // Inzwischen gelöscht: neu anlegen.
                erwartet = 0
                return@repeat
            }
            val kopf = Datensicherung.entschluesseln(vorhanden.chiffrat, tresor, kontoId).kopf
            if (gemerkt != null && kopf.zaehler < gemerkt.zaehler) throw AeltereSicherung(gemerkt.zaehler, kopf.zaehler)
            if (kopf.geraetId != geraetId && !uebernehmen(kopf)) return Gesichert.AnderesGeraet(kopf)
            zaehler = maxOf(zaehler, kopf.zaehler + 1)
            erwartet = vorhanden.version
        }
        error("nicht erreichbar")
    }

    /**
     * Spielt die neueste Sicherung ein: holen, entschlüsseln, den Kopf zeigen und
     * erst nach Bestätigung [anwenden] aufrufen.
     *
     * KRITISCH: Nichts wird angewendet, wenn die Sicherung sich nicht entschlüsseln
     * lässt, älter ist als gemerkt ([AeltereSicherung]), eine Schema-Version über
     * [hoechsteSchemaVersion] hat oder der Mensch ablehnt. Den Stand merkt sich das
     * Gerät erst, wenn [anwenden] ohne Fehler durchgelaufen ist; danach sichert es
     * selbst weiter.
     */
    suspend fun wiederherstellen(
        hoechsteSchemaVersion: Int,
        bestaetigen: (Sicherungskopf) -> Boolean,
        anwenden: (Sicherung) -> Unit,
    ): Wiederhergestellt {
        val geholt = server.holen() ?: return Wiederhergestellt.KeineSicherung
        val sicherung = Datensicherung.entschluesseln(geholt.chiffrat, tresor, kontoId)
        val gemerkt = stand.laden()
        if (gemerkt != null && sicherung.kopf.zaehler < gemerkt.zaehler) {
            throw AeltereSicherung(gemerkt.zaehler, sicherung.kopf.zaehler)
        }
        if (sicherung.kopf.schemaVersion > hoechsteSchemaVersion) {
            throw NeueresSchema(sicherung.kopf.schemaVersion, hoechsteSchemaVersion)
        }
        if (!bestaetigen(sicherung.kopf)) return Wiederhergestellt.Abgelehnt
        anwenden(sicherung)
        stand.speichern(GemerkteSicherung(sicherung.kopf.zaehler, geholt.version))
        return Wiederhergestellt.Ja(sicherung.kopf)
    }
}

/** Die Sicherung stammt aus einer neueren App-Version. */
class NeueresSchema(gesichert: Int, bekannt: Int) :
    Exception("Die Sicherung hat Schema-Version $gesichert, diese App kennt nur bis $bekannt - bitte die App aktualisieren")
