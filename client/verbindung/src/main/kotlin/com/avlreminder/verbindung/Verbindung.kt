package com.avlreminder.verbindung

import com.avlreminder.protokoll.BackupAblage
import com.avlreminder.protokoll.Datensicherung
import com.avlreminder.protokoll.Diagnose
import com.avlreminder.protokoll.DiagnoseAblage
import com.avlreminder.protokoll.DiagnoseNichtFreigeschaltet
import com.avlreminder.protokoll.DiagnoseTageslimit
import com.avlreminder.protokoll.DiagnoseZuGross
import com.avlreminder.protokoll.GeraeteFreischaltung
import com.avlreminder.protokoll.GeraetNichtFreigeschaltet
import com.avlreminder.protokoll.GeholteSicherung
import com.avlreminder.protokoll.GeholtesBackup
import com.avlreminder.protokoll.KontoAufDemServer
import com.avlreminder.protokoll.KontoGeraet
import com.avlreminder.protokoll.SicherungTageslimit
import com.avlreminder.protokoll.SicherungZuGross
import com.avlreminder.protokoll.SicherungsAblage
import com.avlreminder.protokoll.VersionVeraltet
import com.avlreminder.schnittstelle.v1.AnmeldeDienstClient
import com.avlreminder.schnittstelle.v1.BackupHolenRequest
import com.avlreminder.schnittstelle.v1.BackupLoeschenRequest
import com.avlreminder.schnittstelle.v1.BackupSpeichernRequest
import com.avlreminder.schnittstelle.v1.DatensicherungDienstClient
import com.avlreminder.schnittstelle.v1.DiagnoseDienstClient
import com.avlreminder.schnittstelle.v1.DiagnoseSendenRequest
import com.avlreminder.schnittstelle.v1.GeraetFreischaltenRequest
import com.avlreminder.schnittstelle.v1.GeraetSperrenRequest
import com.avlreminder.schnittstelle.v1.GeraeteAuflistenRequest
import com.avlreminder.schnittstelle.v1.GeraeteDienstClient
import com.avlreminder.schnittstelle.v1.MitWiederherstellungFreischaltenRequest
import com.avlreminder.schnittstelle.v1.SchluesselDienstClient
import com.avlreminder.schnittstelle.v1.SicherungHolenRequest
import com.avlreminder.schnittstelle.v1.SicherungLoeschenRequest
import com.avlreminder.schnittstelle.v1.SicherungSpeichernRequest
import com.connectrpc.Code
import com.connectrpc.ConnectException
import com.connectrpc.Headers
import com.connectrpc.ProtocolClientConfig
import com.connectrpc.ResponseMessage
import com.connectrpc.extensions.GoogleJavaLiteProtobufStrategy
import com.connectrpc.impl.ProtocolClient
import com.connectrpc.okhttp.ConnectOkHttpClient
import com.connectrpc.protocols.NetworkProtocol
import com.google.protobuf.ByteString
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Response
import okhttp3.ResponseBody.Companion.asResponseBody
import okio.Buffer
import okio.ForwardingSource
import okio.buffer
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Die Connect-Clients für einen Server.
 *
 * [basis] liefert Zeitlimits und anderes; die Obergrenze für Antworten
 * ([MAX_ANTWORT]) kommt immer dazu, auch bei einem eigenen Client.
 */
class Verbindung(server: String, basis: OkHttpClient = standardHttp()) {
    val http: OkHttpClient = basis.newBuilder().addInterceptor(AntwortGrenze(MAX_ANTWORT)).build()

    private val client = ProtocolClient(
        ConnectOkHttpClient(http),
        ProtocolClientConfig(
            host = server,
            serializationStrategy = GoogleJavaLiteProtobufStrategy(),
            networkProtocol = NetworkProtocol.CONNECT,
            // KRITISCH: Connect entpackt nichts selbst (E9). Sonst entpackte es
            // gzip nach der Obergrenze, und eine kleine Antwort könnte zu vielen
            // MiB werden. OkHttp entpackt stattdessen vor der Obergrenze.
            compressionPools = emptyList(),
        ),
    )
    val anmelde = AnmeldeDienstClient(client)
    val geraete = GeraeteDienstClient(client)
    val schluessel = SchluesselDienstClient(client)
    val sicherung = DatensicherungDienstClient(client)
    val diagnose = DiagnoseDienstClient(client)

