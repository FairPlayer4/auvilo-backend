package com.avlreminder.testclient

import com.avlreminder.protokoll.BackupAblage
import com.avlreminder.protokoll.GeholtesBackup
import com.avlreminder.protokoll.VersionVeraltet
import com.avlreminder.schnittstelle.v1.AnmeldeDienstClient
import com.avlreminder.schnittstelle.v1.BackupHolenRequest
import com.avlreminder.schnittstelle.v1.BackupSpeichernRequest
import com.avlreminder.schnittstelle.v1.GeraeteDienstClient
import com.avlreminder.schnittstelle.v1.SchluesselDienstClient
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

/** Die Backup-Ablage des Servers für die [Einrichtung]. */
class ServerBackupAblage(private val verbindung: Verbindung, private val token: String) : BackupAblage {
    override suspend fun holen(): GeholtesBackup? =
        try {
            val b = verbindung.schluessel.backupHolen(BackupHolenRequest.getDefaultInstance(), mitSitzung(token)).ergebnis()
            GeholtesBackup(b.chiffrat.toByteArray(), b.version)
        } catch (e: Serverfehler) {
            if (e.code == Code.NOT_FOUND) null else throw e
        }

    override suspend fun speichern(chiffrat: ByteArray, erwartet: Long): Long =
        try {
            val anfrage = BackupSpeichernRequest.newBuilder()
                .setChiffrat(ByteString.copyFrom(chiffrat))
                .setErwarteteVersion(erwartet)
                .build()
            verbindung.schluessel.backupSpeichern(anfrage, mitSitzung(token)).ergebnis().version
        } catch (e: Serverfehler) {
            if (e.code == Code.FAILED_PRECONDITION) throw VersionVeraltet() else throw e
        }
}
