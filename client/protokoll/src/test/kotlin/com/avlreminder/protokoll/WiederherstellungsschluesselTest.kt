package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class WiederherstellungsschluesselTest {

    // Fester Schlüssel 00 01 02 … 1F, damit die Fälle wiederholbar sind.
    private val fest = Wiederherstellungsschluessel.ausBytes(ByteArray(32) { it.toByte() })
    private val text = fest.alsText()

    @Test
    fun `Text hat 14 Vierergruppen aus Crockford-Base32`() {
        assertTrue(Regex("([0-9A-HJKMNP-TV-Z]{4}-){13}[0-9A-HJKMNP-TV-Z]{4}").matches(text), text)
    }

    // KRITISCH: Gedruckte Schlüssel liegen jahrelang auf Papier. Ändert sich
    // Alphabet, Bitreihenfolge oder Prüfzeichen-Berechnung, wird dieser Test rot.
    // Erwartung unabhängig berechnet: Base32 nach RFC 4648 (Python base64.b32encode),
    // Zeichen auf Crockford umgesetzt; Prüfzeichen = erste 4 Zeichen von SHA-256(Schlüssel).
    @Test
    fun `fester Schlüssel ergibt immer denselben Text`() {
        assertEquals("000G-40R4-0M30-E209-185G-R38E-1W81-24GK-2GAH-C5RR-34D1-P70X-3RFG-CC6W", text)
    }

    @Test
    fun `neue Schlüssel sind verschieden`() {
        assertNotEquals(Wiederherstellungsschluessel.erzeugen().alsText(), Wiederherstellungsschluessel.erzeugen().alsText())
    }

    @Test
    fun `ein anderer Schlüssel entschlüsselt nicht`() {
        val chiffrat = fest.backupAead().encrypt(byteArrayOf(1), byteArrayOf())
        assertThrows<java.security.GeneralSecurityException> {
            Wiederherstellungsschluessel.erzeugen().backupAead().decrypt(chiffrat, byteArrayOf())
        }
    }

    @Test
    fun `toString verrät den Schlüssel nicht`() {
        assertEquals("Wiederherstellungsschluessel(***)", fest.toString())
    }

    // Erlaubt: Schreibweisen, die Menschen beim Abtippen typischerweise wählen.
    data class Schreibweise(val name: String, val eingabe: (String) -> String) {
        override fun toString() = name
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("erlaubteSchreibweisen")
    fun `nimmt Schlüssel an`(fall: Schreibweise) {
        val gelesen = Wiederherstellungsschluessel.ausText(fall.eingabe(text))
        assertEquals(text, gelesen.alsText())
        // Derselbe Schlüssel heißt: Er entschlüsselt, was der Originalschlüssel verschlüsselt hat.
        val chiffrat = fest.backupAead().encrypt(byteArrayOf(1, 2, 3), byteArrayOf())
        assertArrayEquals(byteArrayOf(1, 2, 3), gelesen.backupAead().decrypt(chiffrat, byteArrayOf()))
    }

    // Abgelehnt: Alles, was nicht genau dieser Schlüssel ist.
    data class Ablehnung(val name: String, val eingabe: (String) -> String) {
        override fun toString() = name
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab`(fall: Ablehnung) {
        assertThrows<UngueltigerWiederherstellungsschluessel> { Wiederherstellungsschluessel.ausText(fall.eingabe(text)) }
    }

    companion object {
        @JvmStatic
        fun erlaubteSchreibweisen() = listOf(
            Schreibweise("nimmt den gedruckten Text an") { it },
            Schreibweise("nimmt Kleinbuchstaben an") { it.lowercase() },
            Schreibweise("nimmt Text ohne Bindestriche an") { it.replace("-", "") },
            Schreibweise("nimmt Leerzeichen statt Bindestrichen an") { it.replace("-", " ") },
            Schreibweise("nimmt Zeilenumbrüche und Ränder an") { "  " + it.replace("-", "\n") + "\n" },
            Schreibweise("nimmt O statt 0 an") { it.replace('0', 'O') },
            Schreibweise("nimmt I und L statt 1 an") { t ->
                var n = 0
                t.map { if (it == '1') (if (n++ % 2 == 0) 'I' else 'l') else it }.joinToString("")
            },
        )

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt leeren Text ab") { "" },
            Ablehnung("lehnt Text mit einem Zeichen zu wenig ab") { it.dropLast(1) },
            Ablehnung("lehnt Text mit einem Zeichen zu viel ab") { it + "0" },
            Ablehnung("lehnt Text ohne Prüfzeichen ab") { it.dropLast(5) },
            Ablehnung("lehnt Tippfehler im Schlüssel ab (Prüfzeichen passen nicht)") { ersetze(it, 0) },
            Ablehnung("lehnt Tippfehler in den Prüfzeichen ab") { ersetze(it, 55) }, // letztes der 56 Zeichen
            Ablehnung("lehnt vertauschte Nachbarzeichen ab") { vertauscheNachbarn(it) },
            Ablehnung("lehnt U ab (nicht im Alphabet)") { "U" + it.drop(1) },
            Ablehnung("lehnt Sonderzeichen ab") { "*" + it.drop(1) },
            // Das letzte Schlüsselzeichen trägt 1 Bit Schlüssel und 4 Füllbits, die 0 sein müssen.
            Ablehnung("lehnt gesetzte Füllbits ab") { t ->
                val roh = t.replace("-", "")
                val letztes = roh[51]
                val wert = "0123456789ABCDEFGHJKMNPQRSTVWXYZ".indexOf(letztes) or 1
                roh.substring(0, 51) + "0123456789ABCDEFGHJKMNPQRSTVWXYZ"[wert] + roh.substring(52)
            },
        )

        private fun ersetze(text: String, index: Int): String {
            val roh = text.replace("-", "")
            val c = roh[index]
            return roh.substring(0, index) + (if (c == 'X') 'Y' else 'X') + roh.substring(index + 1)
        }

        private fun vertauscheNachbarn(text: String): String {
            val roh = text.replace("-", "").toCharArray()
            val i = (0 until roh.size - 1).first { roh[it] != roh[it + 1] }
            roh[i] = roh[i + 1].also { roh[i + 1] = roh[i] }
            return String(roh)
        }
    }
}