    companion object {
        /**
         * Größte Antwort, entpackt: die größte Sicherung ([Datensicherung.MAX_CHIFFRAT])
         * und 1 MiB für den Rest der Nachricht.
         */
        const val MAX_ANTWORT: Long = Datensicherung.MAX_CHIFFRAT + 1024L * 1024

        /**
         * Zeitlimits für die App (E9). OkHttp hat sonst je 10 s für Verbinden, Lesen
         * und Schreiben, aber kein Gesamtlimit: Ein Server, der alle paar Sekunden ein
         * Byte schickt, hielte einen Aufruf ewig offen. Lesen und Schreiben bekommen
         * 30 s Pause, damit 8 MiB auch über langsames Mobilfunknetz gehen (der Server
         * hat dafür selbst 30 s je Anfrage); der ganze Aufruf höchstens 2 Minuten.
         */
        fun standardHttp(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .readTimeout(30, TimeUnit.SECONDS)
            .callTimeout(2, TimeUnit.MINUTES)
            .build()
    }
}

/** Die Antwort des Servers ist größer als [grenze] Bytes (entpackt); gelesen wurde nur bis dorthin. */
class AntwortZuGross(val grenze: Long) : IOException("Antwort des Servers größer als $grenze Bytes")

/**
 * KRITISCH: Begrenzt jede Antwort auf [grenze] Bytes, entpackt gezählt (E9).
 * Als Interceptor der Anwendung sieht er die Antwort, nachdem OkHttp gzip
 * entpackt hat. Eine zu große Antwort bricht beim Lesen ab, statt den Speicher
 * der App zu füllen; mit bekannter Länge schon vorher.
 */
internal class AntwortGrenze(private val grenze: Long) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val antwort = chain.proceed(chain.request())
        val inhalt = antwort.body
        val laenge = inhalt.contentLength()
        if (laenge > grenze) {
            antwort.close()
            throw AntwortZuGross(grenze)
        }
        val begrenzt = object : ForwardingSource(inhalt.source()) {
            private var gelesen = 0L
            override fun read(sink: Buffer, byteCount: Long): Long {
                val n = super.read(sink, byteCount)
                if (n > 0) {
                    gelesen += n
                    if (gelesen > grenze) throw AntwortZuGross(grenze)
                }
                return n
            }
        }
        return antwort.newBuilder().body(begrenzt.buffer().asResponseBody(inhalt.contentType(), laenge)).build()
    }
}

/** Header mit dem Sitzungs-Token. */
fun mitSitzung(token: String): Headers = mapOf("Authorization" to listOf("Bearer $token"))

/**
 * Die Antwort oder eine verständliche Fehlermeldung.
 *
 * KRITISCH: Kam gar keine Antwort (kein Netz, Verbindung abgelehnt, Zeitüberschreitung),
 * meldet Connect das als Fehler mit Code „unknown“ und der [IOException] darin. Die
 * wird hier wieder eine [IOException], damit die App „kein Netz“ sagt und nicht
 * „Server gesperrt oder gestört“.
 */
fun <T> ResponseMessage<T>.ergebnis(): T =
    when (this) {
        is ResponseMessage.Success -> message
        is ResponseMessage.Failure -> throw netzfehler(cause) ?: Serverfehler(cause, vomDienst = istConnectFehler(headers))
    }

/** Die [IOException] in [e], wenn der Aufruf den Server nicht erreicht hat. */
private fun netzfehler(e: ConnectException): IOException? =
    generateSequence(e.exception) { it.cause }.take(5).filterIsInstance<IOException>().firstOrNull()

/**
 * Eine Fehlerantwort des Dienstes selbst kommt bei Connect als JSON. Antworten
 * ohne JSON stammen von etwas davor (Google bei gesperrtem Dienst: 403 als
 * HTML, ein Proxy, eine falsche Adresse); Connect leitet den Code dann nur aus
 * dem HTTP-Status ab.
 */
private fun istConnectFehler(headers: Headers): Boolean =
    headers.entries.any { (name, werte) ->
        name.equals("content-type", ignoreCase = true) && werte.any { it.trim().startsWith("application/json", ignoreCase = true) }
    }

/**
 * Ein Fehler beim Aufruf. [vomDienst] = false: Die Antwort kam nicht vom
 * Dienst (kein Connect-Fehler), der Code ist nur aus dem HTTP-Status geraten.
 */
class Serverfehler(val ursache: ConnectException, val vomDienst: Boolean = true) :
    Exception("Server: ${ursache.code.codeName} - ${ursache.message}", ursache) {
    val code: Code get() = ursache.code

    /** permission_denied, gesagt vom Dienst selbst (nicht von einer Sperre davor). */
    internal val keineErlaubnis: Boolean get() = vomDienst && code == Code.PERMISSION_DENIED
}

