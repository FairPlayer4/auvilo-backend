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
import okhttp3.OkHttpClient

/** Die Connect-Clients für einen Server. */
class Verbindung(server: String, val http: OkHttpClient = OkHttpClient()) {
    private val client = ProtocolClient(
        ConnectOkHttpClient(http),
        ProtocolClientConfig(
            host = server,
            serializationStrategy = GoogleJavaLiteProtobufStrategy(),
            networkProtocol = NetworkProtocol.CONNECT,
        ),
    )
    val anmelde = AnmeldeDienstClient(client)
    val geraete = GeraeteDienstClient(client)
    val schluessel = SchluesselDienstClient(client)
    val sicherung = DatensicherungDienstClient(client)
    val diagnose = DiagnoseDienstClient(client)
}

/** Header mit dem Sitzungs-Token. */
fun mitSitzung(token: String): Headers = mapOf("Authorization" to listOf("Bearer $token"))

/** Die Antwort oder eine verständliche Fehlermeldung. */
fun <T> ResponseMessage<T>.ergebnis(): T =
    when (this) {
        is ResponseMessage.Success -> message
        is ResponseMessage.Failure -> throw Serverfehler(cause)
    }

/** Der Server hat mit einem Fehlercode geantwortet. */
class Serverfehler(val ursache: ConnectException) : Exception("Server: ${ursache.code.codeName} - ${ursache.message}", ursache) {
    val code: Code get() = ursache.code
}

/**
 * permission_denied heißt bei den ändernden Aufrufen: Dieses Gerät ist noch
 * nicht freigeschaltet (geraete.proto, E-B2b-13). Gleiche Grenze wie bei
 * [ServerDiagnoseAblage]: Ein 403 ohne Connect-Fehler landet ebenfalls hier.
 */
private fun Serverfehler.nichtFreigeschaltetOder(): Exception =
    if (code == Code.PERMISSION_DENIED) GeraetNichtFreigeschaltet() else this

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
            if (e.code == Code.NOT_FOUND) null else throw e
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
 *
 * Bekannte Grenze: Ein HTTP-403 ohne Connect-Fehler (etwa von einem Proxy) macht
 * Connect selbst zu permission_denied; dann meldet der Adapter fälschlich „nicht
 * freigeschaltet“. Folgen hat das keine: Gesendet wird nichts, verloren geht nichts.
 */
class ServerDiagnoseAblage(private val verbindung: Verbindung, private val token: String) : DiagnoseAblage {
    override suspend fun senden(nutzlast: ByteArray) {
        try {
            val anfrage = DiagnoseSendenRequest.newBuilder().setNutzlast(ByteString.copyFrom(nutzlast)).build()
            verbindung.diagnose.diagnoseSenden(anfrage, mitSitzung(token)).ergebnis()
        } catch (e: Serverfehler) {
            when (e.code) {
                Code.PERMISSION_DENIED -> throw DiagnoseNichtFreigeschaltet()
                Code.RESOURCE_EXHAUSTED ->
                    if (nutzlast.size > Diagnose.MAX_NUTZLAST) throw DiagnoseZuGross(nutzlast.size) else throw DiagnoseTageslimit()
                else -> throw e
            }
        }
    }
}
