package com.avlreminder.protokoll

import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.time.Instant

class SicherungsablaufTest {

    /** Server-Ablage im Speicher; Versionen springen wie bei Cloud Storage (nicht +1). */
    class FalscheAblage(var sicherung: GeholteSicherung? = null) : SicherungsAblage {
        var gespeichert = 0

        /** Alle Aufrufe beim Server, auch abgelehnte: Jeder zählt dort zum Tageslimit. */
        var aufrufe = 0
        var vorDemSpeichern: (() -> Unit)? = null
        var antwortGehtVerloren = false
        private var naechste = 1_000L

        override suspend fun holen(): GeholteSicherung? {
            aufrufe++
            return sicherung
        }

        override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long {
            aufrufe++
            vorDemSpeichern?.invoke()?.also { vorDemSpeichern = null }
            if ((sicherung?.version ?: 0L) != erwartet) throw VersionVeraltet()
            gespeichert++
            naechste += 17
            sicherung = GeholteSicherung(chiffrat, naechste)
            if (antwortGehtVerloren) throw EinrichtungTest.Netzfehler()
            return naechste
        }

        override suspend fun loeschen() {
            sicherung = null
        }

        /** Was ein anderes Gerät desselben Kontos hochlädt. */
        fun vonGeraet(geraet: String, zaehler: Long, nutzlast: String = "fremd") {
            val chiffrat = Datensicherung.verschluesseln(Sicherung(kopf(geraet, zaehler), nutzlast.toByteArray()), tresor, KONTO)
            naechste += 17
            sicherung = GeholteSicherung(chiffrat, naechste)
        }

        fun inhalt() = Datensicherung.entschluesseln(sicherung!!.chiffrat, tresor, KONTO)
    }

    class FalscherStand(var stand: GemerkteSicherung? = null) : SicherungsStand {
        override fun laden() = stand

        override fun speichern(stand: GemerkteSicherung) {
            this.stand = stand
        }
    }

    class Geraet(
        val server: FalscheAblage,
        val stand: FalscherStand = FalscherStand(),
        val id: String = "handy",
        var uebernimmt: Boolean = false,
    ) {
        var gefragt: Sicherungskopf? = null

        val ablauf get() = Sicherungsablauf(KONTO, id, tresor, server, stand, uebernehmen = {
            gefragt = it
            uebernimmt
        }, uhr = { JETZT })

        /** Standardmäßig wie die stündliche Sicherung der App; von Hand mit [automatisch] = false. */
        fun sichern(nutzlast: String = "Daten", automatisch: Boolean = true) =
            runBlocking { ablauf.sichern(nutzlast.toByteArray(), SCHEMA, automatisch) }
    }

    // --- Sichern -------------------------------------------------------------

    @Test
    fun `die erste Sicherung legt an und merkt sich Zähler 1 und die Version`() {
        val g = Geraet(FalscheAblage())
        val ergebnis = g.sichern()

        assertEquals(Gesichert.Ja(g.server.sicherung!!.version, 1), ergebnis)
        assertEquals(GemerkteSicherung(1, g.server.sicherung!!.version), g.stand.stand)
        assertEquals(Sicherungskopf(1, JETZT, "handy", SCHEMA), g.server.inhalt().kopf)
        assertArrayEquals("Daten".toByteArray(), g.server.inhalt().nutzlast)
    }

    @Test
    fun `weitere Sicherungen zählen hoch und fragen nicht`() {
        val g = Geraet(FalscheAblage())
        g.sichern()
        g.sichern("neu")

        assertEquals(2, g.server.inhalt().kopf.zaehler)
        assertArrayEquals("neu".toByteArray(), g.server.inhalt().nutzlast)
        assertNull(g.gefragt)
    }

