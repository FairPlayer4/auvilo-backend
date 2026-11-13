package com.avlreminder.verbindung

import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.schnittstelle.v1.SitzungErneuernRequest
import com.connectrpc.Code
import com.google.protobuf.ByteString
import com.google.protobuf.Timestamp
import java.security.SecureRandom
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

/**
 * Wo das Gerät seine Sitzung ablegt (Test-Client: Datei, App: DataStore).
 * `suspend`, weil DataStore nur asynchron liest und schreibt.
 */
interface SitzungsAblage {
    suspend fun lesen(): Sitzung?
    suspend fun speichern(sitzung: Sitzung)
}

/**
 * Eine Anmeldung mit Google: Mit [nonce] holt der Aufrufer das ID-Token, und
 * [Anmeldung.anmelden] schickt [zufall] mit, damit der Server die Nonce
 * nachrechnen kann. Für jede Anmeldung ein neuer Versuch (siehe [Anmeldung.versuch]).
 */
class Anmeldeversuch internal constructor(val zufall: ByteArray, oeffentlich: ByteArray) {
    val nonce: String = Geraetenachrichten.nonce(oeffentlich, zufall)
}

/**
 * Ergebnis von [Anmeldung.anmelden]. [loeschenAb]: Termin einer beantragten Löschung des
 * Kontos (B1), sonst null.
 */
data class Angemeldet(val sitzung: Sitzung, val neuesKonto: Boolean, val freigeschaltet: Boolean, val loeschenAb: Instant? = null)

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
 * Browser, App: Credential Manager), und zwar mit der Nonce eines frischen
 * [versuch].
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
    /** Quelle für den Zufall der Nonce. */
    private val zufall: SecureRandom = SecureRandom(),
) {
    /**
     * Termin einer beantragten Löschung des Kontos (B1) aus der letzten Antwort von
     * [anmelden] oder [erneuern], sonst null. Die App zeigt ihn an, damit der Besitzer
     * einen Antrag aus einem übernommenen Google-Konto bemerkt und abbricht.
     */
    var loeschenAb: Instant? = null
        private set

    /** Die Geräte-ID, wie der Server sie aus dem Schlüssel ableitet. */
    val geraetId: String get() = Geraetenachrichten.geraetId(schluessel.oeffentlich)

    /**
     * Ein neuer Anmeldeversuch mit frischem Zufall.
     *
     * KRITISCH: Jede Anmeldung braucht einen neuen Versuch. Mit derselben Nonce
     * gibt Google bis zu einer Stunde lang dasselbe ID-Token zurück, und das
     * lehnt der Server nach 5 Minuten als zu alt ab.
     */
    fun versuch(): Anmeldeversuch =
        Anmeldeversuch(ByteArray(Geraetenachrichten.NONCE_ZUFALL_LAENGE).also(zufall::nextBytes), schluessel.oeffentlich)

    /** Meldet das Gerät mit einem ID-Token an, das mit der Nonce von [versuch] geholt wurde, und merkt sich die Sitzung. */
    suspend fun anmelden(versuch: Anmeldeversuch, idToken: String): Angemeldet {
        val anfrage = AnmeldenRequest.newBuilder()
            .setGoogleIdToken(idToken)
            .setGeraeteschluessel(ByteString.copyFrom(schluessel.oeffentlich))
            .setSignatur(ByteString.copyFrom(schluessel.signiere(Geraetenachrichten.anmelden(idToken))))
            .setNonceZufall(ByteString.copyFrom(versuch.zufall))
            .build()
        val antwort = try {
            verbindung.anmelde.anmelden(anfrage).ergebnis()
        } catch (e: Serverfehler) {
            throw e.uebersetzt()
        }
        val sitzung = Sitzung(antwort.kontoId, antwort.geraetId, antwort.sitzung.token, antwort.sitzung.laeuftAb.seconds)
        ablage.speichern(sitzung)
        loeschenAb = if (antwort.hasLoeschenAb()) antwort.loeschenAb.instant() else null
        return Angemeldet(sitzung, antwort.neuesKonto, antwort.freigeschaltet, loeschenAb)
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
        loeschenAb = if (antwort.hasLoeschenAb()) antwort.loeschenAb.instant() else null
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

    /**
     * Führt [aufruf] mit einer gültigen Sitzung aus. Lehnt der Dienst die Sitzung ab
     * ([SitzungAbgelehnt]), erneuert es sie einmal mit dem Geräteschlüssel und ruft
     * [aufruf] noch einmal auf.
     *
     * KRITISCH: Erst das Erneuern sagt, warum die Sitzung nicht mehr gilt: Ist das Gerät
     * gesperrt, wirft es [GeraetGesperrt], sonst [AnmeldungAbgelehnt] (neu mit Google
     * anmelden), oder es klappt (Sitzungen auf dem Server gelöscht). Ein zweites
     * [SitzungAbgelehnt] geht an den Aufrufer; es wird nicht endlos erneuert.
     * [aufruf] muss sich wiederholen lassen.
     */
    suspend fun <T> mitSitzung(aufruf: suspend (Sitzung) -> T): T {
        val sitzung = sitzung()
        return try {
            aufruf(sitzung)
        } catch (_: SitzungAbgelehnt) {
            aufruf(erneuern())
        }
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
