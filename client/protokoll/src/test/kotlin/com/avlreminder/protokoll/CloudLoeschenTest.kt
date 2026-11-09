package com.avlreminder.protokoll

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.io.IOException

/** „Cloud-Daten löschen“ sperrt alle Geräte des Kontos (Kap. 11, 2026-11-09). */
class CloudLoeschenTest {

    /**
     * Der Server mit seinen Regeln: Nur ein freigeschaltetes Gerät darf löschen und andere
     * sperren; sich selbst sperren darf jedes. [abbruchBei] lässt einen Aufruf scheitern
     * wie bei fehlendem Netz.
     */
    class FalschesKonto(
        val geraete: MutableList<KontoGeraet>,
        private val freigeschaltet: Boolean = true,
        var abbruchBei: String? = null,
    ) : KontoAufDemServer {
        var datensicherung = true
        var schluesselBackup = true
        val aufrufe = mutableListOf<String>()

        private fun aufruf(name: String) {
            if (name == abbruchBei) throw IOException("kein Netz")
            aufrufe += name
        }

        override suspend fun geraete(): List<KontoGeraet> = geraete.toList()

        override suspend fun sperren(geraetId: String) {
            val i = geraete.indexOfFirst { it.geraetId == geraetId }
            if (!geraete[i].diesesGeraet && !freigeschaltet) throw GeraetNichtFreigeschaltet()
            aufruf("sperren $geraetId")
            geraete[i] = geraete[i].copy(gesperrt = true)
        }

        override suspend fun datensicherungLoeschen() {
            if (!freigeschaltet) throw GeraetNichtFreigeschaltet()
            aufruf("datensicherung")
            datensicherung = false
        }

        override suspend fun schluesselBackupLoeschen() {
            if (!freigeschaltet) throw GeraetNichtFreigeschaltet()
            aufruf("schluessel")
            schluesselBackup = false
        }
    }

    data class Fall(val name: String, val geraete: List<KontoGeraet>, val aufrufe: List<String>, val gesperrt: Int) {
        override fun toString() = name
    }

    // KRITISCH: Danach ist jedes Gerät gesperrt, die anderen vor dem Löschen, dieses zuletzt.
    @ParameterizedTest(name = "{0}")
    @MethodSource("faelle")
    fun `löscht beide Backups und sperrt alle Geräte`(fall: Fall) {
        val konto = FalschesKonto(fall.geraete.toMutableList())

        assertEquals(fall.gesperrt, runBlocking { CloudLoeschen.alles(konto) })

        assertEquals(fall.aufrufe, konto.aufrufe)
        assertTrue(konto.geraete.all { it.gesperrt })
        assertEquals(false, konto.datensicherung)
        assertEquals(false, konto.schluesselBackup)
    }

    data class Ablehnung(
        val name: String,
        val freigeschaltet: Boolean = true,
        val geraete: List<KontoGeraet> = listOf(DIESES, ANDERES),
        val abbruchBei: String? = null,
        val erwartet: Class<out Throwable>,
        val aufrufe: List<String> = emptyList(),
    ) {
        override fun toString() = name
    }

    // KRITISCH: Scheitert ein Schritt, bleibt dieses Gerät entsperrt, damit es den Ablauf
    // wiederholen kann, und es wird nichts gelöscht, solange ein anderes Gerät offen ist.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `bricht ab, ohne dieses Gerät zu sperren`(fall: Ablehnung) {
        val konto = FalschesKonto(fall.geraete.toMutableList(), fall.freigeschaltet, fall.abbruchBei)

        val e = runCatching { runBlocking { CloudLoeschen.alles(konto) } }.exceptionOrNull()

        assertInstanceOf(fall.erwartet, e)
        assertEquals(fall.aufrufe, konto.aufrufe)
        assertTrue(konto.geraete.none { it.diesesGeraet && it.gesperrt })
    }

    // KRITISCH: Nach einem Abbruch (Netz) lässt sich der Ablauf wiederholen und endet wie
    // ohne Abbruch.
    @Test
    fun `nach einem Abbruch gelingt die Wiederholung`() {
        val konto = FalschesKonto(mutableListOf(DIESES, ANDERES, DRITTES), abbruchBei = "schluessel")
        assertInstanceOf(IOException::class.java, runCatching { runBlocking { CloudLoeschen.alles(konto) } }.exceptionOrNull())

        konto.abbruchBei = null
        assertEquals(0, runBlocking { CloudLoeschen.alles(konto) })

        assertTrue(konto.geraete.all { it.gesperrt })
        assertEquals(false, konto.schluesselBackup)
    }

    companion object {
        val DIESES = KontoGeraet("dieses", gesperrt = false, diesesGeraet = true)
        val ANDERES = KontoGeraet("anderes", gesperrt = false, diesesGeraet = false)
        val DRITTES = KontoGeraet("drittes", gesperrt = false, diesesGeraet = false)
        val GESPERRTES = KontoGeraet("gesperrtes", gesperrt = true, diesesGeraet = false)

        @JvmStatic
        fun faelle() = listOf(
            Fall(
                "zwei andere Geräte: erst sie, dieses zuletzt",
                listOf(ANDERES, DIESES, DRITTES),
                listOf("sperren anderes", "sperren drittes", "datensicherung", "schluessel", "sperren dieses"),
                gesperrt = 2,
            ),
            Fall(
                "ein gesperrtes Gerät wird übersprungen",
                listOf(DIESES, GESPERRTES, ANDERES),
                listOf("sperren anderes", "datensicherung", "schluessel", "sperren dieses"),
                gesperrt = 1,
            ),
            Fall(
                "nur dieses Gerät",
                listOf(DIESES),
                listOf("datensicherung", "schluessel", "sperren dieses"),
                gesperrt = 0,
            ),
        )

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung(
                "nicht freigeschaltet: darf andere nicht sperren",
                freigeschaltet = false,
                erwartet = GeraetNichtFreigeschaltet::class.java,
            ),
            Ablehnung(
                "nicht freigeschaltet und allein: darf nicht löschen",
                freigeschaltet = false,
                geraete = listOf(DIESES),
                erwartet = GeraetNichtFreigeschaltet::class.java,
            ),
            Ablehnung(
                "Abbruch beim Sperren eines anderen Geräts: nichts gelöscht",
                geraete = listOf(DIESES, ANDERES, DRITTES),
                abbruchBei = "sperren drittes",
                erwartet = IOException::class.java,
                aufrufe = listOf("sperren anderes"),
            ),
            Ablehnung(
                "Abbruch beim Löschen des Schlüssel-Backups",
                abbruchBei = "schluessel",
                erwartet = IOException::class.java,
                aufrufe = listOf("sperren anderes", "datensicherung"),
            ),
            Ablehnung(
                "Server listet dieses Gerät nicht: nichts gesperrt, nichts gelöscht",
                geraete = listOf(ANDERES, DRITTES),
                erwartet = DiesesGeraetFehlt::class.java,
            ),
        )
    }
}
