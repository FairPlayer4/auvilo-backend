package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.time.Instant

class DatensicherungTest {

    @Test
    fun `entschlüsselt Kopf und Nutzlast unverändert`() {
        val chiffrat = Datensicherung.verschluesseln(sicherung, tresor, KONTO)
        val wieder = Datensicherung.entschluesseln(chiffrat, tresor, KONTO)
        assertEquals(sicherung.kopf, wieder.kopf)
        assertArrayEquals(sicherung.nutzlast, wieder.nutzlast)
    }

    @Test
    fun `entschlüsselt mit dem Tresorschlüssel nach einem Schlüsselwechsel`() {
        val chiffrat = Datensicherung.verschluesseln(sicherung, tresor, KONTO)
        assertArrayEquals(sicherung.nutzlast, Datensicherung.entschluesseln(chiffrat, tresor.neueVersion(), KONTO).nutzlast)
    }

    data class Ablehnung(val name: String, val chiffrat: ByteArray, val tresor: SymmetrischerSchluessel, val konto: String) {
        override fun toString() = name
    }

    // KRITISCH: Zu „entschlüsselt“ oben die Ablehnungen. Es gibt nie eine halbe oder
    // fremde Sicherung, immer EntschluesselnFehlgeschlagen.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab`(fall: Ablehnung) {
        assertThrows<EntschluesselnFehlgeschlagen> { Datensicherung.entschluesseln(fall.chiffrat, fall.tresor, fall.konto) }
    }

    companion object {
        private const val KONTO = "konto-a"
        private val tresor = SymmetrischerSchluessel.erzeugen()
        private val sicherung = Sicherung(
            Sicherungskopf(zaehler = 7, erstellt = Instant.ofEpochSecond(1_793_000_000), geraetId = "geraet-1", schemaVersion = 5),
            "Daten".toByteArray(),
        )
        private val chiffrat = Datensicherung.verschluesseln(sicherung, tresor, KONTO)

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt die Sicherung eines anderen Kontos ab", chiffrat, tresor, "konto-b"),
            Ablehnung("lehnt einen fremden Tresorschlüssel ab", chiffrat, SymmetrischerSchluessel.erzeugen(), KONTO),
            Ablehnung("lehnt veränderte Bytes ab", chiffrat.copyOf().also { it[it.size - 1] = (it.last() + 1).toByte() }, tresor, KONTO),
            Ablehnung("lehnt abgeschnittene Bytes ab", chiffrat.copyOf(chiffrat.size - 1), tresor, KONTO),
            // Gleicher Schlüssel, anderer Zweck: Ein anderes Chiffrat des Tresors passt nie als Sicherung.
            Ablehnung(
                "lehnt ein Chiffrat des Tresors für einen anderen Zweck ab",
                tresor.verschluesseln("Daten".toByteArray(), KONTO.toByteArray()),
                tresor,
                KONTO,
            ),
        )
    }
}
