package com.avlreminder.protokoll

import com.google.crypto.tink.Aead
import com.google.crypto.tink.InsecureSecretKeyAccess
import com.google.crypto.tink.aead.XChaCha20Poly1305Key
import com.google.crypto.tink.aead.subtle.XChaCha20Poly1305Aead
import com.google.crypto.tink.subtle.Ed25519Sign
import com.google.crypto.tink.subtle.Hkdf
import com.google.crypto.tink.util.SecretBytes
import java.security.MessageDigest
import java.security.SecureRandom

/** Der eingetippte Text ist kein gültiger Wiederherstellungsschlüssel. */
class UngueltigerWiederherstellungsschluessel(grund: String) : Exception(grund)

/**
 * Wiederherstellungsschlüssel: 256 Bit Zufall, nur auf Papier (Anforderung ID4).
 * Er entschlüsselt das Schlüssel-Backup auf dem Server.
 *
 * Ein echter Schlüssel statt eines Passworts: Es braucht keine Schlüssel-
 * ableitung, und Raten ist aussichtslos (Entscheidung E-B2-3).
 *
 * Als Text: Crockford-Base32 (ohne die verwechselbaren Buchstaben I, L, O, U),
 * 52 Zeichen Schlüssel und 4 Prüfzeichen, in 14 Vierergruppen, z. B.
 * `K7QM-3XA9-…`. Beim Einlesen zählen Groß-/Kleinschreibung, Bindestriche und
 * Leerzeichen nicht; O gilt als 0, I und L als 1.
 */
class Wiederherstellungsschluessel private constructor(private val bytes: ByteArray) {

    /** Text zum Ausdrucken. */
    fun alsText(): String =
        (Base32.kodiere(bytes) + Base32.kodiere(pruefzeichen(bytes)).take(PRUEF_ZEICHEN))
            .chunked(4)
            .joinToString("-")

    /** Verschlüsselung mit diesem Schlüssel (XChaCha20-Poly1305). */
    internal fun aead(): Aead {
        val schluessel = XChaCha20Poly1305Key.create(SecretBytes.copyFrom(bytes, InsecureSecretKeyAccess.get()))
        return XChaCha20Poly1305Aead.create(schluessel)
    }

    /**
     * Öffentlicher Freischaltschlüssel (Ed25519, 32 Bytes). Geht mit dem
     * Schlüssel-Backup an den Server; aus ihm lässt sich nichts entschlüsseln.
     */
    fun freischaltschluessel(): ByteArray = Ed25519Sign.KeyPair.newKeyPairFromSeed(freischaltSeed()).publicKey

    /**
     * Signatur, mit der sich das Gerät [geraetId] beim Server freischaltet
     * (E-B2b-13). Der Server prüft sie gegen [freischaltschluessel].
     *
     * KRITISCH: Signiert wird Zweck + Geräte-ID. Die Signatur schaltet so nur
     * genau dieses Gerät frei und gilt für nichts anderes. Gemeinsame
     * Testvektoren mit dem Server: proto/testvektoren/freischaltung.json.
     */
    fun signiereFreischaltung(geraetId: String): ByteArray = Ed25519Sign(freischaltSeed()).sign(freischaltNachricht(geraetId))

    /**
     * Seed des Ed25519-Schlüssels: HKDF-SHA256 ohne Salz mit eigenem Zweck, damit
     * er nichts mit dem Schlüssel für das Backup gemein hat.
     */
    private fun freischaltSeed(): ByteArray =
        Hkdf.computeHkdf("HMACSHA256", bytes, ByteArray(0), Zweck.FREISCHALTSCHLUESSEL.toByteArray(), SEED_LAENGE)

    // Nie den Schlüssel in Logs oder Fehlermeldungen.
    override fun toString() = "Wiederherstellungsschluessel(***)"

