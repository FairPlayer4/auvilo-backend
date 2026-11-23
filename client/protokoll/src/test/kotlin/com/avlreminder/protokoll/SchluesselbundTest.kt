package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.security.GeneralSecurityException

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

    data class Vergleich(val name: String, val gleich: Boolean, val anderer: (Schluesselbund) -> Schluesselbund) {
        override fun toString() = name
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("vergleiche")
    fun `vergleicht Schlüsselbunde`(fall: Vergleich) {
        val bund = Schluesselbund.erzeugen()
        assertEquals(fall.gleich, bund.gleicheSchluessel(fall.anderer(bund)))
        assertEquals(fall.gleich, fall.anderer(bund).gleicheSchluessel(bund), "in beide Richtungen")
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
        fun vergleiche() = listOf(
            Vergleich("gleich nach lokaler Ablage", true) { Schluesselbund.ausGeheimenBytes(it.geheimeBytes()) },
            Vergleich("gleich nach dem Backup", true) { Schluesselbund.ausBackup(it.backupVerschluesseln(wh, KONTO), wh, KONTO) },
            Vergleich("anders mit anderem Personenschlüssel", false) { Schluesselbund(Personenschluessel.erzeugen(), it.tresor) },
            Vergleich("anders mit anderem Tresor", false) { Schluesselbund(it.person, SymmetrischerSchluessel.erzeugen()) },
            Vergleich("anders mit einer Tresor-Version mehr", false) { Schluesselbund(it.person, it.tresor.neueVersion()) },
        )

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
                val unsinn = wh.backupAead().encrypt(byteArrayOf(10, 3, 1, 2, 3), V2_ZWECK + KONTO.toByteArray())
                Schluesselbund.ausBackup(unsinn, wh, KONTO)
            },
        )

        @JvmStatic
        fun ablehnungenV1() = listOf(
            Ablehnung("lehnt ein v1-Backup mit einem anderen Wiederherstellungsschlüssel ab") { c ->
                Schluesselbund.ausBackup(c, Wiederherstellungsschluessel.erzeugen(), KONTO)
            },
            Ablehnung("lehnt das v1-Backup eines anderen Kontos ab") { c -> Schluesselbund.ausBackup(c, wh, "konto-b") },
            Ablehnung("lehnt ein verändertes v1-Backup ab") { c -> Schluesselbund.ausBackup(c.kippe(c.size / 2), wh, KONTO) },
        )

        @JvmStatic
        fun mischformen() = listOf(
            Mischform("Schlüssel aus v1, zugehörige Daten aus v2") { bund ->
                wh.backupAeadV1().encrypt(bund.geheimeBytes(), V2_ZWECK + KONTO.toByteArray())
            },
            Mischform("Schlüssel aus v2, zugehörige Daten aus v1") { bund ->
                wh.backupAead().encrypt(bund.geheimeBytes(), V1_ZWECK + KONTO.toByteArray())
            },
            Mischform("Schlüssel aus v2, ohne Zweck in den zugehörigen Daten") { bund ->
                wh.backupAead().encrypt(bund.geheimeBytes(), KONTO.toByteArray())
            },
        )

        // Absichtlich ausgeschrieben statt Zweck.BACKUP: Ändert jemand die
        // Präfixe, passen alte Backups nicht mehr, und diese Tests werden rot.
        private val V1_ZWECK = "avlreminder/backup/v1\n".toByteArray()
        private val V2_ZWECK = "avlreminder/backup/v2\n".toByteArray()

        /** Ein Backup, wie es die Version vor Format v2 geschrieben hat. */
        private fun v1Backup(bund: Schluesselbund): ByteArray =
            wh.backupAeadV1().encrypt(bund.geheimeBytes(), V1_ZWECK + KONTO.toByteArray())
    }

    // KRITISCH: Format v2 (SE6). Neue Backups sind nie mehr direkt mit dem
    // Wiederherstellungsschlüssel verschlüsselt, sondern mit dem abgeleiteten
    // Backup-Schlüssel und dem Zweck v2.
    @Test
    fun `schreibt das Backup im Format v2`() {
        val bund = Schluesselbund.erzeugen()
        val chiffrat = bund.backupVerschluesseln(wh, KONTO)

        assertArrayEquals(bund.geheimeBytes(), wh.backupAead().decrypt(chiffrat, V2_ZWECK + KONTO.toByteArray()))
        for (zugehoerig in listOf(V1_ZWECK, V2_ZWECK)) {
            assertThrows<GeneralSecurityException>("nicht mit dem Wiederherstellungsschlüssel direkt") {
                wh.backupAeadV1().decrypt(chiffrat, zugehoerig + KONTO.toByteArray())
            }
        }
    }

    // Erlaubt bis zur Sicherheitsphase: Kevins vorhandenes Backup ist noch v1.
    @Test
    fun `liest ein Backup im alten Format v1`() {
        val bund = Schluesselbund.erzeugen()
        assertTrue(bund.gleicheSchluessel(Schluesselbund.ausBackup(v1Backup(bund), wh, KONTO)))
    }

    // KRITISCH: Für v1 gelten dieselben Ablehnungen wie für v2.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungenV1")
    fun `lehnt v1-Backup ab`(fall: Ablehnung) {
        val chiffrat = v1Backup(Schluesselbund.erzeugen())
        assertThrows<EntschluesselnFehlgeschlagen> { fall.versuch(chiffrat) }
    }

    data class Mischform(val name: String, val verschluesseln: (Schluesselbund) -> ByteArray) {
        override fun toString() = name
    }

    // KRITISCH: Ein Backup geht nur in genau einem Format auf, mit dessen
    // Schlüssel und dessen Zweck. Sonst ließe sich ein Chiffrat in einen
    // anderen Zusammenhang umdeuten.
    @ParameterizedTest(name = "lehnt Mischform ab: {0}")
    @MethodSource("mischformen")
    fun `lehnt Mischformen aus v1 und v2 ab`(fall: Mischform) {
        val chiffrat = fall.verschluesseln(Schluesselbund.erzeugen())
        assertThrows<EntschluesselnFehlgeschlagen> { Schluesselbund.ausBackup(chiffrat, wh, KONTO) }
    }
}
