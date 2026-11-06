package com.avlreminder.protokoll

import java.io.ByteArrayOutputStream
import java.util.zip.GZIPOutputStream

/** Der Server, an den Diagnosen gehen (im Test ersetzt; im Test-Client per Connect). */
interface DiagnoseAblage {
    /**
     * Sendet eine gepackte Diagnose; wirft [DiagnoseNichtFreigeschaltet], wenn das
     * Konto oder Gerät nicht senden darf, und [DiagnoseTageslimit], wenn der Server ablehnt,
     * weil für heute genug gesendet wurde.
     */
    suspend fun senden(nutzlast: ByteArray)
}

/**
 * Das Konto darf keine Diagnosen senden (docs/diagnose-entwurf.md, E-D-3), oder
 * dieses Gerät ist noch nicht freigeschaltet (E-B2b-13). Der Server meldet beides
 * mit demselben Code (diagnose.proto).
 */
class DiagnoseNichtFreigeschaltet :
    Exception("Dieses Konto ist für Diagnosen oder dieses Gerät ist noch nicht freigeschaltet - nichts gesendet")

/** Für heute ist das Limit erreicht (10 Sendungen je Konto und Tag, UTC; diagnose.proto). */
class DiagnoseTageslimit : Exception("Tageslimit für Diagnosen erreicht - morgen wieder")

/** Die gepackte Diagnose ist größer als [Diagnose.MAX_NUTZLAST]; gesendet wurde nichts. */
class DiagnoseZuGross(groesse: Int) : Exception("Diagnose zu groß: $groesse Bytes - nichts gesendet")

/**
 * Diagnose-Log (Format in docs/formate.md, „Diagnose-Sendung“). Liegt in
 * `protokoll`, weil die App dasselbe Packen und Senden braucht. Was in den Zeilen
 * steht, entscheidet der Logger der App; hier zählen nur JSON-Zeilen und Größe.
 */
object Diagnose {
    /** Formatkennung für den Kopf (erste Zeile) einer Sendung. */
    const val FORMAT = "avlreminder/diagnose/v1"

    /** Größte gepackte Sendung, die der Server annimmt (diagnose.proto). */
    const val MAX_NUTZLAST = 256 * 1024

    /**
     * Packt JSON-Zeilen mit gzip, je Zeile ein Objekt.
     *
     * KRITISCH: Eine Zeile mit Zeilenumbruch würde beim Lesen zu zwei halben
     * Objekten; darum lehnt [packen] sie ab, statt sie still zu teilen.
     */
    fun packen(zeilen: List<String>): ByteArray {
        require(zeilen.isNotEmpty()) { "Eine Diagnose braucht mindestens den Kopf" }
        require(zeilen.none { '\n' in it || '\r' in it }) { "Eine Zeile der Diagnose enthält einen Zeilenumbruch" }
        val bytes = ByteArrayOutputStream()
        GZIPOutputStream(bytes).use { gz ->
            for (z in zeilen) {
                gz.write(z.toByteArray(Charsets.UTF_8))
                gz.write('\n'.code)
            }
        }
        return bytes.toByteArray()
    }

    /**
     * Packt und sendet; liefert die Größe der Sendung in Bytes. Zu große Sendungen
     * lehnt es ab, bevor etwas hochgeladen wird ([DiagnoseZuGross]).
     */
    suspend fun senden(server: DiagnoseAblage, zeilen: List<String>): Int = sendeGepackt(server, packen(zeilen))

    /**
     * Sendet eine schon gepackte Diagnose, wenn sie höchstens [MAX_NUTZLAST] groß
     * ist. Eigene Funktion, damit Tests die Grenze aufs Byte treffen (gzip macht
     * das über [packen] kaum möglich).
     */
    internal suspend fun sendeGepackt(server: DiagnoseAblage, nutzlast: ByteArray): Int {
        if (nutzlast.size > MAX_NUTZLAST) throw DiagnoseZuGross(nutzlast.size)
        server.senden(nutzlast)
        return nutzlast.size
    }
}