/**
 * permission_denied heißt bei den ändernden Aufrufen: Dieses Gerät ist noch
 * nicht freigeschaltet (geraete.proto, E-B2b-13).
 *
 * KRITISCH: Nur wenn der Dienst selbst das sagt. Ein 403 ohne Connect-Fehler
 * (Kill-Switch hat den Dienst gesperrt) bleibt ein Serverfehler; sonst führte
 * die App ein freigeschaltetes Gerät in die Freischaltung.
 */
private fun Serverfehler.nichtFreigeschaltetOder(): Exception =
    if (keineErlaubnis) GeraetNichtFreigeschaltet() else this

/** Die Backup-Ablage des Servers für die Einrichtung (protokoll). */
class ServerBackupAblage(private val verbindung: Verbindung, private val token: String) : BackupAblage {
    override suspend fun holen(): GeholtesBackup? =
        try {
            val b = verbindung.schluessel.backupHolen(BackupHolenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
            GeholtesBackup(b.chiffrat.toByteArray(), b.version)
        } catch (e: Serverfehler) {
            if (e.code == Code.NOT_FOUND) null else throw e
        }

    override suspend fun speichern(chiffrat: ByteArray, freischaltschluessel: ByteArray, erwartet: Long): Long =
        try {
            val anfrage = BackupSpeichernRequest.newBuilder()
                .setChiffrat(ByteString.copyFrom(chiffrat))
                .setFreischaltschluessel(ByteString.copyFrom(freischaltschluessel))
                .setErwarteteVersion(erwartet)
                .build()
            verbindung.schluessel.backupSpeichern(anfrage, mitSitzung(token)).ergebnis().version
        } catch (e: Serverfehler) {
            if (e.code == Code.FAILED_PRECONDITION) throw VersionVeraltet() else throw e.nichtFreigeschaltetOder()
        }

    /** Löscht das Schlüssel-Backup des Kontos (gibt es keins, ist das kein Fehler). */
    suspend fun loeschen() {
        try {
            verbindung.schluessel.backupLoeschen(BackupLoeschenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            throw e.nichtFreigeschaltetOder()
        }
    }
}

/**
 * Freischaltung von Geräten (geraete.proto, E-B2b-13).
 *
 * KRITISCH: Nur permission_denied heißt „nicht freigeschaltet“ bzw. „Signatur
 * passt nicht“. not_found (fremdes Gerät) und failed_precondition (gesperrt, kein
 * Backup mit Freischaltschlüssel) bleiben Serverfehler mit eigener Meldung.
 */
class ServerGeraete(private val verbindung: Verbindung, private val token: String) : GeraeteFreischaltung {
    override suspend fun mitWiederherstellung(signatur: ByteArray) {
        try {
            val anfrage = MitWiederherstellungFreischaltenRequest.newBuilder().setSignatur(ByteString.copyFrom(signatur)).build()
            verbindung.geraete.mitWiederherstellungFreischalten(anfrage, mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            throw e.nichtFreigeschaltetOder()
        }
    }

    /**
     * Sperrt ein Gerät des Kontos, endgültig. Das eigene Gerät darf sich immer sperren (=
     * abmelden); danach braucht es einen neuen Geräteschlüssel. Ein anderes nur, wenn dieses
     * Gerät freigeschaltet ist.
     */
    suspend fun sperren(geraetId: String) {
        try {
            val anfrage = GeraetSperrenRequest.newBuilder().setGeraetId(geraetId).build()
            verbindung.geraete.geraetSperren(anfrage, mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            throw e.nichtFreigeschaltetOder()
        }
    }

    /** Die Geräte des Kontos, auch gesperrte. */
    suspend fun auflisten(): List<KontoGeraet> =
        verbindung.geraete.geraeteAuflisten(GeraeteAuflistenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
            .geraeteList.map { KontoGeraet(it.geraetId, it.gesperrt, it.diesesGeraet) }

    /** Schaltet ein anderes Gerät des Kontos frei; dieses Gerät muss selbst freigeschaltet sein. */
    suspend fun freischalten(geraetId: String) {
        try {
            val anfrage = GeraetFreischaltenRequest.newBuilder().setGeraetId(geraetId).build()
            verbindung.geraete.geraetFreischalten(anfrage, mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            throw e.nichtFreigeschaltetOder()
        }
    }
}

/** Das Konto auf dem Server für „Cloud-Daten löschen“ ([com.avlreminder.protokoll.CloudLoeschen]). */
class ServerKonto(verbindung: Verbindung, token: String) : KontoAufDemServer {
    private val geraete = ServerGeraete(verbindung, token)
    private val sicherung = ServerSicherungsAblage(verbindung, token)
    private val schluessel = ServerBackupAblage(verbindung, token)

    override suspend fun geraete() = geraete.auflisten()
    override suspend fun sperren(geraetId: String) = geraete.sperren(geraetId)
    override suspend fun datensicherungLoeschen() = sicherung.loeschen()
    override suspend fun schluesselBackupLoeschen() = schluessel.loeschen()
}

/**
 * Die Datensicherung des Servers für den Sicherungsablauf (protokoll).
 *
 * KRITISCH: Übersetzt die Fehlercodes aus datensicherung.proto in die Ausnahmen,
 * auf die der Ablauf reagiert. Ein falsch übersetzter Code ließe den Ablauf etwa
 * ein fremdes Gerät überschreiben (veraltet) oder „keine Sicherung“ melden, wo es
 * nur einen Netzfehler gab.
 */
class ServerSicherungsAblage(private val verbindung: Verbindung, private val token: String) : SicherungsAblage {
    override suspend fun holen(): GeholteSicherung? =
        try {
            val s = verbindung.sicherung.sicherungHolen(SicherungHolenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
            GeholteSicherung(s.chiffrat.toByteArray(), s.version)
        } catch (e: Serverfehler) {
            when (e.code) {
                Code.NOT_FOUND -> null
                Code.RESOURCE_EXHAUSTED -> throw SicherungTageslimit("Abrufe der Sicherung")
                else -> throw e
            }
        }

    override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long =
        try {
            val anfrage = SicherungSpeichernRequest.newBuilder()
                .setChiffrat(ByteString.copyFrom(chiffrat))
                .setErwarteteVersion(erwartet)
                .build()
            verbindung.sicherung.sicherungSpeichern(anfrage, mitSitzung(token)).ergebnis().version
        } catch (e: Serverfehler) {
            when (e.code) {
                Code.FAILED_PRECONDITION -> throw VersionVeraltet()
                // Der Server meldet "zu groß" und "Tageslimit" mit demselben Code
                // (Connect selbst nimmt ihn für zu große Anfragen). Die Größe des
                // Gesendeten unterscheidet: Bis 8 MiB lehnt er nur wegen des Limits ab.
                Code.RESOURCE_EXHAUSTED ->
                    if (chiffrat.size > Datensicherung.MAX_CHIFFRAT) throw SicherungZuGross(chiffrat.size) else throw SicherungTageslimit()
                else -> throw e.nichtFreigeschaltetOder()
            }
        }

    override suspend fun loeschen() {
        try {
            verbindung.sicherung.sicherungLoeschen(SicherungLoeschenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            if (e.code == Code.RESOURCE_EXHAUSTED) throw SicherungTageslimit("das Löschen der Sicherung")
            throw e.nichtFreigeschaltetOder()
        }
    }
}

/**
 * Der Diagnose-Dienst des Servers (protokoll, [DiagnoseAblage]).
 *
 * KRITISCH: Übersetzt nur die zwei Codes, auf die die App mit eigener Meldung
 * reagiert (diagnose.proto); alles andere bleibt ein [Serverfehler].
 * resource_exhausted heißt „zu groß“ oder „Tageslimit“; wie bei der Datensicherung
 * unterscheidet die Größe des Gesendeten: Bis 256 KiB lehnt der Server nur wegen
 * des Limits ab. Zu große Sendungen hält schon [Diagnose.senden] zurück.
 * Ein 403 ohne Connect-Fehler (Kill-Switch, Proxy) bleibt ein Serverfehler.
 */
class ServerDiagnoseAblage(private val verbindung: Verbindung, private val token: String) : DiagnoseAblage {
    override suspend fun senden(nutzlast: ByteArray) {
        try {
            val anfrage = DiagnoseSendenRequest.newBuilder().setNutzlast(ByteString.copyFrom(nutzlast)).build()
            verbindung.diagnose.diagnoseSenden(anfrage, mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            when {
                e.keineErlaubnis -> throw DiagnoseNichtFreigeschaltet()
                e.code == Code.RESOURCE_EXHAUSTED ->
                    if (nutzlast.size > Diagnose.MAX_NUTZLAST) throw DiagnoseZuGross(nutzlast.size) else throw DiagnoseTageslimit()
                else -> throw e
            }
        }
    }
}
