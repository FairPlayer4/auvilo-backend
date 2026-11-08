package com.avlreminder.protokoll

import java.time.Instant

/** Eine Datensicherung, wie der Server sie liefert. */
class GeholteSicherung(val chiffrat: ByteArray, val version: Long)

/** Die Datensicherung auf dem Server (im Test ersetzt; im Test-Client per Connect). */
interface SicherungsAblage {
    /**
     * Die neueste Sicherung des Kontos, oder null, wenn es keine gibt; wirft
     * [SicherungTageslimit], wenn für heute genug geholt wurde.
     */
    suspend fun holen(): GeholteSicherung?

    /**
     * Speichert die Sicherung; wirft [VersionVeraltet], wenn [erwartet] nicht mehr
     * stimmt (0 = noch keine), [SicherungZuGross], wenn der Server sie als zu groß
     * ablehnt, und [SicherungTageslimit], wenn für heute genug gesichert wurde.
     */
    suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long

    /**
     * Löscht die Sicherung mit allen älteren Fassungen; wirft [SicherungTageslimit],
     * wenn für heute genug gelöscht wurde.
     */
    suspend fun loeschen()
}

/**
 * Was ein Gerät sich über die Sicherungen merkt (E-B2b-3).
 *
 * @property zaehler der höchste Zähler, den dieses Gerät gesichert oder eingespielt hat.
 * @property version die Server-Version dieser Sicherung; stimmt sie noch, hat seitdem
 *   kein anderes Gerät gesichert (0 = keine bekannt, etwa nach dem Löschen).
 * @property gesehen der höchste Zähler einer Sicherung, die dieses Gerät gesehen, aber
 *   nicht übernommen hat (der Mensch hat abgelehnt). Sie ist echt, denn sie ließ sich
 *   entschlüsseln; ältere Fassungen gelten deshalb ebenfalls als veraltet.
 * @property abgelehnt der Mensch wollte die Sicherung eines anderen Geräts nicht
 *   übernehmen (E-B2b-6: „fragt einmal“). Solange das gilt, sichert dieses Gerät nicht
 *   mehr automatisch, erst wieder, wenn der Mensch selbst sichert, wiederherstellt
 *   oder löscht.
 */
data class GemerkteSicherung(val zaehler: Long, val version: Long, val gesehen: Long = 0, val abgelehnt: Boolean = false) {
    /** Sicherungen mit kleinerem Zähler sind älter, als dieses Gerät kennt. */
    val untergrenze get() = maxOf(zaehler, gesehen)
}

/** Die Ablage auf dem Gerät für [GemerkteSicherung]. */
interface SicherungsStand {
    fun laden(): GemerkteSicherung?

    fun speichern(stand: GemerkteSicherung)
}

/** Die Sicherung auf dem Server ist älter als eine, die dieses Gerät schon kennt. */
class AeltereSicherung(gemerkt: Long, geliefert: Long) :
    Exception("Der Server liefert eine ältere Sicherung (Zähler $geliefert, dieses Gerät kennt schon $gemerkt) - nichts geändert")

/**
 * Die Sicherung ist größer als erlaubt ([Datensicherung.MAX_CHIFFRAT]). Kommt vom
 * Ablauf selbst, bevor etwas hochgeladen wird; der Server lehnt zu große ebenfalls
 * ab. Die App meldet "Sicherung zu groß".
 */
class SicherungZuGross(groesse: Int?) :
    Exception("Sicherung zu groß" + (groesse?.let { ": $it Bytes" } ?: "") + " - nichts hochgeladen")

/**
 * Für heute hat das Konto genug gesichert, geholt oder gelöscht (je Tag, UTC,
 * höchstens 48 Sicherungen, 50 Abrufe, 20 Löschungen; datensicherung.proto).
 * Die Sicherung auf dem Server bleibt, wie sie ist; die App versucht es am
 * nächsten Tag wieder, statt in einer Schleife zu wiederholen. [vorgang] nennt,
 * welches Limit erreicht ist.
 */
class SicherungTageslimit(vorgang: String = "Sicherungen") :
    Exception("Tageslimit für $vorgang erreicht - morgen wieder, nichts geändert")

/** Ergebnis von [Sicherungsablauf.sichern]. */
sealed interface Gesichert {
    /** Hochgeladen; [version] ist die neue Server-Version. */
    data class Ja(val version: Long, val zaehler: Long) : Gesichert

    /**
     * Ein anderes Gerät sichert; der Mensch wollte nicht übernehmen. Auf dem Server
     * bleibt alles, wie es ist; das Gerät merkt sich den Zähler als gesehen und dass
     * die automatische Sicherung ruht ([Ruht]).
     */
    data class AnderesGeraet(val kopf: Sicherungskopf) : Gesichert

    /**
     * Automatisches Sichern ruht, weil der Mensch die Übernahme schon abgelehnt hat
     * ([GemerkteSicherung.abgelehnt]); der Server wurde nicht gefragt. Die App bietet
     * an, von Hand zu sichern (dann fragt sie wieder).
     */
    data object Ruht : Gesichert
}

/** Ergebnis von [Sicherungsablauf.wiederherstellen]. */
sealed interface Wiederhergestellt {
    data class Ja(val kopf: Sicherungskopf) : Wiederhergestellt

    data object KeineSicherung : Wiederhergestellt

