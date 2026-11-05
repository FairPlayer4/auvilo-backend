package com.avlreminder.verbindung

import com.avlreminder.protokoll.DiagnoseNichtFreigeschaltet
import com.avlreminder.protokoll.DiagnoseTageslimit
import com.avlreminder.protokoll.SicherungZuGross
import com.avlreminder.protokoll.VersionVeraltet
import com.avlreminder.schnittstelle.v1.BackupHolenResponse
import com.avlreminder.schnittstelle.v1.BackupLoeschenResponse
import com.avlreminder.schnittstelle.v1.BackupSpeichernRequest
import com.avlreminder.schnittstelle.v1.BackupSpeichernResponse
import com.avlreminder.schnittstelle.v1.DiagnoseSendenRequest
import com.avlreminder.schnittstelle.v1.DiagnoseSendenResponse
import com.avlreminder.schnittstelle.v1.SicherungHolenResponse
import com.avlreminder.schnittstelle.v1.SicherungLoeschenResponse
import com.avlreminder.schnittstelle.v1.SicherungSpeichernRequest
import com.avlreminder.schnittstelle.v1.SicherungSpeichernResponse
import com.connectrpc.Code
import com.google.protobuf.ByteString
import com.google.protobuf.MessageLite
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import java.net.InetSocketAddress
import kotlin.reflect.KClass

/**
 * Prüft die Adapter gegen einen Nachbau des Servers, der Connect spricht (Unary:
 * POST mit der Nachricht als Protobuf; Fehler als JSON mit Code). So ist geprüft,
 * dass jeder Fehlercode aus der Schnittstelle bei der richtigen Ausnahme landet.
 */
class AdapterTest {

    /** Was der Nachbau als Nächstes antwortet: eine Nachricht oder ein Fehlercode. */
    private sealed interface Antwort {
        data class Ok(val nachricht: MessageLite) : Antwort
        data class Fehler(val code: Code) : Antwort
        /** Eine Antwort ohne Connect-Fehler, etwa eine HTML-Seite von einem Proxy. */
        data class Roh(val status: Int, val typ: String, val text: String) : Antwort
    }

    /** Eine Anfrage, wie sie beim Nachbau ankam. */
    private class Anfrage(val pfad: String, val autorisierung: String?, val inhalt: ByteArray)

    private lateinit var server: HttpServer
    private var antwort: Antwort = Antwort.Fehler(Code.INTERNAL_ERROR)
    private val anfragen = mutableListOf<Anfrage>()

    @BeforeEach
    fun starten() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/") { ex ->
            anfragen += Anfrage(ex.requestURI.path, ex.requestHeaders.getFirst("Authorization"), ex.requestBody.readAllBytes())
            val (status, typ, inhalt) = when (val a = antwort) {
                is Antwort.Ok -> Triple(200, "application/proto", a.nachricht.toByteArray())
                // Der Status ist für den Client zweitrangig; maßgeblich ist der Code im JSON.
                is Antwort.Fehler -> Triple(400, "application/json", """{"code":"${a.code.codeName}","message":"nachgebaut"}""".toByteArray())
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

    private fun verbindung() = Verbindung("http://127.0.0.1:${server.address.port}")

    data class Fall(
        val name: String,
        val fehler: Code,
        val aufruf: suspend (Verbindung) -> Any?,
        /** Die erwartete Ausnahme, oder null für „kein Fehler, Ergebnis null“. */
        val erwartet: KClass<out Throwable>?,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Nur die Codes aus der Schnittstelle werden umgedeutet; alles andere
    // (Sitzung ungültig, Server überlastet) bleibt ein Serverfehler. Sonst hielte
    // der Ablauf etwa einen Netzfehler für „keine Sicherung“ und spielte nichts ein.
    @ParameterizedTest(name = "{0}")
    @MethodSource("fehlerfaelle")
    fun `übersetzt Fehlercodes`(fall: Fall) {
        antwort = Antwort.Fehler(fall.fehler)
        val ergebnis = runCatching { runBlocking { fall.aufruf(verbindung()) } }
        if (fall.erwartet == null) {
            assertNull(ergebnis.getOrThrow())
        } else {
            val e = ergebnis.exceptionOrNull()
            assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
            if (e is Serverfehler) assertEquals(fall.fehler, e.code)
        }
    }

    // KRITISCH: Ein 404 ohne Connect-Code (falsche Adresse, Proxy) darf nicht als
    // „keine Sicherung“ gelten, sonst richtete ein Gerät sich neu ein.
    @ParameterizedTest(name = "{0}")
    @MethodSource("rohfaelle")
    fun `Antworten ohne Connect-Fehler bleiben Serverfehler`(fall: RohFall) {
        antwort = Antwort.Roh(fall.status, fall.typ, fall.text)
        val e = runCatching { runBlocking { fall.aufruf(verbindung()) } }.exceptionOrNull()
        assertTrue(e is Serverfehler, "erwartet Serverfehler, bekommen $e")
    }

    data class RohFall(
        val name: String,
        val status: Int,
        val typ: String,
        val text: String,
        val aufruf: suspend (Verbindung) -> Any?,
    ) {
        override fun toString() = name
    }

    @Test
    fun `speichert die Sicherung mit Sitzung und erwarteter Version`() {
        antwort = Antwort.Ok(SicherungSpeichernResponse.newBuilder().setVersion(42).build())
        val version = runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).speichern(CHIFFRAT, 7) }
        assertEquals(42, version)
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.DatensicherungDienst/SicherungSpeichern", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        val gesendet = SicherungSpeichernRequest.parseFrom(a.inhalt)
        assertArrayEquals(CHIFFRAT, gesendet.chiffrat.toByteArray())
        assertEquals(7, gesendet.erwarteteVersion)
    }

