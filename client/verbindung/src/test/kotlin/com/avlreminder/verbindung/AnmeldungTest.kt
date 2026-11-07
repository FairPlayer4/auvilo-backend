package com.avlreminder.verbindung

import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.schnittstelle.v1.AnmeldenResponse
import com.avlreminder.schnittstelle.v1.SitzungErneuernRequest
import com.avlreminder.schnittstelle.v1.SitzungErneuernResponse
import com.google.protobuf.MessageLite
import com.google.protobuf.Timestamp
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.net.InetSocketAddress
import java.security.KeyPairGenerator
import java.security.PublicKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import kotlin.reflect.KClass
import com.avlreminder.schnittstelle.v1.Sitzung as SitzungNachricht

/**
 * Anmelden und Erneuern gegen einen Nachbau des Servers, der Connect spricht.
 * Geprüft wird, was das Gerät signiert und sendet, wann es erneuert und wie
 * Fehler des Servers ankommen.
 */
class AnmeldungTest {

    private sealed interface Antwort {
        data class Ok(val nachricht: MessageLite) : Antwort
        /** Fehler im Connect-Format (JSON), wie ihn der Dienst selbst sendet. */
        data class Fehler(val code: String) : Antwort
        /** Antwort von etwas vor dem Dienst, etwa Google bei gesperrtem Dienst. */
        data class Roh(val status: Int, val typ: String, val text: String) : Antwort
    }

    private class Anfrage(val pfad: String, val inhalt: ByteArray)

    private lateinit var server: HttpServer
    private var antwort: Antwort = Antwort.Fehler("internal")
    private val anfragen = mutableListOf<Anfrage>()