    @Test
    fun `nach verlorener Antwort erkennt das Gerät seine eigene Sicherung und sichert weiter`() {
        val server = FalscheAblage().apply { antwortGehtVerloren = true }
        val g = Geraet(server)
        assertThrows<EinrichtungTest.Netzfehler> { g.sichern() }
        assertNull(g.stand.stand)

        server.antwortGehtVerloren = false
        val ergebnis = g.sichern("zweiter Versuch")

        assertInstanceOf(Gesichert.Ja::class.java, ergebnis)
        assertNull(g.gefragt, "die eigene Sicherung braucht keine Rückfrage")
        assertEquals(2, server.inhalt().kopf.zaehler)
    }

    @Test
    fun `übernimmt die Sicherung eines anderen Geräts nach Zustimmung mit höherem Zähler`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val g = Geraet(server, uebernimmt = true)
        val ergebnis = g.sichern()

        assertInstanceOf(Gesichert.Ja::class.java, ergebnis)
        assertEquals("tablet", g.gefragt?.geraetId)
        assertEquals(Sicherungskopf(6, JETZT, "handy", SCHEMA), server.inhalt().kopf)
    }

    @Test
    fun `ist gerade ein anderes Gerät schneller, fragt das Gerät erst nach`() {
        val server = FalscheAblage()
        val g = Geraet(server, uebernimmt = false)
        g.sichern()
        val gemerkt = g.stand.stand
        server.vorDemSpeichern = { server.vonGeraet("tablet", zaehler = 9) }

        val ergebnis = g.sichern("meins")

        assertInstanceOf(Gesichert.AnderesGeraet::class.java, ergebnis)
        assertEquals("tablet", g.gefragt?.geraetId)
        assertEquals("tablet", server.inhalt().kopf.geraetId)
        assertEquals(gemerkt?.copy(gesehen = 9, abgelehnt = true), g.stand.stand) // eigener Stand bleibt, Zähler 9 gesehen
    }

    // KRITISCH: Gegenstück zu „übernimmt nach Zustimmung“: Ohne Zustimmung bleibt die
    // Sicherung des anderen Geräts unverändert. Das Gerät merkt sich nur ihren Zähler
    // als gesehen, nicht als eigenen Stand.
    @Test
    fun `überschreibt ohne Zustimmung nie die Sicherung eines anderen Geräts`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val vorher = server.sicherung
        val g = Geraet(server, uebernimmt = false)
        val ergebnis = g.sichern()

        assertEquals(Gesichert.AnderesGeraet(server.inhalt().kopf), ergebnis)
        assertSame(vorher, server.sicherung)
        assertEquals(0, server.gespeichert)
        assertEquals(GemerkteSicherung(zaehler = 0, version = 0, gesehen = 5, abgelehnt = true), g.stand.stand)
    }

    // KRITISCH: E-B2b-3 „gesichert oder gesehen“: Hat der Mensch eine Sicherung
    // abgelehnt, darf der Server danach keine ältere Fassung unterschieben.
    @Test
    fun `nach abgelehnter Übernahme gilt eine ältere Fassung als veraltet`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val g = Geraet(server, uebernimmt = false)
        g.sichern()
        server.vonGeraet("tablet", zaehler = 4) // Server liefert eine ältere Fassung

        assertThrows<AeltereSicherung> { g.sichern(automatisch = false) }
        assertThrows<AeltereSicherung> { runBlocking { g.ablauf.wiederherstellen(SCHEMA, { true }, { }) } }
        assertEquals(0, server.gespeichert)
    }

    // Gegenprobe zu „ruht nach abgelehnter Übernahme“: Von Hand fragt es wieder.
    @Test
    fun `von Hand fragt das Gerät nach abgelehnter Übernahme wieder nach`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val g = Geraet(server, uebernimmt = false)
        g.sichern()
        g.gefragt = null

        assertInstanceOf(Gesichert.AnderesGeraet::class.java, g.sichern(automatisch = false))
        assertEquals(5, g.gefragt?.zaehler)
    }

    // KRITISCH: E-B2b-6 „fragt einmal“ und Review-Lauf 8, Frage 6: Nach einer Ablehnung
    // fragt die stündliche Sicherung nicht wieder und ruft den Server gar nicht auf.
    // Sonst verbrauchte jeder Lauf einen Versuch vom Tageslimit.
    @Test
    fun `nach abgelehnter Übernahme ruht das automatische Sichern, ohne den Server zu fragen`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val vorher = server.sicherung
        val g = Geraet(server, uebernimmt = false)
        assertInstanceOf(Gesichert.AnderesGeraet::class.java, g.sichern()) // fragt einmal
        assertEquals(5, g.gefragt?.zaehler)
        g.gefragt = null
        val aufrufe = server.aufrufe

        repeat(3) { assertEquals(Gesichert.Ruht, g.sichern()) }
        assertNull(g.gefragt)
        assertEquals(aufrufe, server.aufrufe)
        assertSame(vorher, server.sicherung)
    }

    data class Freigabe(val name: String, val ausloesen: (Geraet) -> Unit) {
        override fun toString() = name
    }

    // Was das automatische Sichern nach einer Ablehnung wieder in Gang bringt: Der
    // Mensch hat selbst entschieden. Danach sichert es ohne Rückfrage.
    @ParameterizedTest(name = "{0}")
    @MethodSource("freigaben")
    fun `nach abgelehnter Übernahme sichert das Gerät wieder automatisch`(fall: Freigabe) {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val g = Geraet(server, uebernimmt = false)
        g.sichern() // abgelehnt
        fall.ausloesen(g)
        g.uebernimmt = false
        g.gefragt = null

        assertInstanceOf(Gesichert.Ja::class.java, g.sichern())
        assertNull(g.gefragt)
        assertEquals("handy", server.inhalt().kopf.geraetId)
    }

    // KRITISCH: Ein Server, der eine ältere Fassung zeigt als das Gerät kennt, bringt
    // es nicht dazu, Zähler zurückzusetzen oder etwas zu überschreiben.
    @Test
    fun `lädt nichts hoch, wenn der Server eine ältere Sicherung liefert als gemerkt`() {
        val server = FalscheAblage().apply { vonGeraet("handy", zaehler = 3) }
        val gemerkt = GemerkteSicherung(zaehler = 8, version = 42)
        val g = Geraet(server, FalscherStand(gemerkt), uebernimmt = true)

        assertThrows<AeltereSicherung> { g.sichern() }
        assertEquals(0, server.gespeichert)
        assertEquals(gemerkt, g.stand.stand)
    }

    @Test
    fun `legt neu an, wenn die Sicherung inzwischen gelöscht wurde`() {
        val server = FalscheAblage()
        val g = Geraet(server)
        g.sichern()
        runBlocking { server.loeschen() }

        assertInstanceOf(Gesichert.Ja::class.java, g.sichern())
        assertEquals(2, server.inhalt().kopf.zaehler)
    }

    @Test
    fun `gibt nach dem zweiten Konflikt auf, statt endlos zu versuchen`() {
        val server = FalscheAblage()
        // Jedes Mal, wenn das Gerät speichern will, war das Tablet wieder schneller.
        var tablet = 0L
        val ablage = object : SicherungsAblage by server {
            override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long {
                server.vonGeraet("tablet", zaehler = ++tablet)
                return server.speichern(chiffrat, erwartet)
            }
        }
        val ablauf = Sicherungsablauf(KONTO, "handy", tresor, ablage, FalscherStand(), uebernehmen = { true }, uhr = { JETZT })
        assertThrows<VersionVeraltet> { runBlocking { ablauf.sichern("x".toByteArray(), SCHEMA, automatisch = true) } }
        assertEquals("tablet", server.inhalt().kopf.geraetId)
    }

    @Test
    fun `lädt eine Sicherung knapp unter der Größengrenze hoch`() {
        val g = Geraet(FalscheAblage())
        // 1 KiB Luft für Kopf, Nonce, Tag und Tink-Präfix.
        g.sichern("x".repeat(8 * 1024 * 1024 - 1024))
        assertEquals(1, g.server.gespeichert)
    }

    // KRITISCH: Die Grenze liegt genau bei 8 MiB Chiffrat (docs/formate.md): genau
    // 8 MiB gehen hoch, ein Byte mehr nicht. Der Aufschlag von Kopf und Verschlüsselung
    // wird gemessen, weil er von Tink und vom Kopf abhängt.
    @Test
    fun `nimmt genau 8 MiB Chiffrat an, ein Byte mehr nicht`() {
        val achtMiB = 8 * 1024 * 1024
        val probe = Geraet(FalscheAblage())
        val probeLaenge = achtMiB - 4096 // gleiche Längenkodierung wie an der Grenze
        probe.sichern("x".repeat(probeLaenge))
        val aufschlag = probe.server.sicherung!!.chiffrat.size - probeLaenge

        val genau = Geraet(FalscheAblage())
        genau.sichern("x".repeat(achtMiB - aufschlag))
        assertEquals(achtMiB, genau.server.sicherung!!.chiffrat.size)

        val darueber = Geraet(FalscheAblage())
        assertThrows<SicherungZuGross> { darueber.sichern("x".repeat(achtMiB - aufschlag + 1)) }
        assertEquals(0, darueber.server.gespeichert)
    }

    @Test
    fun `lehnt eine Sicherung über der Größengrenze ab, ohne hochzuladen`() {
        val g = Geraet(FalscheAblage())
        // Eigene Zahl (8 MiB aus docs/formate.md) statt MAX_CHIFFRAT: Sonst bliebe
        // der Test grün, wenn jemand die Konstante ändert.
        val zuGross = "x".repeat(8 * 1024 * 1024)
        assertThrows<SicherungZuGross> { g.sichern(zuGross) }
        assertEquals(0, g.server.gespeichert)
    }

    // --- Wiederherstellen ----------------------------------------------------

    private fun wiederherstellen(
        server: FalscheAblage,
        stand: FalscherStand = FalscherStand(),
        bestaetigt: Boolean = true,
        hoechste: Int = SCHEMA,
        angewendet: MutableList<Sicherung> = mutableListOf(),
        anwenden: (Sicherung) -> Unit = { angewendet += it },
    ) = runBlocking {
        Sicherungsablauf(KONTO, "neu", tresor, server, stand, uebernehmen = { false }, uhr = { JETZT })
            .wiederherstellen(hoechste, bestaetigen = { bestaetigt }, anwenden = anwenden)
    }

    @Test
    fun `spielt nach Bestätigung die neueste Sicherung ein und merkt sich ihren Stand`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 4, nutzlast = "alles") }
        val stand = FalscherStand()
        val angewendet = mutableListOf<Sicherung>()
        val ergebnis = wiederherstellen(server, stand, angewendet = angewendet)

        assertEquals(Wiederhergestellt.Ja(server.inhalt().kopf), ergebnis)
        assertArrayEquals("alles".toByteArray(), angewendet.single().nutzlast)
        assertEquals(GemerkteSicherung(4, server.sicherung!!.version), stand.stand)
    }

    @Test
    fun `danach sichert das Gerät ohne Rückfrage weiter`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 4) }
        val stand = FalscherStand()
        wiederherstellen(server, stand)
        val g = Geraet(server, stand, id = "neu")

        assertInstanceOf(Gesichert.Ja::class.java, g.sichern())
        assertNull(g.gefragt)
        assertEquals(5, server.inhalt().kopf.zaehler)
    }

    @Test
    fun `spielt dieselbe Fassung erneut ein, die das Gerät schon kennt`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 4) }
        val stand = FalscherStand(GemerkteSicherung(zaehler = 4, version = server.sicherung!!.version))
        assertInstanceOf(Wiederhergestellt.Ja::class.java, wiederherstellen(server, stand))
    }

    @Test
    fun `ohne Sicherung meldet es das und wendet nichts an`() {
        val angewendet = mutableListOf<Sicherung>()
        assertEquals(Wiederhergestellt.KeineSicherung, wiederherstellen(FalscheAblage(), angewendet = angewendet))
        assertTrue(angewendet.isEmpty())
    }

    @Test
    fun `nimmt eine Sicherung mit der höchsten bekannten Schema-Version an`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 1) }
        assertInstanceOf(Wiederhergestellt.Ja::class.java, wiederherstellen(server, hoechste = SCHEMA))
    }

    data class Ablehnung(
        val name: String,
        val server: () -> FalscheAblage,
        val gemerkt: GemerkteSicherung? = null,
        val bestaetigt: Boolean = true,
        val hoechste: Int = SCHEMA,
        val erwartet: Class<out Throwable>? = null,
        /** Der Stand danach; ohne Angabe unverändert. */
        val danach: GemerkteSicherung? = gemerkt,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Zu „spielt nach Bestätigung ein“ die Ablehnungen. Danach ist nichts
    // angewendet, und das Gerät merkt sich keinen neuen Stand (lehnt der Mensch ab,
    // nur den gesehenen Zähler).
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablehnungen")
    fun `spielt nicht ein`(fall: Ablehnung) {
        val stand = FalscherStand(fall.gemerkt)
        val angewendet = mutableListOf<Sicherung>()
        if (fall.erwartet != null) {
            val e = runCatching { wiederherstellen(fall.server(), stand, fall.bestaetigt, fall.hoechste, angewendet) }.exceptionOrNull()
            assertInstanceOf(fall.erwartet, e)
        } else {
            assertEquals(Wiederhergestellt.Abgelehnt, wiederherstellen(fall.server(), stand, fall.bestaetigt, fall.hoechste, angewendet))
        }
        assertTrue(angewendet.isEmpty())
        assertEquals(fall.danach, stand.stand)
    }

    // KRITISCH: Das Ruhen gilt nur nach „nein“ auf die Übernahme einer fremden Sicherung
    // (E-B2b-6). Bricht der Mensch das Wiederherstellen der eigenen ab, sichert das
    // Gerät weiter automatisch; sonst stünde das Backup unbemerkt still.
    @Test
    fun `nach abgelehnter Wiederherstellung der eigenen Sicherung sichert das Gerät weiter automatisch`() {
        val server = FalscheAblage()
        val stand = FalscherStand()
        val g = Geraet(server, stand, id = "neu")
        g.sichern()
        assertEquals(Wiederhergestellt.Abgelehnt, wiederherstellen(server, stand, bestaetigt = false))

        assertInstanceOf(Gesichert.Ja::class.java, g.sichern())
        assertNull(g.gefragt)
    }

    // --- Löschen --------------------------------------------------------------

    // KRITISCH: Nach dem Löschen zählt das Gerät weiter, damit andere Geräte, die
    // den alten Zähler kennen, die neue Sicherung annehmen.
    @Test
    fun `nach dem Löschen zählt die nächste Sicherung weiter`() {
        val server = FalscheAblage()
        val g = Geraet(server)
        g.sichern()
        g.sichern()
        val anderes = FalscherStand()
        wiederherstellen(server, anderes) // kennt jetzt Zähler 2

        runBlocking { g.ablauf.loeschen() }
        assertNull(server.sicherung)
        assertEquals(GemerkteSicherung(2, 0), g.stand.stand)

        assertEquals(3, (g.sichern() as Gesichert.Ja).zaehler)
        assertInstanceOf(Wiederhergestellt.Ja::class.java, wiederherstellen(server, anderes))
    }

    // KRITISCH: Auch ein nur gesehener Zähler zählt beim Neuanlegen mit. Sonst finge
    // das Handy nach dem Löschen bei 1 an, und das Tablet (kennt 5) lehnte die
    // Sicherung des Handys dauerhaft als älter ab.
    @Test
    fun `nach abgelehnter Übernahme und Löschen zählt die nächste Sicherung über dem gesehenen Zähler weiter`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val g = Geraet(server, uebernimmt = false)
        g.sichern() // abgelehnt: Zähler 5 gesehen
        runBlocking { g.ablauf.loeschen() }

        assertEquals(6, (g.sichern() as Gesichert.Ja).zaehler)
    }

    @Test
    fun `löscht auch ohne Schlüssel und ohne gemerkten Stand und merkt sich nichts`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 4) }
        val stand = FalscherStand()
        runBlocking { Sicherungsablauf.loeschen(server, stand) }
        assertNull(server.sicherung)
        assertNull(stand.stand)
    }

    @Test
    fun `merkt sich nichts, wenn das Anwenden scheitert`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 4) }
        val stand = FalscherStand()
        assertThrows<IllegalStateException> { wiederherstellen(server, stand, anwenden = { error("Datenbank voll") }) }
        assertNull(stand.stand)
        // Sonst würde die nächste Sicherung die gute Fassung auf dem Server mit kaputten Daten überschreiben.
        assertFalse(Geraet(server, stand, id = "neu").sichern() is Gesichert.Ja)
    }

    companion object {
        private const val KONTO = "konto-a"
        private const val SCHEMA = 5
        private val JETZT: Instant = Instant.ofEpochSecond(1_793_000_000)
        private val tresor = SymmetrischerSchluessel.erzeugen()

        private fun kopf(geraet: String, zaehler: Long) = Sicherungskopf(zaehler, JETZT, geraet, SCHEMA)

        @JvmStatic
        fun freigaben() = listOf(
            Freigabe("von Hand übernommen") {
                it.uebernimmt = true
                assertInstanceOf(Gesichert.Ja::class.java, it.sichern(automatisch = false))
            },
            Freigabe("wiederhergestellt") {
                runBlocking { it.ablauf.wiederherstellen(SCHEMA, { true }, { }) }
            },
            Freigabe("gelöscht") {
                runBlocking { it.ablauf.loeschen() }
            },
        )

        @JvmStatic
        fun ablehnungen() = listOf(
            Ablehnung(
                "nicht ohne Bestätigung",
                { FalscheAblage().apply { vonGeraet("tablet", 4) } },
                bestaetigt = false,
                danach = GemerkteSicherung(0, 0, gesehen = 4, abgelehnt = true),
            ),
            // Die eigene Fassung muss das Gerät nicht übernehmen: Ablehnen merkt nur den Zähler.
            Ablehnung(
                "nicht ohne Bestätigung, auch die eigene Sicherung",
                { FalscheAblage().apply { vonGeraet("neu", 4) } },
                bestaetigt = false,
                danach = GemerkteSicherung(0, 0, gesehen = 4),
            ),
            Ablehnung(
                "keine ältere Sicherung als gemerkt",
                { FalscheAblage().apply { vonGeraet("tablet", 3) } },
                gemerkt = GemerkteSicherung(8, 99),
                erwartet = AeltereSicherung::class.java,
            ),
            Ablehnung(
                "keine Sicherung aus einer neueren App-Version",
                { FalscheAblage().apply { vonGeraet("tablet", 4) } },
                hoechste = SCHEMA - 1,
                erwartet = NeueresSchema::class.java,
            ),
            Ablehnung(
                "keine Sicherung, die sich nicht entschlüsseln lässt",
                { FalscheAblage(GeholteSicherung(SymmetrischerSchluessel.erzeugen().verschluesseln(ByteArray(8), ByteArray(0)), 5)) },
                erwartet = EntschluesselnFehlgeschlagen::class.java,
            ),
        )
    }
}
