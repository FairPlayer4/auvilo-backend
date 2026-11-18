package com.avlreminder.protokoll

import com.google.crypto.tink.HybridDecrypt
import com.google.crypto.tink.HybridEncrypt
import com.google.crypto.tink.InsecureSecretKeyAccess
import com.google.crypto.tink.KeysetHandle
import com.google.crypto.tink.PublicKeySign
import com.google.crypto.tink.PublicKeyVerify
import com.google.crypto.tink.TinkProtoKeysetFormat
import com.google.crypto.tink.hybrid.HpkeParameters
import com.google.crypto.tink.signature.PredefinedSignatureParameters
import java.security.GeneralSecurityException
import java.security.MessageDigest

/**
 * Personenschlüssel (Kap. 4.1): Liegt auf allen eigenen Geräten und besteht aus
 * zwei Schlüsselpaaren, weil Tink Verschlüsseln und Signieren trennt:
 *  - **Entpacken** (HPKE mit X25519): Andere verpacken Freigabe-Schlüssel für
 *    mich; nur ich kann sie entpacken.
 *  - **Signieren** (Ed25519): Ich unterschreibe Operationen und Kontaktkarten.
 */
class Personenschluessel private constructor(
    private val entpacken: KeysetHandle,
    private val signieren: KeysetHandle,
) {
    /** Der öffentliche Teil, den andere bekommen (später per QR-Kontakt, B3). */
    val oeffentlich: OeffentlicherPersonenschluessel by lazy {
        OeffentlicherPersonenschluessel(entpacken.publicKeysetHandle, signieren.publicKeysetHandle)
    }

    /** Signiert [daten] für [zweck]. Prüfen kann jeder mit [oeffentlich]. */
    fun signieren(zweck: Signaturzweck, daten: ByteArray): ByteArray =
        signieren.getPrimitive(Tink.konfiguration, PublicKeySign::class.java)
            .sign(zweck.praefix.toByteArray() + daten)

    /**
     * Entpackt einen Schlüssel, den jemand mit [OeffentlicherPersonenschluessel.verpacken]
     * für diese Person verpackt hat.
     *
     * KRITISCH: Gelingt nur mit dem passenden privaten Schlüssel und unveränderten Bytes.
     * Sonst [EntschluesselnFehlgeschlagen], auch für einen echten Umschlag ohne Schlüssel
     * darin: HPKE prüft den Absender nicht, bauen kann ihn jeder mit [oeffentlich].
     */
    fun entpacken(umschlag: ByteArray): SymmetrischerSchluessel = try {
        val bytes = entpacken.getPrimitive(Tink.konfiguration, HybridDecrypt::class.java)
            .decrypt(umschlag, Zweck.UMSCHLAG.toByteArray())
        SymmetrischerSchluessel.ausGeheimenBytes(bytes)
    } catch (e: GeneralSecurityException) {
        throw EntschluesselnFehlgeschlagen("Umschlag lässt sich nicht entpacken", e)
    }

    internal fun geheimeBytesEntpacken(): ByteArray =
        TinkProtoKeysetFormat.serializeKeyset(entpacken, InsecureSecretKeyAccess.get(), Tink.konfiguration)

    internal fun geheimeBytesSignieren(): ByteArray =
        TinkProtoKeysetFormat.serializeKeyset(signieren, InsecureSecretKeyAccess.get(), Tink.konfiguration)

    override fun toString() = "Personenschluessel(${oeffentlich.fingerabdruck()})"

    companion object {
        // HPKE (RFC 9180) mit X25519 und ChaCha20-Poly1305, wie Kap. 4.1. TINK =
        // mit Schlüssel-ID vorn im Chiffrat, damit später mehrere Versionen gehen.
        private val HPKE: HpkeParameters = HpkeParameters.builder()
            .setKemId(HpkeParameters.KemId.DHKEM_X25519_HKDF_SHA256)
            .setKdfId(HpkeParameters.KdfId.HKDF_SHA256)
            .setAeadId(HpkeParameters.AeadId.CHACHA20_POLY1305)
            .setVariant(HpkeParameters.Variant.TINK)
            .build()

        fun erzeugen(): Personenschluessel {
            return Personenschluessel(
                KeysetHandle.generateNew(HPKE, Tink.konfiguration),
                KeysetHandle.generateNew(PredefinedSignatureParameters.ED25519, Tink.konfiguration),
            )
        }

        internal fun ausGeheimenBytes(entpacken: ByteArray, signieren: ByteArray): Personenschluessel {
            return Personenschluessel(
                TinkProtoKeysetFormat.parseKeyset(entpacken, InsecureSecretKeyAccess.get(), Tink.konfiguration),
                TinkProtoKeysetFormat.parseKeyset(signieren, InsecureSecretKeyAccess.get(), Tink.konfiguration),
            )
        }
    }
}

/** Öffentlicher Teil eines Personenschlüssels: verpacken für diese Person, ihre Signaturen prüfen. */
class OeffentlicherPersonenschluessel internal constructor(
    private val verpacken: KeysetHandle,
    private val pruefen: KeysetHandle,
) {
    /** Verpackt [schluessel] so, dass nur diese Person ihn entpacken kann. */
    fun verpacken(schluessel: SymmetrischerSchluessel): ByteArray =
        verpacken.getPrimitive(Tink.konfiguration, HybridEncrypt::class.java)
            .encrypt(schluessel.geheimeBytes(), Zweck.UMSCHLAG.toByteArray())

    /**
     * KRITISCH: true nur, wenn [signatur] mit dem privaten Gegenstück für genau
     * diesen [zweck] und genau diese [daten] erstellt wurde.
     */
    fun pruefen(zweck: Signaturzweck, daten: ByteArray, signatur: ByteArray): Boolean =
        try {
            pruefen.getPrimitive(Tink.konfiguration, PublicKeyVerify::class.java)
                .verify(signatur, zweck.praefix.toByteArray() + daten)
            true
        } catch (_: GeneralSecurityException) {
            false
        }

    /**
     * Fingerabdruck zum Vergleichen beim Kontakt (B3): SHA-256 über beide
     * öffentlichen Keysets, die ersten 16 Bytes als Base32 in Vierergruppen.
     */
    fun fingerabdruck(): String {
        val sha = MessageDigest.getInstance("SHA-256")
        for (teil in listOf(verpacken, pruefen)) {
            val bytes = TinkProtoKeysetFormat.serializeKeysetWithoutSecret(teil, Tink.konfiguration)
            // Länge vorweg, damit die Grenze zwischen den Teilen eindeutig ist.
            sha.update(java.nio.ByteBuffer.allocate(4).putInt(bytes.size).array())
            sha.update(bytes)
        }
        return Base32.kodiere(sha.digest().copyOf(16)).chunked(4).joinToString("-")
    }
}
