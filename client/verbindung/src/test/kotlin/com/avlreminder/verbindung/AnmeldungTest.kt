package com.avlreminder.verbindung

import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.protokoll.KontoGeraet
import com.avlreminder.schnittstelle.v1.AnmeldenResponse
import com.avlreminder.schnittstelle.v1.Geraet
import com.avlreminder.schnittstelle.v1.GeraeteAuflistenResponse
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

    private class Anfrage(val pfad: String, val inhalt: ByteArray, val token: String?)

    private lateinit var server: HttpServer
    private var antwort: Antwort = Antwort.Fehler("internal")

    /** Antworten der Reihe nach; sind sie aufgebraucht, gilt [antwort]. */
    private val folge = ArrayDeque<Antwort>()
    private val anfragen = mutableListOf<Anfrage>()

    @BeforeEach
    fun starten() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { ex ->
            anfragen += Anfrage(ex.requestURI.path, ex.requestBody.readAllBytes(), ex.requestHeaders.getFirst("Authorization"))
            val (status, typ, inhalt) = when (val a = folge.removeFirstOrNull() ?: antwort) {
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

        val a = anmeldung(speicher)
        val versuch = a.versuch()
        val ergebnis = runBlocking { a.anmelden(versuch, "id-token") }

        val anfrage = AnmeldenRequest.parseFrom(anfragen.single().inhalt)
        assertArrayEquals(versuch.zufall, anfrage.nonceZufall.toByteArray())
        assertEquals("/avlreminder.v1.AnmeldeDienst/Anmelden", anfragen.single().pfad)
        assertEquals("id-token", anfrage.googleIdToken)
        assertArrayEquals(schluessel.oeffentlich, anfrage.geraeteschluessel.toByteArray())
        pruefeSignatur(Geraetenachrichten.anmelden("id-token"), anfrage.signatur.toByteArray())
        val erwartet = Sitzung("konto-1", "geraet-1", "token-1", jetzt + 3600)
        assertEquals(Angemeldet(erwartet, neuesKonto = true, freigeschaltet = true), ergebnis)
        assertEquals(erwartet, speicher.sitzung)
    }

    @Test
    fun `Die Nonce ist an den Geräteschlüssel und den Zufall des Versuchs gebunden`() {
        val v = anmeldung(Speicher()).versuch()
        assertEquals(Geraetenachrichten.nonce(schluessel.oeffentlich, v.zufall), v.nonce)
        assertTrue(Geraetenachrichten.nonce(TestSchluessel().oeffentlich, v.zufall) != v.nonce, "anderer Schlüssel, gleiche Nonce")
    }

    // KRITISCH: Jeder Versuch hat eine eigene Nonce; sonst gibt Google ein
    // zwischengespeichertes, für den Server zu altes ID-Token zurück.
    @Test
    fun `Jeder Versuch hat eine neue Nonce`() {
        val a = anmeldung(Speicher())
        val nonces = List(20) { a.versuch().nonce }.toSet()
        assertEquals(20, nonces.size)
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

        val e = runCatching { runBlocking { if (fall.erneuern) a.erneuern() else a.anmelden(a.versuch(), "id-token") } }.exceptionOrNull()

        assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
        assertEquals(vorher, speicher.sitzung)
    }

    data class Abgelehnt(
        val name: String,
        val antworten: List<Any>,
        /** null = der Aufruf gelingt. */
        val erwartet: KClass<out Throwable>?,
        val anfragen: Int,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Lehnt der Dienst die Sitzung ab, erneuert mitSitzung genau einmal; erst
    // das Erneuern sagt, ob das Gerät gesperrt ist. Zu „erneuert und ruft erneut auf“
    // die Fälle, in denen es nicht erneuert oder aufgibt.
    @ParameterizedTest(name = "{0}")
    @MethodSource("abgelehnteSitzungen")
    fun `Aufruf mit Sitzung`(fall: Abgelehnt) {
        fall.antworten.forEach { folge += it as Antwort }
        val geraetId = Geraetenachrichten.geraetId(schluessel.oeffentlich)
        val a = anmeldung(Speicher(Sitzung("konto-1", geraetId, "token-alt", jetzt + 3600)))
        val verbindung = Verbindung("http://127.0.0.1:${server.address.port}")

        val ergebnis = runCatching { runBlocking { a.mitSitzung { ServerGeraete(verbindung, it.token).auflisten() } } }

        if (fall.erwartet == null) {
            assertEquals(listOf(KontoGeraet("g-1", gesperrt = false, diesesGeraet = true)), ergebnis.getOrThrow())
            assertEquals(if (fall.anfragen > 1) "Bearer token-neu" else "Bearer token-alt", anfragen.last().token)
        } else {
            val e = ergebnis.exceptionOrNull()
            assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
        }
        assertEquals(fall.anfragen, anfragen.size)
        assertEquals("Bearer token-alt", anfragen.first().token)
    }

    companion object {
        private val liste = Antwort.Ok(
            GeraeteAuflistenResponse.newBuilder().addGeraete(Geraet.newBuilder().setGeraetId("g-1").setDiesesGeraet(true)).build(),
        )
        private val abgelehnt = Antwort.Fehler("unauthenticated")
        private val erneuert = Antwort.Ok(
            SitzungErneuernResponse.newBuilder()
                .setSitzung(SitzungNachricht.newBuilder().setToken("token-neu").setLaeuftAb(Timestamp.newBuilder().setSeconds(1_793_538_000L)))
                .build(),
        )

        @JvmStatic
        fun abgelehnteSitzungen() = listOf(
            Abgelehnt("Sitzung gilt: ein Aufruf, kein Erneuern", listOf(liste), erwartet = null, anfragen = 1),
            Abgelehnt(
                "Sitzung abgelehnt: erneuert und ruft mit dem neuen Token erneut auf",
                listOf(abgelehnt, erneuert, liste),
                erwartet = null,
                anfragen = 3,
            ),
            Abgelehnt("Sitzung abgelehnt, Gerät gesperrt", listOf(abgelehnt, Antwort.Fehler("permission_denied")), GeraetGesperrt::class, anfragen = 2),
            Abgelehnt("Sitzung abgelehnt, Erneuern abgelehnt", listOf(abgelehnt, abgelehnt), AnmeldungAbgelehnt::class, anfragen = 2),
            Abgelehnt(
                "nach dem Erneuern wieder abgelehnt: gibt auf",
                listOf(abgelehnt, erneuert, abgelehnt),
                SitzungAbgelehnt::class,
                anfragen = 3,
            ),
            Abgelehnt("401 ohne Connect-Fehler (vor dem Dienst): kein Erneuern", listOf(Antwort.Roh(401, "text/html", "<html>nein</html>")), Serverfehler::class, anfragen = 1),
            Abgelehnt("anderer Fehler des Dienstes: kein Erneuern", listOf(Antwort.Fehler("unavailable")), Serverfehler::class, anfragen = 1),
        )

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
