package com.avlreminder.testclient

import com.avlreminder.protokoll.Schluesselbund
import com.avlreminder.protokoll.Wiederherstellungsschluessel
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class EinrichtungTest {

    /** Server-Ablage im Speicher, mit derselben Versionsprüfung wie der Server. */
    class FalscheAblage(var backup: GeholtesBackup? = null) : BackupAblage {
        var gespeichert = 0

        /** Simuliert ein anderes Gerät, das zwischen Holen und Speichern schneller ist. */
        var vorDemSpeichern: (() -> Unit)? = null

        override suspend fun holen() = backup

        override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long {
            vorDemSpeichern?.invoke()?.also { vorDemSpeichern = null }
            if ((backup?.version ?: 0L) != erwartet) throw VersionVeraltet()
            gespeichert++
            return (erwartet + 1).also { backup = GeholtesBackup(chiffrat, it) }
        }
    }

    private fun einrichtung(ablage: BackupAblage, vararg eingaben: String?): Einrichtung {
        val rest = eingaben.toMutableList()
        return Einrichtung(KONTO, ablage, frageSchluessel = { rest.removeFirst() }, meldung = {})
    }

    @Test
    fun `ohne Backup erzeugt das Gerät die Schlüssel und lädt das Backup hoch`() = runBlocking {
        val ablage = FalscheAblage()
        val ergebnis = einrichtung(ablage).ausfuehren()

        val neu = assertInstanceOf(Eingerichtet.Neu::class.java, ergebnis)
        assertEquals(1, ablage.gespeichert)
        // Das hochgeladene Backup öffnet sich mit dem angezeigten Schlüssel und enthält denselben Bund.
        val wieder = Schluesselbund.ausBackup(ablage.backup!!.chiffrat, neu.wiederherstellung, KONTO)
        assertEquals(neu.bund.person.oeffentlich.fingerabdruck(), wieder.person.oeffentlich.fingerabdruck())
    }

    @Test
    fun `mit Backup holt das Gerät die Schlüssel und lädt nichts hoch`() = runBlocking {
        val ablage = FalscheAblage(vorhanden.backup)
        val ergebnis = einrichtung(ablage, vorhanden.text).ausfuehren()

        assertInstanceOf(Eingerichtet.AusBackup::class.java, ergebnis)
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
        assertEquals(0, ablage.gespeichert)
    }

    @Test
    fun `nimmt den richtigen Schlüssel nach einem Tippfehler an`() = runBlocking {
        val ablage = FalscheAblage(vorhanden.backup)
        val ergebnis = einrichtung(ablage, "AAAA-BBBB", vorhanden.text.lowercase()).ausfuehren()
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
    }

    // Gegenstück zu „lehnt nach drei Tippfehlern ab“: Der dritte Versuch zählt noch.
    @Test
    fun `nimmt den richtigen Schlüssel im dritten Versuch an`() = runBlocking {
        val ablage = FalscheAblage(vorhanden.backup)
        val ergebnis = einrichtung(ablage, "falsch", fremdes.text, vorhanden.text).ausfuehren()
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
    }

    // KRITISCH: Ein anderes Gerät war zwischen Holen und Speichern schneller.
    // Sein Backup bleibt, und dieses Gerät übernimmt dessen Schlüssel.
    @Test
    fun `überschreibt kein Backup, das ein anderes Gerät gerade angelegt hat`() = runBlocking {
        val ablage = FalscheAblage()
        ablage.vorDemSpeichern = { ablage.backup = vorhanden.backup }
        val ergebnis = einrichtung(ablage, vorhanden.text).ausfuehren()

        assertInstanceOf(Eingerichtet.AusBackup::class.java, ergebnis)
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
        assertEquals(vorhanden.backup, ablage.backup)
    }

    data class Ablehnung(val name: String, val backup: GeholtesBackup, val eingaben: List<String?>) {
        override fun toString() = name
    }

    // KRITISCH: Zu jeder Annahme oben die Ablehnung. Danach gibt es keinen
    // Schlüsselbund, und am Backup auf dem Server ändert sich nichts.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab`(fall: Ablehnung) {
        val ablage = FalscheAblage(fall.backup)
        assertThrows<EinrichtungAbgebrochen> { runBlocking { einrichtung(ablage, *fall.eingaben.toTypedArray()).ausfuehren() } }
        assertEquals(0, ablage.gespeichert)
        assertEquals(fall.backup, ablage.backup)
    }

    @Test
    fun `fragt nach dem Abbruch nicht weiter`() {
        val ablage = FalscheAblage(vorhanden.backup)
        val eingaben = mutableListOf<String?>(null, vorhanden.text)
        val e = Einrichtung(KONTO, ablage, frageSchluessel = { eingaben.removeFirst() }, meldung = {})
        assertThrows<EinrichtungAbgebrochen> { runBlocking { e.ausfuehren() } }
        assertEquals(listOf(vorhanden.text), eingaben)
    }

    /** Ein Konto, dessen Schlüssel schon ein anderes Gerät angelegt hat. */
    class Vorhanden(kontoId: String) {
        private val bund = Schluesselbund.erzeugen()
        private val schluessel = Wiederherstellungsschluessel.erzeugen()
        val text = schluessel.alsText()
        val fingerabdruck = bund.person.oeffentlich.fingerabdruck()
        val backup = GeholtesBackup(bund.backupVerschluesseln(schluessel, kontoId), 1)
    }

    companion object {
        private const val KONTO = "konto-a"
        private val vorhanden = Vorhanden(KONTO)
        private val fremdes = Vorhanden("konto-b")

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt nach drei Tippfehlern ab", vorhanden.backup, listOf("falsch", "auch falsch", "immer noch falsch")),
            Ablehnung(
                "lehnt den Wiederherstellungsschlüssel eines anderen Kontos ab",
                vorhanden.backup,
                List(Einrichtung.VERSUCHE) { fremdes.text },
            ),
            Ablehnung(
                "lehnt ein vom Server untergeschobenes Backup eines anderen Kontos ab, selbst mit dessen Schlüssel",
                fremdes.backup,
                List(Einrichtung.VERSUCHE) { fremdes.text },
            ),
            Ablehnung("lehnt ab, wenn der Mensch abbricht", vorhanden.backup, listOf(null)),
        )
    }
}
