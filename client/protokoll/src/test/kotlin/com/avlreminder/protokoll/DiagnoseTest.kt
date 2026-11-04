package com.avlreminder.protokoll

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.util.zip.GZIPInputStream
import kotlin.random.Random

class DiagnoseTest {

    /** Merkt sich, was gesendet wurde. */
    private class Mitschnitt : DiagnoseAblage {
        val gesendet = mutableListOf<ByteArray>()

        override suspend fun senden(nutzlast: ByteArray) {
            gesendet += nutzlast
        }
    }

    private fun entpacken(b: ByteArray) = GZIPInputStream(b.inputStream()).readBytes().toString(Charsets.UTF_8)

    @Test
    fun `packt JSON-Zeilen mit gzip, je Zeile ein Objekt`() {
        val zeilen = listOf("""{"art":"kopf","format":"${Diagnose.FORMAT}"}""", """{"ereignis":"ä_ß"}""")
        val gepackt = Diagnose.packen(zeilen)
        // Der Server prüft nur diese drei Bytes (gzip, deflate).
        assertArrayEquals(byteArrayOf(0x1f, 0x8b.toByte(), 0x08), gepackt.copyOf(3))
        assertEquals(zeilen.joinToString("\n", postfix = "\n"), entpacken(gepackt))
    }

    @Test
    fun `sendet die gepackte Diagnose und nennt ihre Größe`() {
        val server = Mitschnitt()
        val zeilen = listOf("""{"art":"kopf"}""")
        val groesse = runBlocking { Diagnose.senden(server, zeilen) }
        val gesendet = server.gesendet.single()
        assertEquals(gesendet.size, groesse)
        assertEquals("{\"art\":\"kopf\"}\n", entpacken(gesendet))
    }

    @Test
    fun `sendet eine große Diagnose, die gepackt klein genug ist`() {
        // 2 MiB gleiche Zeilen packen auf wenige KiB: Maßgeblich ist die gepackte Größe.
        val zeilen = List(20_000) { """{"ereignis":"wiederholt","felder":{"n":1},"auffuellen":"${"x".repeat(80)}"}""" }
        val server = Mitschnitt()
        runBlocking { Diagnose.senden(server, zeilen) }
        assertTrue(server.gesendet.single().size < 64 * 1024)
    }

    @Test
    fun `lehnt gepackt zu große Diagnose ab und sendet nichts`() {
        // Zufall in Hex packt gzip nur etwa auf die Hälfte: 700 KiB Hex bleiben gepackt
        // über 256 KiB.
        val zufall = Random(1).nextBytes(700 * 1024 / 2).joinToString("") { "%02x".format(it) }
        val zeilen = zufall.chunked(1000).map { """{"ereignis":"x","rest":"$it"}""" }
        val server = Mitschnitt()
        assertThrows<DiagnoseZuGross> { runBlocking { Diagnose.senden(server, zeilen) } }
        assertTrue(server.gesendet.isEmpty())
    }

    data class Ablehnung(val name: String, val zeilen: List<String>) {
        override fun toString() = name
    }

    // KRITISCH: Zum Packen oben die Ablehnungen; abgelehnt wird nie gesendet.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab und sendet nichts`(fall: Ablehnung) {
        val server = Mitschnitt()
        assertThrows<IllegalArgumentException> { runBlocking { Diagnose.senden(server, fall.zeilen) } }
        assertTrue(server.gesendet.isEmpty())
    }

    companion object {
        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt Diagnose ohne Zeilen ab", emptyList()),
            Ablehnung("lehnt Zeile mit Zeilenumbruch ab", listOf("""{"art":"kopf"}""", "{\"a\":1}\n{\"b\":2}")),
            Ablehnung("lehnt Zeile mit Wagenrücklauf ab", listOf("{\"art\":\"kopf\"}\r")),
        )
    }
}