    /**
     * Der Mensch hat nach Anzeige des Kopfs abgelehnt. Nichts angewendet; das Gerät merkt
     * sich den Zähler als gesehen. Stammt die Sicherung von einem anderen Gerät, gilt das
     * als „nicht übernehmen“, und die automatische Sicherung ruht (E-B2b-6).
     */
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
     *
     * KRITISCH: [automatisch] (die stündliche Sicherung der App) fragt nach einer
     * Ablehnung nicht erneut und ruft den Server gar nicht erst auf ([Gesichert.Ruht]).
     * Sonst verbrauchte jeder Lauf einen Versuch vom Tageslimit (Review-Lauf 8, Frage 6),
     * und der Mensch würde stündlich gefragt. Von Hand ([automatisch] = false) fragt es
     * wieder.
     */
    suspend fun sichern(nutzlast: ByteArray, schemaVersion: Int, automatisch: Boolean): Gesichert {
        val gemerkt = stand.laden()
        if (automatisch && gemerkt?.abgelehnt == true) return Gesichert.Ruht
        var zaehler = (gemerkt?.untergrenze ?: 0) + 1
        var erwartet = gemerkt?.version ?: 0
        // Höchstens ein zweiter Versuch: nach dem Abgleich mit der Server-Fassung.
        repeat(2) { versuch ->
            val chiffrat = Datensicherung.verschluesseln(
                Sicherung(Sicherungskopf(zaehler, uhr(), geraetId, schemaVersion), nutzlast),
                tresor,
                kontoId,
            )
            if (chiffrat.size > Datensicherung.MAX_CHIFFRAT) throw SicherungZuGross(chiffrat.size)
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
            if (gemerkt != null && kopf.zaehler < gemerkt.untergrenze) throw AeltereSicherung(gemerkt.untergrenze, kopf.zaehler)
            if (kopf.geraetId != geraetId && !uebernehmen(kopf)) {
                merkeGesehen(gemerkt, kopf.zaehler, abgelehnt = true)
                return Gesichert.AnderesGeraet(kopf)
            }
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
     * [hoechsteSchemaVersion] hat oder der Mensch ablehnt (dann merkt es sich nur
     * den Zähler als gesehen). Den Stand merkt sich das Gerät erst, wenn [anwenden]
     * ohne Fehler durchgelaufen ist; danach sichert es selbst weiter.
     */
    suspend fun wiederherstellen(
        hoechsteSchemaVersion: Int,
        bestaetigen: (Sicherungskopf) -> Boolean,
        anwenden: (Sicherung) -> Unit,
    ): Wiederhergestellt {
        val geholt = server.holen() ?: return Wiederhergestellt.KeineSicherung
        val sicherung = Datensicherung.entschluesseln(geholt.chiffrat, tresor, kontoId)
        val gemerkt = stand.laden()
        if (gemerkt != null && sicherung.kopf.zaehler < gemerkt.untergrenze) {
            throw AeltereSicherung(gemerkt.untergrenze, sicherung.kopf.zaehler)
        }
        if (sicherung.kopf.schemaVersion > hoechsteSchemaVersion) {
            throw NeueresSchema(sicherung.kopf.schemaVersion, hoechsteSchemaVersion)
        }
        if (!bestaetigen(sicherung.kopf)) {
            // KRITISCH: Nur die Fassung eines anderen Geräts ist eine abgelehnte Übernahme.
            // Die eigene abzulehnen darf das automatische Sichern nicht stilllegen.
            merkeGesehen(gemerkt, sicherung.kopf.zaehler, abgelehnt = sicherung.kopf.geraetId != geraetId)
            return Wiederhergestellt.Abgelehnt
        }
        anwenden(sicherung)
        stand.speichern(GemerkteSicherung(sicherung.kopf.zaehler, geholt.version))
        return Wiederhergestellt.Ja(sicherung.kopf)
    }

    /**
     * Löscht die Sicherung mit allen älteren Fassungen auf dem Server.
     *
     * KRITISCH: Der gemerkte Zähler bleibt, nur die Version wird 0. Die nächste
     * Sicherung zählt also weiter; finge sie bei 1 an, lehnten andere Geräte, die
     * schon einen höheren Zähler kennen, sie als [AeltereSicherung] ab. Eine
     * Ablehnung gilt danach nicht mehr: Es gibt keine fremde Sicherung mehr zu schützen.
     */
    suspend fun loeschen() = loeschen(server, stand)

    companion object {
        /**
         * Wie [Sicherungsablauf.loeschen], aber ohne Schlüssel: Zum Löschen braucht es
         * keinen Tresorschlüssel, etwa wenn auf dem Gerät keine Schlüssel mehr sind.
         */
        suspend fun loeschen(server: SicherungsAblage, stand: SicherungsStand) {
            server.loeschen()
            val gemerkt = stand.laden() ?: return
            stand.speichern(gemerkt.copy(version = 0, abgelehnt = false))
        }
    }

    /**
     * Merkt sich den Zähler einer abgelehnten Sicherung als Untergrenze (E-B2b-3) und,
     * mit [abgelehnt], dass der Mensch die Übernahme abgelehnt hat (E-B2b-6). Eine
     * frühere Ablehnung bleibt bestehen.
     */
    private fun merkeGesehen(gemerkt: GemerkteSicherung?, zaehler: Long, abgelehnt: Boolean) {
        val bisher = gemerkt ?: GemerkteSicherung(0, 0)
        val neu = bisher.copy(gesehen = maxOf(bisher.gesehen, zaehler), abgelehnt = bisher.abgelehnt || abgelehnt)
        if (neu != bisher) stand.speichern(neu)
    }
}

/** Die Sicherung stammt aus einer neueren App-Version. */
class NeueresSchema(gesichert: Int, bekannt: Int) :
    Exception("Die Sicherung hat Schema-Version $gesichert, diese App kennt nur bis $bekannt - bitte die App aktualisieren")
