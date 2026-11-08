package com.avlreminder.protokoll

import java.security.MessageDigest
import java.util.Base64
import java.util.HexFormat

/**
 * Was ein Gerät mit seinem Geräteschlüssel (ECDSA P-256) signiert und wie der
 * Server daraus Geräte-ID und Nonce ableitet. Muss Byte für Byte zum Go-Server
 * passen (server/internal/geraeteschluessel); gesichert über die gemeinsamen
 * Testvektoren in proto/testvektoren.
 *
 * Signieren selbst tut der Aufrufer: im Test-Client mit Java-Krypto, in der App
 * mit dem Android Keystore ("SHA256withECDSA", Signatur ASN.1/DER).
 */
object Geraetenachrichten {
    private const val ZWECK_ANMELDEN = "avlreminder/anmelden/v1\n"
    private const val ZWECK_SITZUNG = "avlreminder/sitzung/v1\n"

    /** Geräte-ID: die ersten 16 Bytes von SHA-256 über den Schlüssel (PKIX/DER), hexadezimal. */
    fun geraetId(oeffentlichDer: ByteArray): String =
        HexFormat.of().formatHex(sha256(oeffentlichDer).copyOf(16))

    /** So viele zufällige Bytes gehören zu jeder Nonce. */
    const val NONCE_ZUFALL_LAENGE = 16

    /**
     * Nonce für den Google-Login: SHA-256 über den Schlüssel und dahinter [zufall]
     * ([NONCE_ZUFALL_LAENGE] Bytes, je Anmeldung neu), base64url ohne Auffüllung.
     * Bei gleicher Nonce gäbe Google bis zu einer Stunde lang dasselbe, für den
     * Server zu alte ID-Token zurück.
     */
    fun nonce(oeffentlichDer: ByteArray, zufall: ByteArray): String {
        require(zufall.size == NONCE_ZUFALL_LAENGE) { "Zufallsanteil der Nonce braucht $NONCE_ZUFALL_LAENGE Bytes" }
        return Base64.getUrlEncoder().withoutPadding().encodeToString(sha256(oeffentlichDer + zufall))
    }

    /** Wird beim Anmelden signiert. */
    fun anmelden(idToken: String): ByteArray = (ZWECK_ANMELDEN + idToken).toByteArray()

    /** Wird zum Erneuern der Sitzung signiert; der Zeitpunkt in ganzen Unix-Sekunden. */
    fun sitzung(geraetId: String, unixSekunden: Long): ByteArray =
        (ZWECK_SITZUNG + geraetId + "\n" + unixSekunden).toByteArray()

    private fun sha256(bytes: ByteArray) = MessageDigest.getInstance("SHA-256").digest(bytes)
}
