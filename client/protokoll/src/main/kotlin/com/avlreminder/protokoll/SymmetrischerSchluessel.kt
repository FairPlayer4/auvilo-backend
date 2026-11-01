package com.avlreminder.protokoll

import com.google.crypto.tink.Aead
import com.google.crypto.tink.InsecureSecretKeyAccess
import com.google.crypto.tink.KeysetHandle
import com.google.crypto.tink.TinkProtoKeysetFormat
import com.google.crypto.tink.aead.PredefinedAeadParameters
import java.security.GeneralSecurityException

/** Entschlüsseln ist gescheitert: falscher Schlüssel, falsche zugehörige Daten oder veränderte Bytes. */
class EntschluesselnFehlgeschlagen(grund: String, ursache: Throwable? = null) : Exception(grund, ursache)

/**
 * Symmetrischer Schlüssel für Tresor und Freigaben (Kap. 4.1): XChaCha20-Poly1305
 * als Tink-Keyset. Ein Keyset kann mehrere Versionen enthalten. Verschlüsselt
 * wird mit der neuesten, entschlüsseln geht mit allen – so bleiben alte Daten
 * nach einem Schlüsselwechsel lesbar.
 */
class SymmetrischerSchluessel private constructor(private val keyset: KeysetHandle) {

    private val aead: Aead by lazy { keyset.getPrimitive(Tink.konfiguration, Aead::class.java) }

    /** Anzahl der Versionen im Keyset. */
    val versionen: Int get() = keyset.size()

    /**
     * Verschlüsselt [klartext]. [zugehoerig] wird nicht verschlüsselt, aber
     * mitgeprüft: Entschlüsseln gelingt nur mit genau denselben Bytes (z. B.
     * die ID der Freigabe, damit niemand Chiffrate zwischen Freigaben vertauscht).
     */
    fun verschluesseln(klartext: ByteArray, zugehoerig: ByteArray): ByteArray = aead.encrypt(klartext, zugehoerig)

    /**
     * KRITISCH: Gibt nur Daten zurück, die mit einem Schlüssel dieses Keysets
     * und genau diesen zugehörigen Daten verschlüsselt und nicht verändert wurden.
     */
    fun entschluesseln(chiffrat: ByteArray, zugehoerig: ByteArray): ByteArray =
        try {
            aead.decrypt(chiffrat, zugehoerig)
        } catch (e: GeneralSecurityException) {
            throw EntschluesselnFehlgeschlagen("Daten lassen sich nicht entschlüsseln", e)
        }

    /** Neuer Schlüssel mit einer zusätzlichen Version, die ab jetzt verschlüsselt. */
    fun neueVersion(): SymmetrischerSchluessel {
        // Den neuen Schlüssel über die feste Tink-Konfiguration erzeugen und dann
        // übernehmen: Der Keyset-Builder allein kennt nur die globale Registry,
        // und die nutzen wir bewusst nicht (siehe Tink.kt).
        val belegt = (0 until keyset.size()).map { keyset.getAt(it).id }.toSet()
        var neu: KeysetHandle.Entry
        do {
            neu = KeysetHandle.generateNew(PredefinedAeadParameters.XCHACHA20_POLY1305, Tink.konfiguration).primary
        } while (neu.id in belegt) // Schlüssel-IDs sind 32 Bit Zufall; Kollision praktisch nie
        val eintrag = KeysetHandle.importKey(neu.key).withFixedId(neu.id).makePrimary()
        return SymmetrischerSchluessel(KeysetHandle.newBuilder(keyset).addEntry(eintrag).build())
    }

    /** Das Keyset mit dem geheimen Teil, z. B. für das Backup. Nie unverschlüsselt verschicken. */
    internal fun geheimeBytes(): ByteArray =
        TinkProtoKeysetFormat.serializeKeyset(keyset, InsecureSecretKeyAccess.get(), Tink.konfiguration)

    override fun toString() = "SymmetrischerSchluessel(versionen=$versionen)"

    companion object {
        fun erzeugen() = SymmetrischerSchluessel(
            KeysetHandle.generateNew(PredefinedAeadParameters.XCHACHA20_POLY1305, Tink.konfiguration),
        )

        internal fun ausGeheimenBytes(bytes: ByteArray) = SymmetrischerSchluessel(
            TinkProtoKeysetFormat.parseKeyset(bytes, InsecureSecretKeyAccess.get(), Tink.konfiguration),
        )
    }
}
