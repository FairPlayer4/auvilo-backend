package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class SchluesselbundTest {

    @Test
    fun `aus dem Backup kommt derselbe Schlüsselbund zurück`() {
        val bund = Schluesselbund.erzeugen()
        val wieder = Schluesselbund.ausBackup(bund.backupVerschluesseln(wh, KONTO), wh, KONTO)

        // Dieselbe Person: gleicher Fingerabdruck, Signaturen gelten, Umschläge gehen auf.
        assertEquals(bund.person.oeffentlich.fingerabdruck(), wieder.person.oeffentlich.fingerabdruck())
        assertTrue(bund.person.oeffentlich.pruefen(Signaturzweck.OPERATION, daten, wieder.person.signieren(Signaturzweck.OPERATION, daten)))
        val umschlag = bund.person.oeffentlich.verpacken(SymmetrischerSchluessel.erzeugen())
        wieder.person.entpacken(umschlag)
        // Derselbe Tresor: liest, was vorher verschlüsselt wurde.
        assertArrayEquals(daten, wieder.tresor.entschluesseln(bund.tresor.verschluesseln(daten, byteArrayOf()), byteArrayOf()))
    }

    @Test
    fun `lokal abgelegter Schlüsselbund kommt gleich zurück`() {
        val bund = Schluesselbund.erzeugen()
        val wieder = Schluesselbund.ausGeheimenBytes(bund.geheimeBytes())
        assertEquals(bund.person.oeffentlich.fingerabdruck(), wieder.person.oeffentlich.fingerabdruck())
        assertArrayEquals(daten, wieder.tresor.entschluesseln(bund.tresor.verschluesseln(daten, byteArrayOf()), byteArrayOf()))
    }

    @Test
    fun `lehnt kaputte lokale Ablage ab`() {
        assertThrows<EntschluesselnFehlgeschlagen> { Schluesselbund.ausGeheimenBytes(byteArrayOf(10, 3, 1, 2, 3)) }
    }

    @Test
    fun `Backup passt in die 16 KiB, die der Server annimmt`() {
        // Mit einigen Tresor-Versionen, wie sie nach Schlüsselwechseln entstehen.
        var tresor = SymmetrischerSchluessel.erzeugen()
        repeat(9) { tresor = tresor.neueVersion() }
        val chiffrat = Schluesselbund(Personenschluessel.erzeugen(), tresor).backupVerschluesseln(wh, KONTO)
        assertTrue(chiffrat.size < 16 * 1024, "Backup hat ${chiffrat.size} Bytes")
    }

    data class Ablehnung(val name: String, val versuch: (chiffrat: ByteArray) -> Unit) {
        override fun toString() = name
    }

    // KRITISCH: Ein Backup geht nur mit dem richtigen Schlüssel, für das richtige
    // Konto und unverändert auf – sonst ein klarer Fehler, nie ein halber Bund.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt Backup ab`(fall: Ablehnung) {
        val chiffrat = Schluesselbund.erzeugen().backupVerschluesseln(wh, KONTO)
        assertThrows<EntschluesselnFehlgeschlagen> { fall.versuch(chiffrat) }
    }

    companion object {
        private const val KONTO = "konto-a"
        private val wh = Wiederherstellungsschluessel.erzeugen()
        private val daten = "Einkaufsliste".toByteArray()

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt einen anderen Wiederherstellungsschlüssel ab") { c ->
                Schluesselbund.ausBackup(c, Wiederherstellungsschluessel.erzeugen(), KONTO)
            },
            Ablehnung("lehnt das Backup eines anderen Kontos ab (vom Server vertauscht)") { c ->
                Schluesselbund.ausBackup(c, wh, "konto-b")
            },
            Ablehnung("lehnt eine Konto-ID ab, die nur mit dem Präfix übereinstimmt") { c ->
                Schluesselbund.ausBackup(c, wh, "konto-a2")
            },
            Ablehnung("lehnt ein verändertes Backup ab") { c -> Schluesselbund.ausBackup(c.kippe(c.size / 2), wh, KONTO) },
            Ablehnung("lehnt ein abgeschnittenes Backup ab") { c -> Schluesselbund.ausBackup(c.copyOf(c.size - 1), wh, KONTO) },
            Ablehnung("lehnt ein leeres Backup ab") { _ -> Schluesselbund.ausBackup(byteArrayOf(), wh, KONTO) },
            Ablehnung("lehnt ein richtig verschlüsseltes Backup mit unsinnigem Inhalt ab") { _ ->
                // Simuliert einen fehlerhaften Client: Verschlüsselung stimmt, Inhalt nicht.
                val unsinn = wh.aead().encrypt(byteArrayOf(10, 3, 1, 2, 3), (Zweck.BACKUP + KONTO).toByteArray())
                Schluesselbund.ausBackup(unsinn, wh, KONTO)
            },
        )
    }
}