    @Test
    fun `holt die Sicherung`() {
        antwort = Antwort.Ok(SicherungHolenResponse.newBuilder().setChiffrat(ByteString.copyFrom(CHIFFRAT)).setVersion(9).build())
        val s = runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).holen() }!!
        assertArrayEquals(CHIFFRAT, s.chiffrat)
        assertEquals(9, s.version)
        assertEquals("/avlreminder.v1.DatensicherungDienst/SicherungHolen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `löscht die Sicherung`() {
        antwort = Antwort.Ok(SicherungLoeschenResponse.getDefaultInstance())
        runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).loeschen() }
        assertEquals("/avlreminder.v1.DatensicherungDienst/SicherungLoeschen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `speichert das Schlüssel-Backup mit Sitzung und erwarteter Version`() {
        antwort = Antwort.Ok(BackupSpeichernResponse.newBuilder().setVersion(3).build())
        val version = runBlocking { ServerBackupAblage(verbindung(), TOKEN).speichern(CHIFFRAT, 2) }
        assertEquals(3, version)
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.SchluesselDienst/BackupSpeichern", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        val gesendet = BackupSpeichernRequest.parseFrom(a.inhalt)
        assertArrayEquals(CHIFFRAT, gesendet.chiffrat.toByteArray())
        assertEquals(2, gesendet.erwarteteVersion)
    }

    @Test
    fun `holt das Schlüssel-Backup`() {
        antwort = Antwort.Ok(BackupHolenResponse.newBuilder().setChiffrat(ByteString.copyFrom(CHIFFRAT)).setVersion(5).build())
        val b = runBlocking { ServerBackupAblage(verbindung(), TOKEN).holen() }!!
        assertArrayEquals(CHIFFRAT, b.chiffrat)
        assertEquals(5, b.version)
    }

    @Test
    fun `löscht das Schlüssel-Backup`() {
        antwort = Antwort.Ok(BackupLoeschenResponse.getDefaultInstance())
        runBlocking { ServerBackupAblage(verbindung(), TOKEN).loeschen() }
        assertEquals("/avlreminder.v1.SchluesselDienst/BackupLoeschen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `sendet die Diagnose mit Sitzung`() {
        antwort = Antwort.Ok(DiagnoseSendenResponse.getDefaultInstance())
        runBlocking { ServerDiagnoseAblage(verbindung(), TOKEN).senden(CHIFFRAT) }
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.DiagnoseDienst/DiagnoseSenden", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        assertArrayEquals(CHIFFRAT, DiagnoseSendenRequest.parseFrom(a.inhalt).nutzlast.toByteArray())
    }

    companion object {
        private const val TOKEN = "test-token"
        private val CHIFFRAT = byteArrayOf(1, 2, 3, 0, -1)

        private fun sicherung(v: Verbindung) = ServerSicherungsAblage(v, TOKEN)
        private fun backup(v: Verbindung) = ServerBackupAblage(v, TOKEN)
        private suspend fun diagnose(v: Verbindung): Any? = ServerDiagnoseAblage(v, TOKEN).senden(CHIFFRAT).let { "gesendet" }

        @JvmStatic
        fun fehlerfaelle() = listOf(
            // Datensicherung
            Fall("Sicherung holen: nicht gefunden heißt keine Sicherung", Code.NOT_FOUND, { sicherung(it).holen() }, null),
            Fall("Sicherung holen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { sicherung(it).holen() }, Serverfehler::class),
            Fall("Sicherung holen: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { sicherung(it).holen() }, Serverfehler::class),
            Fall("Sicherung speichern: Bedingung verletzt heißt veraltet", Code.FAILED_PRECONDITION, { sicherung(it).speichern(CHIFFRAT, 1) }, VersionVeraltet::class),
            Fall("Sicherung speichern: zu groß", Code.RESOURCE_EXHAUSTED, { sicherung(it).speichern(CHIFFRAT, 1) }, SicherungZuGross::class),
            Fall("Sicherung speichern: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung speichern: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung speichern: ungültige Anfrage bleibt Serverfehler", Code.INVALID_ARGUMENT, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung löschen: Fehler bleibt Serverfehler", Code.UNAUTHENTICATED, { sicherung(it).loeschen() }, Serverfehler::class),
            Fall("Sicherung löschen: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { sicherung(it).loeschen() }, Serverfehler::class),
            // Schlüssel-Backup
            Fall("Backup holen: nicht gefunden heißt kein Backup", Code.NOT_FOUND, { backup(it).holen() }, null),
            Fall("Backup holen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { backup(it).holen() }, Serverfehler::class),
            Fall("Backup speichern: Bedingung verletzt heißt veraltet", Code.FAILED_PRECONDITION, { backup(it).speichern(CHIFFRAT, 1) }, VersionVeraltet::class),
            Fall("Backup speichern: zu groß bleibt Serverfehler", Code.RESOURCE_EXHAUSTED, { backup(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Backup speichern: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { backup(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Backup löschen: Fehler bleibt Serverfehler", Code.UNAUTHENTICATED, { backup(it).loeschen() }, Serverfehler::class),
            // Diagnose
            Fall("Diagnose senden: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { diagnose(it) }, DiagnoseNichtFreigeschaltet::class),
            Fall("Diagnose senden: erschöpft heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { diagnose(it) }, DiagnoseTageslimit::class),
            Fall("Diagnose senden: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { diagnose(it) }, Serverfehler::class),
            Fall("Diagnose senden: ungültige Anfrage bleibt Serverfehler", Code.INVALID_ARGUMENT, { diagnose(it) }, Serverfehler::class),
            Fall("Diagnose senden: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { diagnose(it) }, Serverfehler::class),
        )

        private const val HTML_404 = "<html><body>404 Not Found</body></html>"

        @JvmStatic
        fun rohfaelle() = listOf(
            RohFall("Sicherung holen: HTML-Seite mit 404", 404, "text/html", HTML_404) { sicherung(it).holen() },
            RohFall("Sicherung holen: Text mit 503", 503, "text/plain", "Service Unavailable") { sicherung(it).holen() },
            RohFall("Backup holen: HTML-Seite mit 404", 404, "text/html", HTML_404) { backup(it).holen() },
            RohFall("Backup holen: Text mit 503", 503, "text/plain", "Service Unavailable") { backup(it).holen() },
            // Ein 403 ohne Connect-Fehler deutet Connect selbst als permission_denied
            // (bekannte Grenze, siehe ServerDiagnoseAblage); darum hier 503.
            RohFall("Diagnose senden: Text mit 503", 503, "text/plain", "Service Unavailable") { diagnose(it) },
        )
    }
}
