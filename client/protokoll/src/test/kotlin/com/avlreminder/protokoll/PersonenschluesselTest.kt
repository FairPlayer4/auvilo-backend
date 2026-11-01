package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class PersonenschluesselTest {

    // --- Signieren ---------------------------------------------------------

    @Test
    fun `Signatur gilt für Zweck und Daten, mit denen sie erstellt wurde`() {
        val anna = Personenschluessel.erzeugen()
        val sig = anna.signieren(Signaturzweck.OPERATION, daten)
        assertTrue(anna.oeffentlich.pruefen(Signaturzweck.OPERATION, daten, sig))
    }

    data class Pruefung(val name: String, val pruefe: (anna: Personenschluessel, sig: ByteArray) -> Boolean) {
        override fun toString() = name
    }

    // KRITISCH: Zu jeder gültigen Signatur die Fälle, in denen sie nicht gelten darf.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ungueltigeSignaturen")
    fun `lehnt Signatur ab`(fall: Pruefung) {
        val anna = Personenschluessel.erzeugen()
        val sig = anna.signieren(Signaturzweck.OPERATION, daten)
        assertFalse(fall.pruefe(anna, sig))
    }

    // --- Verpacken (Umschlag) ----------------------------------------------

    @Test
    fun `Empfänger entpackt einen für ihn verpackten Schlüssel`() {
        val ben = Personenschluessel.erzeugen()
        val freigabe = SymmetrischerSchluessel.erzeugen()
        val ausgepackt = ben.entpacken(ben.oeffentlich.verpacken(freigabe))
        // Gleicher Schlüssel = entschlüsselt, was der Original-Schlüssel verschlüsselt hat.
        val chiffrat = freigabe.verschluesseln(daten, byteArrayOf())
        assertArrayEquals(daten, ausgepackt.entschluesseln(chiffrat, byteArrayOf()))
    }

    data class Entpacken(val name: String, val versuch: (ben: Personenschluessel, umschlag: ByteArray) -> Unit) {
        override fun toString() = name
    }

    // KRITISCH: Nur der Empfänger kann entpacken, und nur unveränderte Umschläge.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ungueltigesEntpacken")
    fun `lehnt Entpacken ab`(fall: Entpacken) {
        val ben = Personenschluessel.erzeugen()
        val umschlag = ben.oeffentlich.verpacken(SymmetrischerSchluessel.erzeugen())
        assertThrows<EntschluesselnFehlgeschlagen> { fall.versuch(ben, umschlag) }
    }

    @Test
    fun `Umschlag passt nicht als Chiffrat eines anderen Zwecks`() {
        // Gegenprobe zum Zweck-Präfix: Wer denselben Schlüssel für etwas anderes
        // verwendet, kann Umschläge nicht als solche Daten ausgeben.
        val ben = Personenschluessel.erzeugen()
        val umschlag = ben.oeffentlich.verpacken(SymmetrischerSchluessel.erzeugen())
        assertFalse(ben.oeffentlich.pruefen(Signaturzweck.OPERATION, umschlag, umschlag))
    }

    // --- Fingerabdruck -----------------------------------------------------

    @Test
    fun `Fingerabdruck hat 7 Gruppen und ist für dieselbe Person gleich`() {
        val anna = Personenschluessel.erzeugen()
        val fp = anna.oeffentlich.fingerabdruck()
        assertTrue(Regex("([0-9A-HJKMNP-TV-Z]{4}-){6}[0-9A-HJKMNP-TV-Z]{2}").matches(fp), fp)
        assertEquals(fp, anna.oeffentlich.fingerabdruck())
    }

    @Test
    fun `verschiedene Personen haben verschiedene Fingerabdrücke`() {
        assertNotEquals(Personenschluessel.erzeugen().oeffentlich.fingerabdruck(), Personenschluessel.erzeugen().oeffentlich.fingerabdruck())
    }

    companion object {
        private val daten = "Punkt 3 abgehakt".toByteArray()

        @JvmStatic
        fun ungueltigeSignaturen() = listOf(
            Pruefung("lehnt Signatur für einen anderen Zweck ab") { a, sig ->
                a.oeffentlich.pruefen(Signaturzweck.KONTAKT, daten, sig)
            },
            Pruefung("lehnt veränderte Daten ab") { a, sig ->
                a.oeffentlich.pruefen(Signaturzweck.OPERATION, "Punkt 4 abgehakt".toByteArray(), sig)
            },
            Pruefung("lehnt angehängte Daten ab") { a, sig ->
                a.oeffentlich.pruefen(Signaturzweck.OPERATION, daten + 0, sig)
            },
            Pruefung("lehnt veränderte Signatur ab") { a, sig ->
                a.oeffentlich.pruefen(Signaturzweck.OPERATION, daten, sig.kippe(sig.size - 1))
            },
            Pruefung("lehnt leere Signatur ab") { a, _ -> a.oeffentlich.pruefen(Signaturzweck.OPERATION, daten, byteArrayOf()) },
            Pruefung("lehnt Signatur einer anderen Person ab") { _, sig ->
                Personenschluessel.erzeugen().oeffentlich.pruefen(Signaturzweck.OPERATION, daten, sig)
            },
            Pruefung("lehnt Signatur ab, die mit dem Zweck-Präfix in den Daten erschlichen ist") { a, _ ->
                // Wer KONTAKT-Daten mit vorangestelltem OPERATION-Präfix signieren lässt,
                // bekommt keine gültige OPERATION-Signatur für die restlichen Bytes.
                val sig = a.signieren(Signaturzweck.KONTAKT, Signaturzweck.OPERATION.praefix.toByteArray() + daten)
                a.oeffentlich.pruefen(Signaturzweck.OPERATION, daten, sig)
            },
        )

        @JvmStatic
        fun ungueltigesEntpacken() = listOf(
            Entpacken("lehnt Umschlag für eine andere Person ab") { _, u -> Personenschluessel.erzeugen().entpacken(u) },
            Entpacken("lehnt veränderten Umschlag ab") { b, u -> b.entpacken(u.kippe(u.size / 2)) },
            Entpacken("lehnt abgeschnittenen Umschlag ab") { b, u -> b.entpacken(u.copyOf(u.size - 1)) },
            Entpacken("lehnt leeren Umschlag ab") { b, _ -> b.entpacken(byteArrayOf()) },
            Entpacken("lehnt ein Chiffrat statt eines Umschlags ab") { b, _ ->
                b.entpacken(SymmetrischerSchluessel.erzeugen().verschluesseln(daten, byteArrayOf()))
            },
        )
    }
}
