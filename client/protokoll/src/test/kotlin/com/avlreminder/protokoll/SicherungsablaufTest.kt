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
        var vorDemSpeichern: (() -> Unit)? = null
        var antwortGehtVerloren = false
        private var naechste = 1_000L

        override suspend fun holen() = sicherung

        override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long {
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

    private class Geraet(
        val server: FalscheAblage,
        val stand: FalscherStand = FalscherStand(),
        val id: String = "handy",
        val uebernimmt: Boolean = false,
    ) {
        var gefragt: Sicherungskopf? = null

        val ablauf get() = Sicherungsablauf(KONTO, id, tresor, server, stand, uebernehmen = {
            gefragt = it
            uebernimmt
        }, uhr = { JETZT })

        fun sichern(nutzlast: String = "Daten") = runBlocking { ablauf.sichern(nutzlast.toByteArray(), SCHEMA) }
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
        assertEquals(gemerkt, g.stand.stand)
    }

    // KRITISCH: Gegenstück zu „übernimmt nach Zustimmung“: Ohne Zustimmung bleibt die
    // Sicherung des anderen Geräts unverändert, und das Gerät merkt sich nichts Neues.
    @Test
    fun `überschreibt ohne Zustimmung nie die Sicherung eines anderen Geräts`() {
        val server = FalscheAblage().apply { vonGeraet("tablet", zaehler = 5) }
        val vorher = server.sicherung
        val g = Geraet(server, uebernimmt = false)
        val ergebnis = g.sichern()

        assertEquals(Gesichert.AnderesGeraet(server.inhalt().kopf), ergebnis)
        assertSame(vorher, server.sicherung)
        assertEquals(0, server.gespeichert)
        assertNull(g.stand.stand)
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
        assertThrows<VersionVeraltet> { runBlocking { ablauf.sichern("x".toByteArray(), SCHEMA) } }
        assertEquals("tablet", server.inhalt().kopf.geraetId)
    }

    @Test
    fun `lädt eine Sicherung knapp unter der Größengrenze hoch`() {
        val g = Geraet(FalscheAblage())
        // 1 KiB Luft für Kopf, Nonce, Tag und Tink-Präfix.
        g.sichern("x".repeat(8 * 1024 * 1024 - 1024))
        assertEquals(1, g.server.gespeichert)
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
    ) {
        override fun toString() = name
    }

    // KRITISCH: Zu „spielt nach Bestätigung ein“ die Ablehnungen. Danach ist nichts
    // angewendet, und das Gerät merkt sich keinen neuen Stand.
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
        assertEquals(fall.gemerkt, stand.stand)
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
        fun ablehnungen() = listOf(
            Ablehnung("nicht ohne Bestätigung", { FalscheAblage().apply { vonGeraet("tablet", 4) } }, bestaetigt = false),
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
