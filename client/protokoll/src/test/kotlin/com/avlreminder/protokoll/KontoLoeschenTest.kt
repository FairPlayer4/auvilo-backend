package com.avlreminder.protokoll

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.io.IOException
import java.time.Instant
import kotlin.coroutines.cancellation.CancellationException

/** „Konto löschen“ (B1): Dieses Gerät vergisst seine Daten nur nach bestätigtem Löschen. */
class KontoLoeschenTest {

    /**
     * Der Server antwortet mit [antwort] oder wirft [fehler]. [zugang] ist die Antwort von
     * zugangWeg: true/false oder eine Ausnahme; null = die Standardumsetzung.
     */
    class FalscherServer(
        private val antwort: KontoLoeschung?,
        private val fehler: Exception? = null,
        private val zugang: Any? = false,
    ) : KontoLoeschenAmServer {
        var gefragt = 0
        override suspend fun loeschen(): KontoLoeschung = fehler?.let { throw it } ?: antwort!!
        override suspend fun abbrechen() = error("nicht gebraucht")
        override suspend fun zugangWeg(): Boolean {
            gefragt++
            return when (val z = zugang) {
                null -> super.zugangWeg()
                is Exception -> throw z
                else -> z as Boolean
            }
        }
    }

    data class Fall(val name: String, val server: FalscherServer, val vergessen: Int, val erwartet: Any, val gefragt: Int = 0) {
        override fun toString() = name
    }

    // KRITISCH: Nur ein vom Server bestätigtes Löschen verwirft die lokalen Daten. Bei
    // einem Antrag oder Fehler bleibt das Gerät, wie es war, und kann abbrechen oder es
    // erneut versuchen.
    @ParameterizedTest(name = "{0}")
    @MethodSource("faelle")
    fun `vergisst die lokalen Daten nur nach bestätigtem Löschen`(fall: Fall) {
        var vergessen = 0

        val ergebnis = runCatching { runBlocking { KontoLoeschen.loeschen(fall.server) { vergessen++ } } }

        assertEquals(fall.vergessen, vergessen)
        assertEquals(fall.gefragt, fall.server.gefragt, "zugangWeg gefragt")
        when (val erwartet = fall.erwartet) {
            is Class<*> -> assertInstanceOf(erwartet, ergebnis.exceptionOrNull())
            else -> assertEquals(erwartet, ergebnis.getOrThrow())
        }
    }

    companion object {
        private val TERMIN = Instant.parse("2026-11-19T10:00:00Z")

        @JvmStatic
        fun faelle() = listOf(
            Fall("gelöscht: vergisst einmal", FalscherServer(KontoLoeschung.Geloescht), 1, KontoLoeschung.Geloescht),
            Fall("beantragt: vergisst nichts und nennt den Termin", FalscherServer(KontoLoeschung.Beantragt(TERMIN)), 0, KontoLoeschung.Beantragt(TERMIN)),
            // Antwort verloren, Konto aber gelöscht: Der Server weist das Gerät ab.
            Fall("Fehler, Zugang weg: gilt als gelöscht, vergisst einmal", FalscherServer(null, IOException("Antwort verloren"), zugang = true), 1, KontoLoeschung.Geloescht, gefragt = 1),
            Fall("kein Netz, Zugang noch da: vergisst nichts", FalscherServer(null, IOException("kein Netz")), 0, IOException::class.java, gefragt = 1),
            Fall("Serverfehler, Zugang noch da: vergisst nichts", FalscherServer(null, IllegalStateException("gestört")), 0, IllegalStateException::class.java, gefragt = 1),
            Fall(
                "Fehler, Prüfung scheitert: vergisst nichts und meldet den ersten Fehler",
                FalscherServer(null, IOException("kein Netz"), zugang = IllegalStateException("Prüfung gestört")),
                0,
                IOException::class.java,
                gefragt = 1,
            ),
            Fall("Fehler, Server ohne Prüfung (Standard): vergisst nichts", FalscherServer(null, IOException("kein Netz"), zugang = null), 0, IOException::class.java, gefragt = 1),
            Fall("abgebrochen: fragt nicht nach und vergisst nichts", FalscherServer(null, CancellationException("abgebrochen"), zugang = true), 0, CancellationException::class.java),
            Fall("gelöscht: fragt nicht nach", FalscherServer(KontoLoeschung.Geloescht, zugang = IllegalStateException("nicht fragen")), 1, KontoLoeschung.Geloescht),
        )
    }
}
