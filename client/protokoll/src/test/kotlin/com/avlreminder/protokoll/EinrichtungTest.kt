package com.avlreminder.protokoll

import com.google.crypto.tink.subtle.Ed25519Verify
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class EinrichtungTest {

    /**
     * Server im Speicher, mit derselben Versionsprüfung wie der Server und
     * echter Prüfung der Freischalt-Signatur (für das Gerät [GERAET]).
     */
    class FalscheAblage(
        var backup: GeholtesBackup? = null,
        var freischaltschluessel: ByteArray? = null,
    ) : BackupAblage, GeraeteFreischaltung {
        var gespeichert = 0
        var freigeschaltet = false
        var freischaltVersuche = 0

        /** Läuft vor dem Speichern, z. B. ein anderes Gerät, das schneller ist. */
        var vorDemSpeichern: (() -> Unit)? = null

        /** Wirft nach dem Speichern: Der Server hat geschrieben, die Antwort ging verloren. */
        var antwortGehtVerloren = false

        /** Wirft vor dem Speichern: Der Server war nicht erreichbar. */
        var nichtErreichbar = false

        override suspend fun holen() = backup

        override suspend fun speichern(chiffrat: ByteArray, freischaltschluessel: ByteArray, erwartet: Long): Long {
            vorDemSpeichern?.invoke()?.also { vorDemSpeichern = null }
            if (nichtErreichbar) throw Netzfehler()
            if ((backup?.version ?: 0L) != erwartet) throw VersionVeraltet()
            gespeichert++
            backup = GeholtesBackup(chiffrat, erwartet + 1)
            this.freischaltschluessel = freischaltschluessel
            if (antwortGehtVerloren) throw Netzfehler()
            return erwartet + 1
        }

        override suspend fun mitWiederherstellung(signatur: ByteArray) {
            freischaltVersuche++
            if (nichtErreichbar) throw Netzfehler()
            val schluessel = freischaltschluessel ?: throw GeraetNichtFreigeschaltet()
            try {
                Ed25519Verify(schluessel).verify(signatur, Wiederherstellungsschluessel.freischaltNachricht(GERAET))
            } catch (_: java.security.GeneralSecurityException) {
                throw GeraetNichtFreigeschaltet()
            }
            freigeschaltet = true
        }
    }

    class Netzfehler : Exception("Verbindung abgebrochen")

    /** Gerätespeicher im Speicher. */
    class FalscherLokalerSpeicher(var stand: LokalerStand? = null) : LokaleAblage {
        override fun laden() = stand

        override fun speichern(stand: LokalerStand) {
            this.stand = stand
        }
    }

    /** Ein Lauf der Einrichtung; merkt sich den angezeigten Wiederherstellungsschlüssel. */
    private class Lauf(
        val server: FalscheAblage,
        val lokal: FalscherLokalerSpeicher = FalscherLokalerSpeicher(),
        vararg eingaben: String?,
        val bestaetigt: Boolean = true,
        val beimAnzeigen: () -> Unit = {},
        val freigeschaltet: Boolean = false,
    ) {
        var angezeigt: Wiederherstellungsschluessel? = null
        private val rest = eingaben.toMutableList()

        fun ausfuehren() = runBlocking {
            Einrichtung(
                KONTO,
                GERAET,
                freigeschaltet,
                server,
                server,
                lokal,
                frageSchluessel = { rest.removeFirst() },
                zeigeWiederherstellung = {
                    angezeigt = it
                    beimAnzeigen()
                    bestaetigt
                },
                meldung = {},
            ).ausfuehren()
        }
    }

    // --- Erstes Gerät ------------------------------------------------------

    @Test
    fun `ohne Backup erzeugt das Gerät die Schlüssel, legt sie ab und lädt das Backup hoch`() {
        val lauf = Lauf(FalscheAblage())
        val ergebnis = lauf.ausfuehren()

        assertInstanceOf(Eingerichtet.Neu::class.java, ergebnis)
        assertEquals(1, lauf.server.gespeichert)
        // Das hochgeladene Backup öffnet sich mit dem angezeigten Schlüssel und enthält denselben Bund.
        val wieder = Schluesselbund.ausBackup(lauf.server.backup!!.chiffrat, lauf.angezeigt!!, KONTO)
        assertEquals(ergebnis.bund.person.oeffentlich.fingerabdruck(), wieder.person.oeffentlich.fingerabdruck())
        // Lokal liegt derselbe Bund, nichts mehr offen.
        assertSame(ergebnis.bund, lauf.lokal.stand!!.bund)
        assertNull(lauf.lokal.stand!!.offenesBackup)
        // Mit dem Backup geht der Freischaltschlüssel des angezeigten Schlüssels hoch.
        assertArrayEquals(lauf.angezeigt!!.freischaltschluessel(), lauf.server.freischaltschluessel)
        assertEquals(0, lauf.server.freischaltVersuche)
    }

    // KRITISCH: Erst anzeigen, dann lokal ablegen, dann hochladen. Sonst kann ein
    // Backup auf dem Server landen, dessen Wiederherstellungsschlüssel niemand kennt.
    @Test
    fun `zeigt den Wiederherstellungsschlüssel, bevor etwas gespeichert oder hochgeladen wird`() {
        val server = FalscheAblage()
        val lokal = FalscherLokalerSpeicher()
        var beimAnzeigen: Pair<GeholtesBackup?, LokalerStand?>? = null
        var beimHochladen: LokalerStand? = null
        server.vorDemSpeichern = { beimHochladen = lokal.stand }
        Lauf(server, lokal, beimAnzeigen = { beimAnzeigen = server.backup to lokal.stand }).ausfuehren()

        assertEquals(null to null, beimAnzeigen)
        assertNotNull(beimHochladen?.offenesBackup, "vor dem Hochladen muss das Backup lokal liegen")
    }

    @Test
    fun `nach verlorener Antwort erkennt der nächste Lauf das eigene Backup`() {
        val server = FalscheAblage().apply { antwortGehtVerloren = true }
        val erster = Lauf(server)
        assertThrows<Netzfehler> { erster.ausfuehren() }

        server.antwortGehtVerloren = false
        val ergebnis = Lauf(server, erster.lokal).ausfuehren()

        assertInstanceOf(Eingerichtet.Neu::class.java, ergebnis)
        assertEquals(1, server.gespeichert)
        assertNull(erster.lokal.stand!!.offenesBackup)
        // Der beim ersten Lauf angezeigte Schlüssel öffnet das Backup auf dem Server.
        val wieder = Schluesselbund.ausBackup(server.backup!!.chiffrat, erster.angezeigt!!, KONTO)
        assertEquals(ergebnis.bund.person.oeffentlich.fingerabdruck(), wieder.person.oeffentlich.fingerabdruck())
    }

    @Test
    fun `nach gescheitertem Hochladen lädt der nächste Lauf dasselbe Backup hoch`() {
        val server = FalscheAblage().apply { nichtErreichbar = true }
        val erster = Lauf(server)
        assertThrows<Netzfehler> { erster.ausfuehren() }
        val offen = erster.lokal.stand!!.offenesBackup!!

        server.nichtErreichbar = false
        val zweiter = Lauf(server, erster.lokal)
        assertInstanceOf(Eingerichtet.Neu::class.java, zweiter.ausfuehren())
        assertArrayEquals(offen.chiffrat, server.backup!!.chiffrat)
        assertArrayEquals(erster.angezeigt!!.freischaltschluessel(), server.freischaltschluessel)
        assertNull(zweiter.angezeigt, "kein zweiter Wiederherstellungsschlüssel")
    }

    // KRITISCH: Ein anderes Gerät war zwischen Holen und Speichern schneller.
    // Sein Backup bleibt, und dieses Gerät übernimmt dessen Schlüssel.
    @Test
    fun `überschreibt kein Backup, das ein anderes Gerät gerade angelegt hat`() {
        val server = FalscheAblage()
        server.vorDemSpeichern = {
            server.backup = vorhanden.backup
            server.freischaltschluessel = vorhanden.freischaltschluessel
        }
        val lauf = Lauf(server, FalscherLokalerSpeicher(), vorhanden.text)
        val ergebnis = lauf.ausfuehren()

        assertInstanceOf(Eingerichtet.AusBackup::class.java, ergebnis)
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
        assertSame(vorhanden.backup, server.backup)
        assertSame(vorhanden.freischaltschluessel, server.freischaltschluessel)
        assertTrue(server.freigeschaltet, "mit dem Schlüssel des anderen Geräts freigeschaltet")
        // Lokal liegt jetzt der Bund des anderen Geräts, nichts offen.
        assertEquals(vorhanden.fingerabdruck, lauf.lokal.stand!!.bund.person.oeffentlich.fingerabdruck())
        assertNull(lauf.lokal.stand!!.offenesBackup)
    }

    // --- Weitere Geräte ----------------------------------------------------

    @Test
    fun `mit Backup holt das Gerät die Schlüssel, schaltet sich frei, legt sie ab und lädt nichts hoch`() {
        val lauf = Lauf(vorhanden.server(), FalscherLokalerSpeicher(), vorhanden.text)
        val ergebnis = lauf.ausfuehren()

        assertInstanceOf(Eingerichtet.AusBackup::class.java, ergebnis)
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
        assertSame(ergebnis.bund, lauf.lokal.stand!!.bund)
        assertTrue(lauf.server.freigeschaltet)
        assertEquals(0, lauf.server.gespeichert)
        assertNull(lauf.angezeigt)
    }

    @Test
    fun `ein schon freigeschaltetes Gerät schaltet sich nicht noch einmal frei`() {
        val lauf = Lauf(vorhanden.server(), FalscherLokalerSpeicher(), vorhanden.text, freigeschaltet = true)
        assertInstanceOf(Eingerichtet.AusBackup::class.java, lauf.ausfuehren())
        assertEquals(0, lauf.server.freischaltVersuche)
    }

    // KRITISCH: Scheitert die Freischaltung, legt das Gerät nichts ab. Der nächste
    // Lauf fragt wieder nach dem Schlüssel und schaltet dann frei.
    @Test
    fun `legt nichts ab, wenn die Freischaltung scheitert`() {
        val server = vorhanden.server().apply { nichtErreichbar = true }
        val erster = Lauf(server, FalscherLokalerSpeicher(), vorhanden.text)
        assertThrows<Netzfehler> { erster.ausfuehren() }
        assertNull(erster.lokal.stand)
        assertFalse(server.freigeschaltet)

        server.nichtErreichbar = false
        assertInstanceOf(Eingerichtet.AusBackup::class.java, Lauf(server, erster.lokal, vorhanden.text).ausfuehren())
        assertTrue(server.freigeschaltet)
    }

    @Test
    fun `nimmt den richtigen Schlüssel nach einem Tippfehler an`() {
        val ergebnis = Lauf(vorhanden.server(), FalscherLokalerSpeicher(), "AAAA-BBBB", vorhanden.text.lowercase()).ausfuehren()
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
    }

    // Gegenstück zu „lehnt nach drei Tippfehlern ab“: Der dritte Versuch zählt noch.
    @Test
    fun `nimmt den richtigen Schlüssel im dritten Versuch an`() {
        val ergebnis = Lauf(vorhanden.server(), FalscherLokalerSpeicher(), "falsch", fremdes.text, vorhanden.text).ausfuehren()
        assertEquals(vorhanden.fingerabdruck, ergebnis.bund.person.oeffentlich.fingerabdruck())
    }

    @Test
    fun `ein eingerichtetes Gerät fragt den Server nicht`() {
        val bund = Schluesselbund.erzeugen()
        val server = vorhanden.server().apply { nichtErreichbar = true }
        val ergebnis = Lauf(server, FalscherLokalerSpeicher(LokalerStand(bund, null))).ausfuehren()
        assertInstanceOf(Eingerichtet.Vorhanden::class.java, ergebnis)
        assertSame(bund, ergebnis.bund)
    }

    // --- Ablehnungen -------------------------------------------------------

    data class Ablehnung(
        val name: String,
        val konto: Vorhanden?,
        val eingaben: List<String?>,
        val bestaetigt: Boolean = true,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Zu jeder Annahme oben die Ablehnung. Danach gibt es keinen
    // Schlüsselbund, lokal liegt nichts, am Backup auf dem Server ändert sich
    // nichts, und das Gerät ist nicht freigeschaltet.
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `lehnt ab`(fall: Ablehnung) {
        val server = fall.konto?.server() ?: FalscheAblage()
        val vorher = server.backup
        val lauf = Lauf(server, FalscherLokalerSpeicher(), *fall.eingaben.toTypedArray(), bestaetigt = fall.bestaetigt)
        assertThrows<EinrichtungAbgebrochen> { lauf.ausfuehren() }
        assertEquals(0, lauf.server.gespeichert)
        assertSame(vorher, lauf.server.backup)
        assertNull(lauf.lokal.stand)
        assertFalse(lauf.server.freigeschaltet)
    }

    @Test
    fun `fragt nach dem Abbruch nicht weiter`() {
        val eingaben = mutableListOf<String?>(null, vorhanden.text)
        val server = vorhanden.server()
        val e = Einrichtung(
            KONTO,
            GERAET,
            false,
            server,
            server,
            FalscherLokalerSpeicher(),
            frageSchluessel = { eingaben.removeFirst() },
            zeigeWiederherstellung = { true },
            meldung = {},
        )
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
        val freischaltschluessel = schluessel.freischaltschluessel()

        /** Ein Server, auf dem dieses Backup samt Freischaltschlüssel liegt. */
        fun server() = FalscheAblage(backup, freischaltschluessel)
    }

    companion object {
        private const val KONTO = "konto-a"
        private const val GERAET = "d0e5518e666349cb0a3637c6b536b1cd"
        private val vorhanden = Vorhanden(KONTO)
        private val fremdes = Vorhanden("konto-b")

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung("lehnt nach drei Tippfehlern ab", vorhanden, listOf("falsch", "auch falsch", "immer noch falsch")),
            Ablehnung("lehnt den Wiederherstellungsschlüssel eines anderen Kontos ab", vorhanden, List(3) { fremdes.text }),
            Ablehnung(
                "lehnt ein vom Server untergeschobenes Backup eines anderen Kontos ab, selbst mit dessen Schlüssel",
                fremdes,
                List(3) { fremdes.text },
            ),
            Ablehnung("lehnt ab, wenn der Mensch abbricht", vorhanden, listOf(null)),
            Ablehnung("speichert nichts, wenn der Mensch den neuen Schlüssel nicht bestätigt", null, emptyList(), bestaetigt = false),
        )
    }
}
