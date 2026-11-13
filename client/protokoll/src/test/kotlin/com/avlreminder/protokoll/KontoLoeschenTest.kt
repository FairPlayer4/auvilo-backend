package com.avlreminder.protokoll

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.io.IOException
import java.time.Instant

/** „Konto löschen“ (B1): Dieses Gerät vergisst seine Daten nur nach bestätigtem Löschen. */
class KontoLoeschenTest {

    /** Der Server antwortet mit [antwort] oder wirft [fehler]. */
    class FalscherServer(private val antwort: KontoLoeschung?, private val fehler: Exception? = null) : KontoLoeschenAmServer {
        override suspend fun loeschen(): KontoLoeschung = fehler?.let { throw it } ?: antwort!!
        override suspend fun abbrechen() = error("nicht gebraucht")
    }

    data class Fall(val name: String, val server: FalscherServer, val vergessen: Int, val erwartet: Any) {
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
            Fall("kein Netz: vergisst nichts", FalscherServer(null, IOException("kein Netz")), 0, IOException::class.java),
            Fall("Serverfehler: vergisst nichts", FalscherServer(null, IllegalStateException("gestört")), 0, IllegalStateException::class.java),
        )
    }
}
