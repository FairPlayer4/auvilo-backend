package com.avlreminder.protokoll

import java.time.Instant
import kotlin.coroutines.cancellation.CancellationException

/** Ergebnis von [KontoLoeschen.loeschen]. */
sealed interface KontoLoeschung {
    /** Das Konto ist auf dem Server gelöscht; dieses Gerät hat seine Daten dazu verworfen. */
    data object Geloescht : KontoLoeschung

    /**
     * Dieses Gerät ist nicht freigeschaltet: Der Server löscht erst ab [ab], und bis dahin
     * kann jedes freigeschaltete Gerät abbrechen (B1, konto.proto).
     */
    data class Beantragt(val ab: Instant) : KontoLoeschung
}

/** Die Löschung ist schon fällig oder läuft; abbrechen geht nicht mehr. */
class LoeschungLaeuft : Exception("Die Löschung des Kontos ist schon fällig")

/**
 * Was „Konto löschen“ vom Server braucht (konto.proto).
 *
 * [abbrechen] wirft [GeraetNichtFreigeschaltet], wenn dieses Gerät nicht freigeschaltet
 * ist, und [LoeschungLaeuft], wenn der Termin schon erreicht ist.
 */
interface KontoLoeschenAmServer {
    suspend fun loeschen(): KontoLoeschung

    /** Bricht einen offenen Antrag ab; ohne Antrag ist das kein Fehler. */
    suspend fun abbrechen()

    /**
     * Nach einem Fehler von [loeschen]: true, wenn der Server dieses Gerät abweist
     * (gesperrt oder unbekannt). Dann war das Löschen schon durch, und nur die Antwort
     * kam nicht an. false, wenn der Zugang noch da ist oder sich das nicht prüfen lässt.
     * Ein Fehler hier heißt: unklar.
     *
     * Ohne eigene Umsetzung false; dann bleibt es beim Fehler wie bisher.
     */
    suspend fun zugangWeg(): Boolean = false
}

/**
 * „Konto löschen“ (B1 in docs/anforderungen.md Kap. 3.5). App und Test-Client nutzen
 * denselben Ablauf.
 */
object KontoLoeschen {
    /**
     * Löscht das Konto (freigeschaltetes Gerät) oder beantragt die Löschung.
     *
     * KRITISCH: [vergessen] (Sitzung, Geräteschlüssel, Schlüssel der Sicherung verwerfen)
     * läuft nur, wenn das Löschen auf dem Server durch ist: Der Server hat es bestätigt,
     * oder nach einem Fehler weist er dieses Gerät ab ([KontoLoeschenAmServer.zugangWeg],
     * etwa wenn nur die Antwort verloren ging). Bei einem Antrag oder einem anderen Fehler
     * bleiben die Schlüssel: Sonst wären die Daten auf dem Server noch da, aber dieses
     * Gerät könnte sie weder lesen noch den Antrag abbrechen.
     *
     * Bekannte Grenze: Ein Gerät, das ein anderes Gerät gerade gesperrt hat, oder eines,
     * dessen Uhr mehr als 2 Minuten falsch geht, wird ebenfalls abgewiesen und gilt dann
     * als gelöscht. In beiden Fällen konnte es ohne neues Anmelden mit Google ohnehin
     * nichts mehr tun.
     */
    suspend fun loeschen(server: KontoLoeschenAmServer, vergessen: suspend () -> Unit): KontoLoeschung {
        val ergebnis = try {
            server.loeschen()
        } catch (e: CancellationException) {
            throw e // abgebrochen: nichts nachfragen
        } catch (e: Exception) {
            if (!zugangWeg(server, e)) throw e
            KontoLoeschung.Geloescht
        }
        if (ergebnis == KontoLoeschung.Geloescht) vergessen()
        return ergebnis
    }

    /** [KontoLoeschenAmServer.zugangWeg]; scheitert die Prüfung, gilt der Zugang als da. */
    private suspend fun zugangWeg(server: KontoLoeschenAmServer, fehler: Exception): Boolean =
        try {
            server.zugangWeg()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            fehler.addSuppressed(e)
            false
        }
}
