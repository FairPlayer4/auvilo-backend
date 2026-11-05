package com.avlreminder.testclient

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.io.TempDir
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.io.File

class AblageTest {

    @TempDir
    lateinit var ordner: File

    data class Fall(val name: String, val datei: String, val lesen: (Ablage) -> Any?) {
        override fun toString() = name
    }

    // KRITISCH: Was im Terminal landet, geht in den Gesprächsverlauf (CLAUDE.md).
    // Eine kaputte oder fremde Datei darf ihren Inhalt (Token, Schlüssel) nicht
    // über die Fehlermeldung oder eine Ursache mit Stacktrace verraten.
    @ParameterizedTest(name = "{0}")
    @MethodSource("faelle")
    fun `kaputte Datei meldet sich ohne ihren Inhalt`(fall: Fall) {
        // Ein unbekanntes Feld (andere Version) und ein Wert, der nicht passt.
        File(ordner, fall.datei).writeText("""{"kontoId":"k","neuesFeld":"$GEHEIM","token":"$GEHEIM","zaehler":"$GEHEIM"}""")
        val e = assertThrows(Bedienfehler::class.java) { fall.lesen(Ablage(ordner)) }
        assertFalse(e.message!!.contains(GEHEIM), "Meldung zitiert die Datei: ${e.message}")
        assertNull(e.cause, "Ursache würde mit Stacktrace ausgegeben")
        assertTrue(e.message!!.contains(fall.datei), "Meldung nennt die Datei nicht: ${e.message}")
    }

    // Gegenprobe: Eine gültige Datei liest die Ablage weiter.
    @Test
    fun `gültige Sitzung wird gelesen`() {
        val ablage = Ablage(ordner)
        val sitzung = GespeicherteSitzung("k", "g", "t", 1_800_000_000)
        ablage.speichereSitzung("handy", sitzung)
        assertEquals(sitzung, ablage.sitzung("handy"))
    }

    companion object {
        private const val GEHEIM = "GEHEIMES-TOKEN-123"

        @JvmStatic
        fun faelle() = listOf(
            Fall("Sitzung", "sitzung-handy.json") { it.sitzung("handy") },
            Fall("Schlüssel", "schluessel-handy.json") { it.schluessel("handy", "k") },
            Fall("Sicherungsstand", "stand-handy.json") { it.sicherungsStand("handy", "k").laden() },
        )
    }
}