    companion object {
        private const val LAENGE = 32 // Bytes = 256 Bit
        private const val SCHLUESSEL_ZEICHEN = 52 // 52 × 5 Bit = 260 Bit ≥ 256 Bit
        private const val PRUEF_ZEICHEN = 4 // 20 Bit aus SHA-256 des Schlüssels
        private const val SEED_LAENGE = 32 // Ed25519

        /** Die Bytes, die [signiereFreischaltung] signiert. */
        internal fun freischaltNachricht(geraetId: String): ByteArray = (Zweck.FREISCHALTEN + geraetId).toByteArray()

        fun erzeugen(): Wiederherstellungsschluessel =
            Wiederherstellungsschluessel(ByteArray(LAENGE).also { SecureRandom().nextBytes(it) })

        /** Nur für Tests: fester Schlüssel. */
        internal fun ausBytes(bytes: ByteArray): Wiederherstellungsschluessel {
            require(bytes.size == LAENGE)
            return Wiederherstellungsschluessel(bytes.copyOf())
        }

        /**
         * Liest einen eingetippten Schlüssel.
         *
         * KRITISCH: Nur ein vollständiger Schlüssel mit passenden Prüfzeichen
         * wird angenommen. Ein Tippfehler fällt so hier auf, nicht erst als
         * „Backup lässt sich nicht entschlüsseln“.
         */
        fun ausText(text: String): Wiederherstellungsschluessel {
            val zeichen = text.filterNot { it == '-' || it.isWhitespace() }.uppercase()
            if (zeichen.length != SCHLUESSEL_ZEICHEN + PRUEF_ZEICHEN) {
                throw UngueltigerWiederherstellungsschluessel(
                    "erwartet ${SCHLUESSEL_ZEICHEN + PRUEF_ZEICHEN} Zeichen, gefunden ${zeichen.length}",
                )
            }
            val bytes = Base32.dekodiere(zeichen.take(SCHLUESSEL_ZEICHEN), LAENGE)
            val erwartet = Base32.kodiere(pruefzeichen(bytes)).take(PRUEF_ZEICHEN)
            if (Base32.normalisiere(zeichen.takeLast(PRUEF_ZEICHEN)) != erwartet) {
                throw UngueltigerWiederherstellungsschluessel("Prüfzeichen passen nicht – bitte auf Tippfehler prüfen")
            }
            return Wiederherstellungsschluessel(bytes)
        }

        private fun pruefzeichen(bytes: ByteArray): ByteArray =
            MessageDigest.getInstance("SHA-256").digest(bytes).copyOf(3) // 24 Bit, davon 20 genutzt
    }
}

/** Crockford-Base32: Ziffern und Großbuchstaben ohne I, L, O, U. */
internal object Base32 {
    private const val ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

    fun kodiere(bytes: ByteArray): String {
        val text = StringBuilder()
        var puffer = 0
        var bits = 0
        for (b in bytes) {
            puffer = (puffer shl 8) or (b.toInt() and 0xff)
            bits += 8
            while (bits >= 5) {
                text.append(ALPHABET[(puffer shr (bits - 5)) and 31])
                bits -= 5
            }
        }
        if (bits > 0) text.append(ALPHABET[(puffer shl (5 - bits)) and 31]) // Rest mit Nullen auffüllen
        return text.toString()
    }

    /** Ersetzt verwechselbare Zeichen: O → 0, I und L → 1. */
    fun normalisiere(text: String): String =
        text.map {
            when (it) {
                'O' -> '0'
                'I', 'L' -> '1'
                else -> it
            }
        }.joinToString("")

    fun dekodiere(text: String, laenge: Int): ByteArray {
        val ergebnis = ByteArray(laenge)
        var puffer = 0
        var bits = 0
        var i = 0
        for (c in normalisiere(text)) {
            val wert = ALPHABET.indexOf(c)
            if (wert < 0) throw UngueltigerWiederherstellungsschluessel("unerlaubtes Zeichen '$c'")
            puffer = (puffer shl 5) or wert
            bits += 5
            if (bits >= 8) {
                if (i == laenge) throw UngueltigerWiederherstellungsschluessel("zu viele Zeichen")
                ergebnis[i++] = (puffer shr (bits - 8)).toByte()
                bits -= 8
            }
            puffer = puffer and ((1 shl bits) - 1) // nur die noch offenen Bits behalten
        }
        // Die Füllbits am Ende müssen 0 sein, sonst gäbe es zwei Texte für denselben Schlüssel.
        if (i != laenge || puffer != 0) throw UngueltigerWiederherstellungsschluessel("Zeichen passen nicht zusammen")
        return ergebnis
    }
}
