package com.avlreminder.verbindung

import com.avlreminder.protokoll.Datensicherung
import com.avlreminder.protokoll.DiagnoseNichtFreigeschaltet
import com.avlreminder.protokoll.DiagnoseTageslimit
import com.avlreminder.protokoll.DiagnoseZuGross
import com.avlreminder.protokoll.GeraetNichtFreigeschaltet
import com.avlreminder.protokoll.KontoGeraet
import com.avlreminder.protokoll.KontoLoeschung
import com.avlreminder.protokoll.LoeschungLaeuft
import com.avlreminder.protokoll.SicherungTageslimit
import com.avlreminder.protokoll.SicherungZuGross
import com.avlreminder.protokoll.VersionVeraltet
import com.avlreminder.schnittstelle.v1.BackupHolenResponse
import com.avlreminder.schnittstelle.v1.BackupLoeschenResponse
import com.avlreminder.schnittstelle.v1.BackupSpeichernRequest
import com.avlreminder.schnittstelle.v1.BackupSpeichernResponse
import com.avlreminder.schnittstelle.v1.DiagnoseSendenRequest
import com.avlreminder.schnittstelle.v1.DiagnoseSendenResponse
import com.avlreminder.schnittstelle.v1.GeraetFreischaltenRequest
import com.avlreminder.schnittstelle.v1.GeraetFreischaltenResponse
import com.avlreminder.schnittstelle.v1.GeraetSperrenRequest
import com.avlreminder.schnittstelle.v1.GeraetSperrenResponse
import com.avlreminder.schnittstelle.v1.Geraet
import com.avlreminder.schnittstelle.v1.GeraeteAuflistenResponse
import com.avlreminder.schnittstelle.v1.KontoLoeschenAbbrechenResponse
import com.avlreminder.schnittstelle.v1.KontoLoeschenResponse
import com.avlreminder.schnittstelle.v1.MitWiederherstellungFreischaltenRequest
import com.avlreminder.schnittstelle.v1.MitWiederherstellungFreischaltenResponse
import com.avlreminder.schnittstelle.v1.FassungenAuflistenResponse
import com.avlreminder.schnittstelle.v1.SicherungHolenRequest
import com.avlreminder.schnittstelle.v1.SicherungHolenResponse
import com.avlreminder.schnittstelle.v1.SicherungsFassung
import com.avlreminder.schnittstelle.v1.SicherungLoeschenResponse
import com.avlreminder.schnittstelle.v1.SicherungSpeichernRequest
import com.avlreminder.schnittstelle.v1.SicherungSpeichernResponse
import com.connectrpc.Code
import com.google.protobuf.ByteString
import com.google.protobuf.MessageLite
import com.google.protobuf.Timestamp
import com.sun.net.httpserver.HttpServer
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import org.junit.jupiter.api.AfterEach
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource
import org.junit.jupiter.params.provider.ValueSource
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.net.InetSocketAddress
import java.time.Instant
import java.util.concurrent.TimeUnit
import java.util.zip.GZIPOutputStream
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

        /** Rohe Bytes als Erfolg, wahlweise gzip-gepackt und ohne Längenangabe. */
        class Bytes(val inhalt: ByteArray, val gzip: Boolean = false, val ohneLaenge: Boolean = false) : Antwort

        /** Antwortet erst nach [ms] Millisekunden. */
        data class Spaet(val ms: Long) : Antwort
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
            val a = antwort
            if (a is Antwort.Spaet) Thread.sleep(a.ms)
            if (a is Antwort.Bytes) {
                val inhalt = if (a.gzip) gzip(a.inhalt) else a.inhalt
                ex.responseHeaders.add("Content-Type", "application/proto")
                if (a.gzip) ex.responseHeaders.add("Content-Encoding", "gzip")
                // Länge 0 heißt bei HttpServer: chunked, ohne Längenangabe.
                ex.sendResponseHeaders(200, if (a.ohneLaenge) 0 else inhalt.size.toLong())
                runCatching { ex.responseBody.use { it.write(inhalt) } } // der Client darf vorher auflegen
                return@createContext
            }
            val (status, typ, inhalt) = when (a) {
                is Antwort.Ok -> Triple(200, "application/proto", a.nachricht.toByteArray())
                // Der Status ist für den Client zweitrangig; maßgeblich ist der Code im JSON.
                is Antwort.Fehler -> Triple(400, "application/json", """{"code":"${a.code.codeName}","message":"nachgebaut"}""".toByteArray())
                is Antwort.Roh -> Triple(a.status, a.typ, a.text.toByteArray())
                is Antwort.Bytes, is Antwort.Spaet -> Triple(200, "application/proto", ByteArray(0))
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
    // „keine Sicherung“ gelten, sonst richtete ein Gerät sich neu ein. Ein 403 ohne
    // Connect-Code (Kill-Switch) darf nicht als „nicht freigeschaltet“ gelten, sonst
    // führte die App ein freigeschaltetes Gerät in die Freischaltung.
    @ParameterizedTest(name = "{0}")
    @MethodSource("rohfaelle")
    fun `Antworten ohne Connect-Fehler bleiben Serverfehler`(fall: RohFall) {
        antwort = Antwort.Roh(fall.status, fall.typ, fall.text)
        val e = runCatching { runBlocking { fall.aufruf(verbindung()) } }.exceptionOrNull()
        assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
        if (e is Serverfehler) assertEquals(false, e.vomDienst, "Antwort ohne Connect-Fehler gilt als vom Dienst")
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("connectFehlerMit403")
    fun `Connect-Fehler mit 403 heißt weiter nicht freigeschaltet`(fall: RohFall) {
        antwort = Antwort.Roh(fall.status, fall.typ, fall.text)
        val e = runCatching { runBlocking { fall.aufruf(verbindung()) } }.exceptionOrNull()
        assertTrue(fall.erwartet.isInstance(e), "erwartet ${fall.erwartet.simpleName}, bekommen $e")
    }

    data class RohFall(
        val name: String,
        val status: Int,
        val typ: String,
        val text: String,
        val erwartet: KClass<out Throwable> = Serverfehler::class,
        val aufruf: suspend (Verbindung) -> Any?,
    ) {
        override fun toString() = name
    }

    // KRITISCH: Ohne Netz (Flugmodus, Server nicht erreichbar) kommt kein Serverfehler,
    // sondern eine IOException. Sonst meldete die App „Server gesperrt oder gestört“, und
    // der Backend-Logger zählte einen Netzfehler als Serverfehler.
    @ParameterizedTest(name = "{0}")
    @MethodSource("aufrufe")
    fun `ohne Netz kommt eine IOException`(fall: Aufruf) {
        server.stop(0) // der Port ist danach zu: Verbindung abgelehnt
        val e = runCatching { runBlocking { fall.aufruf(verbindung()) } }.exceptionOrNull()
        assertTrue(e is IOException, "erwartet IOException, bekommen $e")
    }

    // KRITISCH: Die größte erlaubte Sicherung kommt an, auch gzip-gepackt und
    // ohne Längenangabe; die Obergrenze für Antworten (E9) darf sie nie abschneiden.
    @ParameterizedTest(name = "{0}")
    @MethodSource("groessteSicherung")
    fun `die größte Sicherung kommt an`(fall: GrossFall) {
        val chiffrat = ByteArray(Datensicherung.MAX_CHIFFRAT) { (it % 251).toByte() }
        val nachricht = SicherungHolenResponse.newBuilder().setChiffrat(ByteString.copyFrom(chiffrat)).setVersion(4).build()
        antwort = Antwort.Bytes(nachricht.toByteArray(), fall.gzip, fall.ohneLaenge)
        val s = runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).holen() }!!
        assertArrayEquals(chiffrat, s.chiffrat)
        assertEquals(4, s.version)
    }

    // KRITISCH: Eine zu große Antwort bricht ab, statt den Speicher der App zu
    // füllen (E9) – auch eine kleine gzip-Antwort, die entpackt zu groß wird.
    @ParameterizedTest(name = "{0}")
    @MethodSource("zuGross")
    fun `eine zu große Antwort bricht ab`(fall: GrossFall) {
        antwort = Antwort.Bytes(ByteArray(fall.groesse), fall.gzip, fall.ohneLaenge)
        val e = runCatching { runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).holen() } }.exceptionOrNull()
        assertTrue(e is AntwortZuGross, "erwartet AntwortZuGross, bekommen $e")
    }

    data class GrossFall(val name: String, val gzip: Boolean, val ohneLaenge: Boolean, val groesse: Int = 0) {
        override fun toString() = name
    }

    // Eigene Zeitlimits bleiben erhalten, und ein Server, der zu lange schweigt,
    // heißt für die App „kein Netz“ (IOException), nicht „Server gestört“.
    @Test
    fun `ein Server, der zu lange schweigt, endet als IOException`() {
        antwort = Antwort.Spaet(2_000)
        val kurz = OkHttpClient.Builder().readTimeout(200, TimeUnit.MILLISECONDS).build()
        val v = Verbindung("http://127.0.0.1:${server.address.port}", kurz)
        val e = runCatching { runBlocking { ServerSicherungsAblage(v, TOKEN).holen() } }.exceptionOrNull()
        assertTrue(e is IOException && e !is AntwortZuGross, "erwartet IOException, bekommen $e")
    }

    data class Aufruf(val name: String, val aufruf: suspend (Verbindung) -> Any?) {
        override fun toString() = name
    }

    // KRITISCH (R2): Jeder Aufruf holt das Token neu. Mit einem festen Token
    // scheiterte nach langer Eingabe der zweite Aufruf an der abgelaufenen Sitzung.
    @ParameterizedTest(name = "{0}")
    @MethodSource("tokenFaelle")
    fun `jeder Aufruf holt das Token neu`(fall: TokenFall) {
        antwort = Antwort.Ok(fall.antwort)
        val tokens = ArrayDeque(listOf("erstes", "zweites"))
        runBlocking { repeat(2) { fall.aufruf(verbindung()) { tokens.removeFirst() } } }
        assertEquals(listOf("Bearer erstes", "Bearer zweites"), anfragen.map { it.autorisierung })
    }

    data class TokenFall(val name: String, val antwort: MessageLite, val aufruf: suspend (Verbindung, suspend () -> String) -> Any?) {
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
        assertEquals(0, SicherungHolenRequest.parseFrom(anfragen.single().inhalt).version, "ohne Version: die neueste")
    }

    // R-4: Eine ältere Fassung kommt über ihre Version.
    @Test
    fun `holt eine ältere Fassung über ihre Version`() {
        antwort = Antwort.Ok(SicherungHolenResponse.newBuilder().setChiffrat(ByteString.copyFrom(CHIFFRAT)).setVersion(7).build())
        val s = runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).holen(7) }!!
        assertArrayEquals(CHIFFRAT, s.chiffrat)
        assertEquals(7, s.version)
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.DatensicherungDienst/SicherungHolen", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        assertEquals(7, SicherungHolenRequest.parseFrom(a.inhalt).version)
    }

    // KRITISCH: Version 0 hieße beim Server „die neueste“; wer eine bestimmte Fassung
    // will, bekommt nie stillschweigend eine andere.
    @ParameterizedTest(name = "Version {0}")
    @ValueSource(longs = [0, -1])
    fun `eine Version unter 1 wird gar nicht erst gesendet`(version: Long) {
        val e = runCatching { runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).holen(version) } }.exceptionOrNull()
        assertTrue(e is IllegalArgumentException, "Ausnahme: $e")
        assertTrue(anfragen.isEmpty())
    }

    @Test
    fun `listet die Fassungen mit Version, Zeit und Größe`() {
        val zeit = Timestamp.newBuilder().setSeconds(1_794_000_000).setNanos(123_000_000).build()
        antwort = Antwort.Ok(
            FassungenAuflistenResponse.newBuilder()
                .addFassungen(SicherungsFassung.newBuilder().setVersion(20).setGespeichertAm(zeit).setBytes(300))
                .addFassungen(SicherungsFassung.newBuilder().setVersion(10).setBytes(200))
                .build(),
        )
        val liste = runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).fassungen() }
        assertEquals(
            listOf(
                com.avlreminder.protokoll.SicherungsFassung(20, Instant.ofEpochSecond(1_794_000_000, 123_000_000), 300),
                com.avlreminder.protokoll.SicherungsFassung(10, Instant.EPOCH, 200),
            ),
            liste,
        )
        assertEquals("/avlreminder.v1.DatensicherungDienst/FassungenAuflisten", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `ohne Sicherung ist die Liste leer`() {
        antwort = Antwort.Ok(FassungenAuflistenResponse.getDefaultInstance())
        assertEquals(emptyList<Any>(), runBlocking { ServerSicherungsAblage(verbindung(), TOKEN).fassungen() })
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
        val version = runBlocking { ServerBackupAblage(verbindung(), TOKEN).speichern(CHIFFRAT, SCHLUESSEL, 2) }
        assertEquals(3, version)
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.SchluesselDienst/BackupSpeichern", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        val gesendet = BackupSpeichernRequest.parseFrom(a.inhalt)
        assertArrayEquals(CHIFFRAT, gesendet.chiffrat.toByteArray())
        assertArrayEquals(SCHLUESSEL, gesendet.freischaltschluessel.toByteArray())
        assertEquals(2, gesendet.erwarteteVersion)
    }

    @Test
    fun `schaltet sich mit Sitzung und Signatur frei`() {
        antwort = Antwort.Ok(MitWiederherstellungFreischaltenResponse.getDefaultInstance())
        runBlocking { ServerGeraete(verbindung(), TOKEN).mitWiederherstellung(CHIFFRAT) }
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.GeraeteDienst/MitWiederherstellungFreischalten", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        assertArrayEquals(CHIFFRAT, MitWiederherstellungFreischaltenRequest.parseFrom(a.inhalt).signatur.toByteArray())
    }

    @Test
    fun `schaltet ein anderes Gerät mit Sitzung frei`() {
        antwort = Antwort.Ok(GeraetFreischaltenResponse.getDefaultInstance())
        runBlocking { ServerGeraete(verbindung(), TOKEN).freischalten("geraet-2") }
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.GeraeteDienst/GeraetFreischalten", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        assertEquals("geraet-2", GeraetFreischaltenRequest.parseFrom(a.inhalt).geraetId)
    }

    @Test
    fun `sperrt ein Gerät mit Sitzung`() {
        antwort = Antwort.Ok(GeraetSperrenResponse.getDefaultInstance())
        runBlocking { ServerGeraete(verbindung(), TOKEN).sperren("geraet-1") }
        val a = anfragen.single()
        assertEquals("/avlreminder.v1.GeraeteDienst/GeraetSperren", a.pfad)
        assertEquals("Bearer $TOKEN", a.autorisierung)
        assertEquals("geraet-1", GeraetSperrenRequest.parseFrom(a.inhalt).geraetId)
    }

    @Test
    fun `holt das Schlüssel-Backup`() {
        antwort = Antwort.Ok(BackupHolenResponse.newBuilder().setChiffrat(ByteString.copyFrom(CHIFFRAT)).setVersion(5).build())
        val b = runBlocking { ServerBackupAblage(verbindung(), TOKEN).holen() }!!
        assertArrayEquals(CHIFFRAT, b.chiffrat)
        assertEquals(5, b.version)
        // Backup- und Sicherungsantwort sind feldgleich; nur der Pfad zeigt, dass
        // der richtige Dienst gefragt wurde.
        assertEquals("/avlreminder.v1.SchluesselDienst/BackupHolen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `löscht das Schlüssel-Backup`() {
        antwort = Antwort.Ok(BackupLoeschenResponse.getDefaultInstance())
        runBlocking { ServerBackupAblage(verbindung(), TOKEN).loeschen() }
        assertEquals("/avlreminder.v1.SchluesselDienst/BackupLoeschen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    data class LoeschFall(val name: String, val antwort: KontoLoeschenResponse, val erwartet: Any) {
        override fun toString() = name
    }

    // KRITISCH (B1): Nur geloescht = true heißt gelöscht; danach verwirft die App ihre
    // Schlüssel. Eine Antwort ohne beides ist ein Fehler, kein Löschen.
    @ParameterizedTest(name = "{0}")
    @MethodSource("loeschFaelle")
    fun `Konto löschen`(fall: LoeschFall) {
        antwort = Antwort.Ok(fall.antwort)
        val ergebnis = runCatching { runBlocking { ServerKontoLoeschen(verbindung(), TOKEN).loeschen() } }
        when (val erwartet = fall.erwartet) {
            is KClass<*> -> assertTrue(erwartet.isInstance(ergebnis.exceptionOrNull()), "erwartet ${erwartet.simpleName}, bekommen $ergebnis")
            else -> assertEquals(erwartet, ergebnis.getOrThrow())
        }
        assertEquals("/avlreminder.v1.KontoDienst/KontoLoeschen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @Test
    fun `bricht die Löschung mit Sitzung ab`() {
        antwort = Antwort.Ok(KontoLoeschenAbbrechenResponse.getDefaultInstance())
        runBlocking { ServerKontoLoeschen(verbindung(), TOKEN).abbrechen() }
        assertEquals("/avlreminder.v1.KontoDienst/KontoLoeschenAbbrechen", anfragen.single().pfad)
        assertEquals("Bearer $TOKEN", anfragen.single().autorisierung)
    }

    @ParameterizedTest(name = "mit Termin: {0}")
    @ValueSource(booleans = [true, false])
    fun `die Geräteliste zeigt den Termin der Löschung`(mitTermin: Boolean) {
        val termin = Instant.ofEpochSecond(1_794_139_200L, 7_000)
        antwort = Antwort.Ok(
            GeraeteAuflistenResponse.newBuilder()
                .addGeraete(Geraet.newBuilder().setGeraetId("g-1").setDiesesGeraet(true))
                .apply { if (mitTermin) setLoeschenAb(Timestamp.newBuilder().setSeconds(termin.epochSecond).setNanos(termin.nano)) }
                .build(),
        )
        val liste = runBlocking { ServerGeraete(verbindung(), TOKEN).liste() }
        assertEquals(Geraeteliste(listOf(KontoGeraet("g-1", gesperrt = false, diesesGeraet = true)), if (mitTermin) termin else null), liste)
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
        private val LOESCHEN_AB = Instant.ofEpochSecond(1_794_139_200L, 123_000_000)

        @JvmStatic
        fun loeschFaelle() = listOf(
            LoeschFall("gelöscht", KontoLoeschenResponse.newBuilder().setGeloescht(true).build(), KontoLoeschung.Geloescht),
            LoeschFall(
                "beantragt: mit Termin",
                KontoLoeschenResponse.newBuilder().setLoeschenAb(Timestamp.newBuilder().setSeconds(LOESCHEN_AB.epochSecond).setNanos(LOESCHEN_AB.nano)).build(),
                KontoLoeschung.Beantragt(LOESCHEN_AB),
            ),
            LoeschFall("weder gelöscht noch Termin: Fehler", KontoLoeschenResponse.getDefaultInstance(), IllegalStateException::class),
        )

        @JvmStatic
        fun tokenFaelle() = listOf(
            TokenFall("Schlüssel-Backup", BackupLoeschenResponse.getDefaultInstance()) { v, t -> ServerBackupAblage(v, t).loeschen() },
            TokenFall("Datensicherung", SicherungLoeschenResponse.getDefaultInstance()) { v, t -> ServerSicherungsAblage(v, t).loeschen() },
            TokenFall("Geräte", GeraetSperrenResponse.getDefaultInstance()) { v, t -> ServerGeraete(v, t).sperren("a".repeat(32)) },
            TokenFall("Konto", SicherungLoeschenResponse.getDefaultInstance()) { v, t -> ServerKonto(v, t).datensicherungLoeschen() },
            TokenFall("Diagnose", DiagnoseSendenResponse.getDefaultInstance()) { v, t -> ServerDiagnoseAblage(v, t).senden(ByteArray(1)) },
            TokenFall("Konto löschen", KontoLoeschenResponse.newBuilder().setGeloescht(true).build()) { v, t -> ServerKontoLoeschen(v, t).loeschen() },
            TokenFall("Löschung abbrechen", KontoLoeschenAbbrechenResponse.getDefaultInstance()) { v, t -> ServerKontoLoeschen(v, t).abbrechen() },
        )

        @JvmStatic
        fun aufrufe() = listOf(
            Aufruf("Sicherung holen") { ServerSicherungsAblage(it, TOKEN).holen() },
            Aufruf("Fassung holen") { ServerSicherungsAblage(it, TOKEN).holen(7) },
            Aufruf("Fassungen auflisten") { ServerSicherungsAblage(it, TOKEN).fassungen() },
            Aufruf("Sicherung speichern") { ServerSicherungsAblage(it, TOKEN).speichern(CHIFFRAT, 1) },
            Aufruf("Schlüssel-Backup holen") { ServerBackupAblage(it, TOKEN).holen() },
            Aufruf("Gerät sperren") { ServerGeraete(it, TOKEN).sperren("geraet-1") },
            Aufruf("Diagnose senden") { ServerDiagnoseAblage(it, TOKEN).senden(CHIFFRAT) },
            Aufruf("Konto löschen") { ServerKontoLoeschen(it, TOKEN).loeschen() },
            Aufruf("Löschung abbrechen") { ServerKontoLoeschen(it, TOKEN).abbrechen() },
        )

        private const val TOKEN = "test-token"
        private val CHIFFRAT = byteArrayOf(1, 2, 3, 0, -1)
        private val SCHLUESSEL = ByteArray(32) { it.toByte() }

        // Eigene Zahl statt Datensicherung.MAX_CHIFFRAT (datensicherung.proto: 8 MiB).
        private val UEBER_8_MIB = ByteArray(8 * 1024 * 1024 + 1)
        private val GENAU_8_MIB = ByteArray(8 * 1024 * 1024)

        private fun sicherung(v: Verbindung) = ServerSicherungsAblage(v, TOKEN)
        private fun backup(v: Verbindung) = ServerBackupAblage(v, TOKEN)
        private fun geraete(v: Verbindung) = ServerGeraete(v, TOKEN)
        private fun konto(v: Verbindung) = ServerKontoLoeschen(v, TOKEN)
        private suspend fun diagnose(v: Verbindung, nutzlast: ByteArray = CHIFFRAT): Any? =
            ServerDiagnoseAblage(v, TOKEN).senden(nutzlast).let { "gesendet" }

        // Eigene Zahl statt Diagnose.MAX_NUTZLAST (diagnose.proto: 256 KiB).
        private val UEBER_256_KIB = ByteArray(256 * 1024 + 1)
        private val GENAU_256_KIB = ByteArray(256 * 1024)

        @JvmStatic
        fun fehlerfaelle() = listOf(
            // Datensicherung
            Fall("Sicherung holen: nicht gefunden heißt keine Sicherung", Code.NOT_FOUND, { sicherung(it).holen() }, null),
            Fall("Fassung holen: nicht gefunden heißt nicht mehr da", Code.NOT_FOUND, { sicherung(it).holen(7) }, null),
            Fall("Fassung holen: erschöpft heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).holen(7) }, SicherungTageslimit::class),
            Fall("Fassung holen: ungültige Anfrage bleibt Serverfehler", Code.INVALID_ARGUMENT, { sicherung(it).holen(7) }, Serverfehler::class),
            Fall("Fassungen auflisten: erschöpft heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).fassungen() }, SicherungTageslimit::class),
            Fall("Fassungen auflisten: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { sicherung(it).fassungen() }, Serverfehler::class),
            Fall("Fassungen auflisten: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { sicherung(it).fassungen() }, Serverfehler::class),
            Fall("Sicherung holen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { sicherung(it).holen() }, Serverfehler::class),
            Fall("Sicherung holen: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { sicherung(it).holen() }, Serverfehler::class),
            // Entscheidung nach Review-Lauf 8: Holen und Löschen sind je Tag begrenzt.
            Fall("Sicherung holen: erschöpft heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).holen() }, SicherungTageslimit::class),
            Fall("Sicherung löschen: erschöpft heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).loeschen() }, SicherungTageslimit::class),
            Fall("Sicherung speichern: Bedingung verletzt heißt veraltet", Code.FAILED_PRECONDITION, { sicherung(it).speichern(CHIFFRAT, 1) }, VersionVeraltet::class),
            // KRITISCH: Bis 8 MiB heißt erschöpft "Tageslimit", sonst meldete die App
            // fälschlich "zu groß" (und der Mensch suchte den Fehler bei sich).
            Fall("Sicherung speichern: erschöpft über 8 MiB heißt zu groß", Code.RESOURCE_EXHAUSTED, { sicherung(it).speichern(UEBER_8_MIB, 1) }, SicherungZuGross::class),
            Fall("Sicherung speichern: erschöpft bei genau 8 MiB heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).speichern(GENAU_8_MIB, 1) }, SicherungTageslimit::class),
            Fall("Sicherung speichern: erschöpft bei kleiner Sicherung heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { sicherung(it).speichern(CHIFFRAT, 1) }, SicherungTageslimit::class),
            Fall("Sicherung speichern: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung speichern: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung speichern: ungültige Anfrage bleibt Serverfehler", Code.INVALID_ARGUMENT, { sicherung(it).speichern(CHIFFRAT, 1) }, Serverfehler::class),
            Fall("Sicherung löschen: Fehler bleibt Serverfehler", Code.UNAUTHENTICATED, { sicherung(it).loeschen() }, Serverfehler::class),
            Fall("Sicherung löschen: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { sicherung(it).loeschen() }, Serverfehler::class),
            // KRITISCH (E-B2b-13): permission_denied heißt bei ändernden Aufrufen
            // "Gerät nicht freigeschaltet", damit die App zur Freischaltung führt.
            Fall("Sicherung speichern: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { sicherung(it).speichern(CHIFFRAT, 1) }, GeraetNichtFreigeschaltet::class),
            Fall("Sicherung löschen: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { sicherung(it).loeschen() }, GeraetNichtFreigeschaltet::class),
            Fall("Sicherung holen: keine Erlaubnis bleibt Serverfehler", Code.PERMISSION_DENIED, { sicherung(it).holen() }, Serverfehler::class),
            // Schlüssel-Backup
            Fall("Backup holen: nicht gefunden heißt kein Backup", Code.NOT_FOUND, { backup(it).holen() }, null),
            Fall("Backup holen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { backup(it).holen() }, Serverfehler::class),
            Fall("Backup holen: erschöpft bleibt Serverfehler", Code.RESOURCE_EXHAUSTED, { backup(it).holen() }, Serverfehler::class),
            Fall("Backup speichern: Bedingung verletzt heißt veraltet", Code.FAILED_PRECONDITION, { backup(it).speichern(CHIFFRAT, SCHLUESSEL, 1) }, VersionVeraltet::class),
            Fall("Backup speichern: zu groß bleibt Serverfehler", Code.RESOURCE_EXHAUSTED, { backup(it).speichern(CHIFFRAT, SCHLUESSEL, 1) }, Serverfehler::class),
            Fall("Backup speichern: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { backup(it).speichern(CHIFFRAT, SCHLUESSEL, 1) }, Serverfehler::class),
            Fall("Backup speichern: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { backup(it).speichern(CHIFFRAT, SCHLUESSEL, 1) }, GeraetNichtFreigeschaltet::class),
            Fall("Backup löschen: Fehler bleibt Serverfehler", Code.UNAUTHENTICATED, { backup(it).loeschen() }, Serverfehler::class),
            Fall("Backup löschen: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { backup(it).loeschen() }, GeraetNichtFreigeschaltet::class),
            // Geräte
            Fall("Mit Wiederherstellung freischalten: keine Erlaubnis heißt Signatur abgelehnt", Code.PERMISSION_DENIED, { geraete(it).mitWiederherstellung(CHIFFRAT) }, GeraetNichtFreigeschaltet::class),
            Fall("Mit Wiederherstellung freischalten: kein Backup bleibt Serverfehler", Code.FAILED_PRECONDITION, { geraete(it).mitWiederherstellung(CHIFFRAT) }, Serverfehler::class),
            Fall("Mit Wiederherstellung freischalten: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { geraete(it).mitWiederherstellung(CHIFFRAT) }, Serverfehler::class),
            Fall("Gerät freischalten: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { geraete(it).freischalten("x") }, GeraetNichtFreigeschaltet::class),
            Fall("Gerät freischalten: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { geraete(it).freischalten("x") }, Serverfehler::class),
            Fall("Gerät freischalten: gesperrt bleibt Serverfehler", Code.FAILED_PRECONDITION, { geraete(it).freischalten("x") }, Serverfehler::class),
            Fall("Gerät sperren: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { geraete(it).sperren("x") }, GeraetNichtFreigeschaltet::class),
            Fall("Gerät sperren: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { geraete(it).sperren("x") }, Serverfehler::class),
            Fall("Gerät sperren: nicht gefunden bleibt Serverfehler", Code.NOT_FOUND, { geraete(it).sperren("x") }, Serverfehler::class),
            // Konto löschen (B1)
            // KRITISCH: Nur „schon fällig“ heißt LoeschungLaeuft; nur ein freigeschaltetes
            // Gerät darf abbrechen. Alles andere bleibt Serverfehler.
            Fall("Löschung abbrechen: Bedingung verletzt heißt läuft schon", Code.FAILED_PRECONDITION, { konto(it).abbrechen() }, LoeschungLaeuft::class),
            Fall("Löschung abbrechen: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { konto(it).abbrechen() }, GeraetNichtFreigeschaltet::class),
            Fall("Löschung abbrechen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { konto(it).abbrechen() }, Serverfehler::class),
            Fall("Löschung abbrechen: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { konto(it).abbrechen() }, Serverfehler::class),
            Fall("Konto löschen: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { konto(it).loeschen() }, Serverfehler::class),
            Fall("Konto löschen: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { konto(it).loeschen() }, Serverfehler::class),
            Fall("Konto löschen: keine Erlaubnis bleibt Serverfehler", Code.PERMISSION_DENIED, { konto(it).loeschen() }, Serverfehler::class),
            Fall("Konto löschen: Bedingung verletzt bleibt Serverfehler", Code.FAILED_PRECONDITION, { konto(it).loeschen() }, Serverfehler::class),
            // Diagnose
            Fall("Diagnose senden: keine Erlaubnis heißt nicht freigeschaltet", Code.PERMISSION_DENIED, { diagnose(it) }, DiagnoseNichtFreigeschaltet::class),
            Fall("Diagnose senden: erschöpft bei kleiner Sendung heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { diagnose(it) }, DiagnoseTageslimit::class),
            Fall("Diagnose senden: erschöpft bei genau 256 KiB heißt Tageslimit", Code.RESOURCE_EXHAUSTED, { diagnose(it, GENAU_256_KIB) }, DiagnoseTageslimit::class),
            Fall("Diagnose senden: erschöpft über 256 KiB heißt zu groß", Code.RESOURCE_EXHAUSTED, { diagnose(it, UEBER_256_KIB) }, DiagnoseZuGross::class),
            Fall("Diagnose senden: ungültige Sitzung bleibt Serverfehler", Code.UNAUTHENTICATED, { diagnose(it) }, Serverfehler::class),
            Fall("Diagnose senden: ungültige Anfrage bleibt Serverfehler", Code.INVALID_ARGUMENT, { diagnose(it) }, Serverfehler::class),
            Fall("Diagnose senden: Überlastung bleibt Serverfehler", Code.UNAVAILABLE, { diagnose(it) }, Serverfehler::class),
        )

        private const val HTML_404 = "<html><body>404 Not Found</body></html>"

        @JvmStatic
        fun groessteSicherung() = listOf(
            GrossFall("mit Längenangabe", gzip = false, ohneLaenge = false),
            GrossFall("ohne Längenangabe", gzip = false, ohneLaenge = true),
            GrossFall("gzip-gepackt", gzip = true, ohneLaenge = false),
        )

        private const val UEBER_DER_GRENZE = (Verbindung.MAX_ANTWORT + 1).toInt()

        @JvmStatic
        fun zuGross() = listOf(
            GrossFall("ein Byte über der Grenze, mit Längenangabe", gzip = false, ohneLaenge = false, UEBER_DER_GRENZE),
            GrossFall("ein Byte über der Grenze, ohne Längenangabe", gzip = false, ohneLaenge = true, UEBER_DER_GRENZE),
            GrossFall("gzip-Bombe: wenige KiB gepackt, 64 MiB entpackt", gzip = true, ohneLaenge = false, 64 * 1024 * 1024),
        )

        private fun gzip(daten: ByteArray): ByteArray =
            ByteArrayOutputStream().also { aus -> GZIPOutputStream(aus).use { it.write(daten) } }.toByteArray()

        @JvmStatic
        fun rohfaelle() = listOf(
            RohFall("Sicherung holen: HTML-Seite mit 404", 404, "text/html", HTML_404) { sicherung(it).holen() },
            RohFall("Sicherung holen: Text mit 503", 503, "text/plain", "Service Unavailable") { sicherung(it).holen() },
            RohFall("Backup holen: HTML-Seite mit 404", 404, "text/html", HTML_404) { backup(it).holen() },
            RohFall("Backup holen: Text mit 503", 503, "text/plain", "Service Unavailable") { backup(it).holen() },
            RohFall("Diagnose senden: Text mit 503", 503, "text/plain", "Service Unavailable") { diagnose(it) },
            // So antwortet Google, wenn der Kill-Switch allUsers entfernt hat.
            RohFall("Sicherung speichern: HTML-Seite mit 403", 403, "text/html", HTML_403) { sicherung(it).speichern(CHIFFRAT, 1) },
            RohFall("Sicherung löschen: HTML-Seite mit 403", 403, "text/html", HTML_403) { sicherung(it).loeschen() },
            RohFall("Backup speichern: HTML-Seite mit 403", 403, "text/html", HTML_403) { backup(it).speichern(CHIFFRAT, SCHLUESSEL, 1) },
            RohFall("Backup löschen: HTML-Seite mit 403", 403, "text/html", HTML_403) { backup(it).loeschen() },
            RohFall("Mit Wiederherstellung freischalten: HTML-Seite mit 403", 403, "text/html", HTML_403) { geraete(it).mitWiederherstellung(CHIFFRAT) },
            RohFall("Gerät freischalten: HTML-Seite mit 403", 403, "text/html", HTML_403) { geraete(it).freischalten("x") },
            RohFall("Diagnose senden: HTML-Seite mit 403", 403, "text/html", HTML_403) { diagnose(it) },
            RohFall("Löschung abbrechen: HTML-Seite mit 403", 403, "text/html", HTML_403) { konto(it).abbrechen() },
        )

        private const val HTML_403 = "<html><body>403 Forbidden</body></html>"
        private const val JSON_403 = """{"code":"permission_denied","message":"nicht freigeschaltet"}"""

        // Gegenprobe: Derselbe Status mit Connect-Fehler (so antwortet der Dienst
        // selbst) heißt weiter „nicht freigeschaltet“.
        @JvmStatic
        fun connectFehlerMit403() = listOf(
            RohFall("Sicherung speichern: Connect-Fehler mit 403", 403, "application/json", JSON_403, GeraetNichtFreigeschaltet::class) { sicherung(it).speichern(CHIFFRAT, 1) },
            RohFall("Backup löschen: Connect-Fehler mit 403", 403, "application/json; charset=utf-8", JSON_403, GeraetNichtFreigeschaltet::class) { backup(it).loeschen() },
            RohFall("Diagnose senden: Connect-Fehler mit 403", 403, "application/json", JSON_403, DiagnoseNichtFreigeschaltet::class) { diagnose(it) },
        )
    }
}
