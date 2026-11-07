package com.avlreminder.verbindung

import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.schnittstelle.v1.SitzungErneuernRequest
import com.connectrpc.Code
import com.google.protobuf.ByteString
import com.google.protobuf.Timestamp
import java.time.Instant

/**
 * Der Geräteschlüssel (ECDSA P-256) eines Geräts. Im Test-Client liegt er in
 * einer Datei, in der App im Android Keystore; deshalb nur als Schnittstelle.
 */
interface Geraeteschluessel {
    /** Der öffentliche Schlüssel, PKIX/DER. */
    val oeffentlich: ByteArray

    /** Signatur mit "SHA256withECDSA" (ASN.1/DER), wie der Server sie prüft. */
    fun signiere(nachricht: ByteArray): ByteArray
}

/** Eine Sitzung beim Server, wie das Gerät sie sich merkt. */
data class Sitzung(val kontoId: String, val geraetId: String, val token: String, val laeuftAbUnix: Long)

/** Wo das Gerät seine Sitzung ablegt (Test-Client: Datei, App: DataStore). */
interface SitzungsAblage {
    fun lesen(): Sitzung?
    fun speichern(sitzung: Sitzung)
}

/** Ergebnis von [Anmeldung.anmelden]. */
data class Angemeldet(val sitzung: Sitzung, val neuesKonto: Boolean, val freigeschaltet: Boolean)

/** Es gibt keine Sitzung: Das Gerät muss sich zuerst mit Google anmelden. */
class NichtAngemeldet : Exception("Nicht angemeldet - zuerst mit Google anmelden")

/**
 * Der Server hat Anmelden oder Erneuern abgelehnt (unauthenticated). Nach einem
 * abgelehnten Erneuern hilft nur eine neue Anmeldung mit Google.
 */
class AnmeldungAbgelehnt : Exception("Anmeldung abgelehnt - neu mit Google anmelden")

/**
 * Dieses Gerät ist gesperrt, endgültig (permission_denied beim Anmelden). Nur ein
 * neuer Geräteschlüssel ergibt ein neues Gerät.
 */
class GeraetGesperrt : Exception("Dieses Gerät ist gesperrt")

/**
 * Anmelden mit Google und Erneuern der Sitzung mit dem Geräteschlüssel
 * (anmeldung.proto). Das ID-Token holt der Aufrufer selbst (Test-Client:
 * Browser, App: Credential Manager), und zwar mit [nonce].
 *
 * KRITISCH: Was signiert wird, kommt aus [Geraetenachrichten] (gemeinsame
 * Testvektoren mit dem Server). Und nur Fehler, die der Dienst selbst meldet,
 * werden umgedeutet: Ein 403 vom gesperrten Dienst (Kill-Switch) darf nicht als
 * „Gerät gesperrt“ gelten, sonst würfe die App ihren Geräteschlüssel weg.
 */
class Anmeldung(
    private val verbindung: Verbindung,
    private val schluessel: Geraeteschluessel,
    private val ablage: SitzungsAblage,
    /** Jetzt in Unix-Sekunden; im Test fest. */
    private val uhr: () -> Long = { Instant.now().epochSecond },
) {
    /** Die Geräte-ID, wie der Server sie aus dem Schlüssel ableitet. */
    val geraetId: String get() = Geraetenachrichten.geraetId(schluessel.oeffentlich)

    /** Die Nonce, mit der das ID-Token bei Google geholt werden muss. */
    val nonce: String get() = Geraetenachrichten.nonce(schluessel.oeffentlich)

    /** Meldet das Gerät mit einem ID-Token an und merkt sich die Sitzung. */
    suspend fun anmelden(idToken: String): Angemeldet {
        val anfrage = AnmeldenRequest.newBuilder()
            .setGoogleIdToken(idToken)
            .setGeraeteschluessel(ByteString.copyFrom(schluessel.oeffentlich))
            .setSignatur(ByteString.copyFrom(schluessel.signiere(Geraetenachrichten.anmelden(idToken))))
            .build()
        val antwort = try {
            verbindung.anmelde.anmelden(anfrage).ergebnis()
        } catch (e: Serverfehler) {
            throw e.uebersetzt()
        }
        val sitzung = Sitzung(antwort.kontoId, antwort.geraetId, antwort.sitzung.token, antwort.sitzung.laeuftAb.seconds)
        ablage.speichern(sitzung)
        return Angemeldet(sitzung, antwort.neuesKonto, antwort.freigeschaltet)
    }

    /** Holt eine neue Sitzung mit einer Signatur des Geräteschlüssels und merkt sie sich. */
    suspend fun erneuern(): Sitzung {
        val alt = ablage.lesen() ?: throw NichtAngemeldet()
        val jetzt = uhr()
        val anfrage = SitzungErneuernRequest.newBuilder()
            .setGeraetId(geraetId)
            .setZeitpunkt(Timestamp.newBuilder().setSeconds(jetzt))
            .setSignatur(ByteString.copyFrom(schluessel.signiere(Geraetenachrichten.sitzung(geraetId, jetzt))))
            .build()
        val antwort = try {
            verbindung.anmelde.sitzungErneuern(anfrage).ergebnis()
        } catch (e: Serverfehler) {
            throw e.uebersetzt()
        }
        val neu = alt.copy(geraetId = geraetId, token = antwort.sitzung.token, laeuftAbUnix = antwort.sitzung.laeuftAb.seconds)
        ablage.speichern(neu)
        return neu
    }

    /**
     * Eine gültige Sitzung: die gemerkte, solange sie noch mindestens [VORLAUF]
     * Sekunden gilt, sonst eine erneuerte. So läuft kein Aufruf mit einem Token,
     * das unterwegs abläuft.
     */
    suspend fun sitzung(): Sitzung {
        val gemerkt = ablage.lesen() ?: throw NichtAngemeldet()
        return if (gemerkt.laeuftAbUnix - uhr() >= VORLAUF) gemerkt else erneuern()
    }

    private fun Serverfehler.uebersetzt(): Exception =
        when {
            !vomDienst -> this
            code == Code.UNAUTHENTICATED -> AnmeldungAbgelehnt()
            code == Code.PERMISSION_DENIED -> GeraetGesperrt()
            else -> this
        }

    companion object {
        /** So viele Sekunden vor dem Ablauf wird die Sitzung erneuert. */
        const val VORLAUF = 120L
    }
}
