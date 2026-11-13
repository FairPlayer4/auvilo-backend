package com.avlreminder.protokoll

import java.time.Instant

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
     * läuft nur, wenn der Server das Löschen bestätigt hat. Bei einem Antrag oder einem
     * Fehler (Netz) bleiben die Schlüssel: Sonst wären die Daten auf dem Server noch da,
     * aber dieses Gerät könnte sie weder lesen noch den Antrag abbrechen.
     */
    suspend fun loeschen(server: KontoLoeschenAmServer, vergessen: suspend () -> Unit): KontoLoeschung {
        val ergebnis = server.loeschen()
        if (ergebnis == KontoLoeschung.Geloescht) vergessen()
        return ergebnis
    }
}