    @BeforeEach
    fun starten() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { ex ->
            anfragen += Anfrage(ex.requestURI.path, ex.requestBody.readAllBytes())
            val (status, typ, inhalt) = when (val a = antwort) {
                is Antwort.Ok -> Triple(200, "application/proto", a.nachricht.toByteArray())
                is Antwort.Fehler -> Triple(400, "application/json", """{"code":"${a.code}","message":"nachgebaut"}""".toByteArray())
                is Antwort.Roh -> Triple(a.status, a.typ, a.text.toByteArray())
            }
            ex.responseHeaders.add("Content-Type", typ)
            ex.sendResponseHeaders(status, inhalt.size.toLong())
            ex.responseBody.use { it.write(inhalt) }
        }
        server.start()
    }

    @AfterEach
    fun stoppen() = server.stop(0)

    /** Ein Geräteschlüssel nur im Speicher; [oeffentlicherSchluessel] prüft seine Signaturen. */
    private class TestSchluessel : Geraeteschluessel {
        private val paar = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
        val oeffentlicherSchluessel: PublicKey get() = paar.public
        override val oeffentlich: ByteArray get() = paar.public.encoded
        override fun signiere(nachricht: ByteArray): ByteArray =
            Signature.getInstance("SHA256withECDSA").run {
                initSign(paar.private)
                update(nachricht)
                sign()
            }
    }

    private class Speicher(var sitzung: Sitzung? = null) : SitzungsAblage {
        override suspend fun lesen() = sitzung
        override suspend fun speichern(sitzung: Sitzung) {
            this.sitzung = sitzung
        }
    }

    private val schluessel = TestSchluessel()
    private val jetzt = 1_793_534_400L

    private fun anmeldung(speicher: Speicher) =
        Anmeldung(Verbindung("http://127.0.0.1:${server.address.port}"), schluessel, speicher, uhr = { jetzt })

    private fun pruefeSignatur(nachricht: ByteArray, signatur: ByteArray) {
        val gueltig = Signature.getInstance("SHA256withECDSA").run {
            initVerify(schluessel.oeffentlicherSchluessel)
            update(nachricht)
            verify(signatur)
        }
        assertTrue(gueltig, "Signatur passt nicht zur erwarteten Nachricht")
    }

    private fun sitzungNachricht(token: String, laeuftAb: Long) =
        SitzungNachricht.newBuilder().setToken(token).setLaeuftAb(Timestamp.newBuilder().setSeconds(laeuftAb)).build()

    private fun erneuernAntwort(token: String) =
        Antwort.Ok(SitzungErneuernResponse.newBuilder().setSitzung(sitzungNachricht(token, jetzt + 3600)).build())

    // KRITISCH: Anmelden schickt den öffentlichen Schlüssel und signiert genau
    // "avlreminder/anmelden/v1\n" + ID-Token; sonst lehnt der Server jedes Gerät ab.
    @Test
    fun `Anmelden sendet Schlüssel und Signatur und merkt sich die Sitzung`() {
        antwort = Antwort.Ok(
            AnmeldenResponse.newBuilder()
                .setKontoId("konto-1").setGeraetId("geraet-1")
                .setSitzung(sitzungNachricht("token-1", jetzt + 3600))
                .setNeuesKonto(true).setFreigeschaltet(true)
                .build(),
        )
        val speicher = Speicher()

        val ergebnis = runBlocking { anmeldung(speicher).anmelden("id-token") }

        val anfrage = AnmeldenRequest.parseFrom(anfragen.single().inhalt)
        assertEquals("/avlreminder.v1.AnmeldeDienst/Anmelden", anfragen.single().pfad)
        assertEquals("id-token", anfrage.googleIdToken)
        assertArrayEquals(schluessel.oeffentlich, anfrage.geraeteschluessel.toByteArray())
        pruefeSignatur(Geraetenachrichten.anmelden("id-token"), anfrage.signatur.toByteArray())
        val erwartet = Sitzung("konto-1", "geraet-1", "token-1", jetzt + 3600)
        assertEquals(Angemeldet(erwartet, neuesKonto = true, freigeschaltet = true), ergebnis)
        assertEquals(erwartet, speicher.sitzung)
    }

    @Test
    fun `Die Nonce ist an den Geräteschlüssel gebunden`() {
        val a = anmeldung(Speicher())
        assertEquals(Geraetenachrichten.nonce(schluessel.oeffentlich), a.nonce)
        assertTrue(Geraetenachrichten.nonce(TestSchluessel().oeffentlich) != a.nonce, "anderer Schlüssel, gleiche Nonce")
    }

    // KRITISCH: Erneuern signiert Geräte-ID und die eigene Uhrzeit; der Server
    // nimmt nur Signaturen mit höchstens 2 Minuten Abweichung an.
    @Test
    fun `Erneuern signiert Geräte-ID und Zeitpunkt und behält das Konto`() {
        antwort = erneuernAntwort("token-neu")
        val geraetId = Geraetenachrichten.geraetId(schluessel.oeffentlich)
        val speicher = Speicher(Sitzung("konto-1", geraetId, "token-alt", jetzt - 10))

        val neu = runBlocking { anmeldung(speicher).erneuern() }

        val anfrage = SitzungErneuernRequest.parseFrom(anfragen.single().inhalt)
        assertEquals(geraetId, anfrage.geraetId)
        assertEquals(jetzt, anfrage.zeitpunkt.seconds)
        pruefeSignatur(Geraetenachrichten.sitzung(geraetId, jetzt), anfrage.signatur.toByteArray())
        assertEquals(Sitzung("konto-1", geraetId, "token-neu", jetzt + 3600), neu)
        assertEquals(neu, speicher.sitzung)
    }

    data class Ablauf(val name: String, val laeuftAbIn: Long, val erneuert: Boolean) {
        override fun toString() = name
    }

    // Zu jeder Erlaubnis (gemerkte Sitzung nehmen) der Ablehnungsfall (erneuern).
    @ParameterizedTest(name = "{0}")
    @MethodSource("ablaeufe")
    fun `Gültige Sitzung wird nur kurz vor dem Ablauf erneuert`(fall: Ablauf) {
        antwort = erneuernAntwort("token-neu")
        val geraetId = Geraetenachrichten.geraetId(schluessel.oeffentlich)
        val speicher = Speicher(Sitzung("konto-1", geraetId, "token-alt", jetzt + fall.laeuftAbIn))

        val sitzung = runBlocking { anmeldung(speicher).sitzung() }

        assertEquals(if (fall.erneuert) "token-neu" else "token-alt", sitzung.token)
        assertEquals(if (fall.erneuert) 1 else 0, anfragen.size)
    }

    @Test
    fun `Ohne gemerkte Sitzung kein Aufruf, sondern nicht angemeldet`() {
        val a = anmeldung(Speicher())
        assertTrue(runCatching { runBlocking { a.sitzung() } }.exceptionOrNull() is NichtAngemeldet)
        assertTrue(runCatching { runBlocking { a.erneuern() } }.exceptionOrNull() is NichtAngemeldet)
        assertEquals(0, anfragen.size)
    }

    data class Fehlerfall(
        val name: String,
        val antwort: Any,
        val erwartet: KClass<out Throwable>,
        val erneuern: Boolean,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Nur Fehler des Dienstes selbst werden umgedeutet. Ein 403 als HTML
    // (Kill-Switch hat gesperrt) ist kein gesperrtes Gerät; sonst würfe die App
    // ihren Geräteschlüssel weg. Nach jedem Fehler bleibt die gemerkte Sitzung.
    @ParameterizedTest(name = "{0}")
    @MethodSource("fehlerfaelle")
    fun `Fehler des Servers`(fall: Fehlerfall) {
        antwort = fall.antwort as Antwort
        val geraetId = Geraetenachrichten.geraetId(schluessel.oeffentlich)
        val vorher = Sitzung("konto-1", geraetId, "token-alt", jetzt - 10)
        val speicher = Speicher(vorher)
        val a = anmeldung(speicher)

        val e = runCatching { runBlocking { if (fall.erneuern) a.erneuern() else a.anmelden("id-token") } }.exceptionOrNull()

        assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
        assertEquals(vorher, speicher.sitzung)
    }

    companion object {
        @JvmStatic
        fun ablaeufe() = listOf(
            Ablauf("gilt noch eine Stunde: gemerkte Sitzung", laeuftAbIn = 3600, erneuert = false),
            Ablauf("gilt noch 5 Minuten: gemerkte Sitzung", laeuftAbIn = 300, erneuert = false),
            Ablauf("läuft in einer Minute ab: erneuert", laeuftAbIn = 60, erneuert = true),
            Ablauf("schon abgelaufen: erneuert", laeuftAbIn = -10, erneuert = true),
        )

        private val html403 = Antwort.Roh(403, "text/html", "<html>Forbidden</html>")

        @JvmStatic
        fun fehlerfaelle() = listOf(
            Fehlerfall("Anmelden abgelehnt", Antwort.Fehler("unauthenticated"), AnmeldungAbgelehnt::class, erneuern = false),
            Fehlerfall("Anmelden: Gerät gesperrt", Antwort.Fehler("permission_denied"), GeraetGesperrt::class, erneuern = false),
            Fehlerfall("Anmelden: Tageslimit bleibt Serverfehler", Antwort.Fehler("resource_exhausted"), Serverfehler::class, erneuern = false),
            Fehlerfall("Anmelden: 403 vom gesperrten Dienst bleibt Serverfehler", html403, Serverfehler::class, erneuern = false),
            Fehlerfall("Erneuern abgelehnt", Antwort.Fehler("unauthenticated"), AnmeldungAbgelehnt::class, erneuern = true),
            Fehlerfall("Erneuern: Gerät gesperrt", Antwort.Fehler("permission_denied"), GeraetGesperrt::class, erneuern = true),
            Fehlerfall("Erneuern: vorübergehend nicht erreichbar bleibt Serverfehler", Antwort.Fehler("unavailable"), Serverfehler::class, erneuern = true),
            Fehlerfall("Erneuern: 403 vom gesperrten Dienst bleibt Serverfehler", html403, Serverfehler::class, erneuern = true),
        )
    }
}
