package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class SymmetrischerSchluesselTest {

    private val klartext = "Brunnen gemacht".toByteArray()
    private val zugehoerig = "freigabe-1".toByteArray()

    @Test
    fun `entschlüsselt, was derselbe Schlüssel verschlüsselt hat`() {
        val s = SymmetrischerSchluessel.erzeugen()
        assertArrayEquals(klartext, s.entschluesseln(s.verschluesseln(klartext, zugehoerig), zugehoerig))
    }

    @Test
    fun `gleicher Klartext ergibt jedes Mal ein anderes Chiffrat (zufällige Nonce)`() {
        val s = SymmetrischerSchluessel.erzeugen()
        assertFalse(s.verschluesseln(klartext, zugehoerig).contentEquals(s.verschluesseln(klartext, zugehoerig)))
    }

    @Test
    fun `Schlüssel übersteht die Speicherung als Bytes`() {
        val s = SymmetrischerSchluessel.erzeugen()
        val wieder = SymmetrischerSchluessel.ausGeheimenBytes(s.geheimeBytes())
        assertArrayEquals(klartext, wieder.entschluesseln(s.verschluesseln(klartext, zugehoerig), zugehoerig))
    }

    @Test
    fun `nach neuer Version bleiben alte Chiffrate lesbar`() {
        val alt = SymmetrischerSchluessel.erzeugen()
        val altesChiffrat = alt.verschluesseln(klartext, zugehoerig)
        val neu = alt.neueVersion()
        assertEquals(2, neu.versionen)
        assertArrayEquals(klartext, neu.entschluesseln(altesChiffrat, zugehoerig))
    }

    @Test
    fun `nach neuer Version kann der alte Schlüssel Neues nicht lesen`() {
        val alt = SymmetrischerSchluessel.erzeugen()
        val neu = alt.neueVersion()
        assertThrows<EntschluesselnFehlgeschlagen> { alt.entschluesseln(neu.verschluesseln(klartext, zugehoerig), zugehoerig) }
    }

    data class Ablehnung(val name: String, val versuch: (SymmetrischerSchluessel, ByteArray) -> ByteArray) {
        override fun toString() = name
    }

    // KRITISCH: Jede Abweichung muss scheitern – nie stillschweigend falsche Daten.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab`(fall: Ablehnung) {
        val s = SymmetrischerSchluessel.erzeugen()
        val chiffrat = s.verschluesseln(klartext, zugehoerig)
        assertThrows<EntschluesselnFehlgeschlagen> { fall.versuch(s, chiffrat) }
    }

    companion object {
        private val zugehoerig = "freigabe-1".toByteArray()

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt andere zugehörige Daten ab (Chiffrat einer anderen Freigabe)") { s, c ->
                s.entschluesseln(c, "freigabe-2".toByteArray())
            },
            Ablehnung("lehnt einen anderen Schlüssel ab") { _, c ->
                SymmetrischerSchluessel.erzeugen().entschluesseln(c, zugehoerig)
            },
            Ablehnung("lehnt ein verändertes Byte am Anfang ab (Schlüssel-ID)") { s, c ->
                s.entschluesseln(c.kippe(0), zugehoerig)
            },
            Ablehnung("lehnt ein verändertes Byte in der Mitte ab") { s, c -> s.entschluesseln(c.kippe(c.size / 2), zugehoerig) },
            Ablehnung("lehnt ein verändertes letztes Byte ab (Prüfsumme)") { s, c ->
                s.entschluesseln(c.kippe(c.size - 1), zugehoerig)
            },
            Ablehnung("lehnt ein abgeschnittenes Chiffrat ab") { s, c -> s.entschluesseln(c.copyOf(c.size - 1), zugehoerig) },
            Ablehnung("lehnt ein leeres Chiffrat ab") { s, _ -> s.entschluesseln(byteArrayOf(), zugehoerig) },
        )
    }
}

/** Kopie mit einem gekippten Bit an [index]. */
internal fun ByteArray.kippe(index: Int): ByteArray = copyOf().also { it[index] = (it[index].toInt() xor 1).toByte() }
