package com.avlreminder.protokoll

import com.avlreminder.protokoll.inhalt.v1.SchluesselBackup
import com.google.protobuf.ByteString
import com.google.protobuf.InvalidProtocolBufferException
import java.security.GeneralSecurityException
import java.security.MessageDigest

/**
 * Alle Schlüssel einer Person, die zwischen ihren Geräten wandern: Personen-
 * und Tresorschlüssel. Der Geräteschlüssel gehört nicht dazu, er bleibt auf
 * seinem Gerät.
 */
class Schluesselbund(
    val person: Personenschluessel,
    val tresor: SymmetrischerSchluessel,
) {
    /**
     * Verschlüsselt den Bund für das Backup auf dem Server (Entscheidung E-B2-4).
     * Die Konto-ID geht als zugehörige Daten ein: Ein Backup passt nur zu
     * seinem Konto.
     */
    fun backupVerschluesseln(wiederherstellung: Wiederherstellungsschluessel, kontoId: String): ByteArray =
        wiederherstellung.aead().encrypt(geheimeBytes(), zugehoerig(kontoId))

    /**
     * Alle geheimen Schlüssel im Klartext, zum Ablegen auf dem Gerät.
     * Nie unverschlüsselt verschicken. Die App legt sie später mit einem
     * Keystore-Schlüssel verschlüsselt ab; der Test-Client im Klartext (E-B2-9).
     */
    fun geheimeBytes(): ByteArray =
        SchluesselBackup.newBuilder()
            .setPersonEntpacken(ByteString.copyFrom(person.geheimeBytesEntpacken()))
            .setPersonSignieren(ByteString.copyFrom(person.geheimeBytesSignieren()))
            .setTresor(ByteString.copyFrom(tresor.geheimeBytes()))
            .build()
            .toByteArray()

    /**
     * Ob [anderer] genau dieselben Schlüssel enthält: Personenschlüssel und alle
     * Versionen des Tresors. Der Fingerabdruck allein reicht dafür nicht, er
     * deckt nur den öffentlichen Teil des Personenschlüssels ab, nicht den Tresor.
     * Verglichen werden die geheimen Bytes, ohne Abbruch beim ersten Unterschied.
     */
    fun gleicheSchluessel(anderer: Schluesselbund): Boolean =
        MessageDigest.isEqual(geheimeBytes(), anderer.geheimeBytes())

    companion object {
        /** Neue Schlüssel für das erste Gerät eines Kontos. */
        fun erzeugen() = Schluesselbund(Personenschluessel.erzeugen(), SymmetrischerSchluessel.erzeugen())

        /**
         * Entschlüsselt ein Backup vom Server.
         *
         * KRITISCH: Gelingt nur mit dem richtigen Wiederherstellungsschlüssel,
         * dem Backup genau dieses Kontos und unveränderten Bytes. Sonst
         * [EntschluesselnFehlgeschlagen] – nie ein halber Schlüsselbund.
         */
        fun ausBackup(
            chiffrat: ByteArray,
            wiederherstellung: Wiederherstellungsschluessel,
            kontoId: String,
        ): Schluesselbund {
            val klartext = try {
                wiederherstellung.aead().decrypt(chiffrat, zugehoerig(kontoId))
            } catch (e: GeneralSecurityException) {
                throw EntschluesselnFehlgeschlagen(
                    "Backup lässt sich nicht entschlüsseln (falscher Wiederherstellungsschlüssel oder fremdes Konto)",
                    e,
                )
            }
            return ausGeheimenBytes(klartext)
        }

        /**
         * Gegenstück zu [geheimeBytes].
         *
         * @throws EntschluesselnFehlgeschlagen bei unbekanntem Format oder ungültigen Schlüsseln.
         */
        fun ausGeheimenBytes(bytes: ByteArray): Schluesselbund =
            try {
                val backup = SchluesselBackup.parseFrom(bytes)
                Schluesselbund(
                    Personenschluessel.ausGeheimenBytes(
                        backup.personEntpacken.toByteArray(),
                        backup.personSignieren.toByteArray(),
                    ),
                    SymmetrischerSchluessel.ausGeheimenBytes(backup.tresor.toByteArray()),
                )
            } catch (e: InvalidProtocolBufferException) {
                throw EntschluesselnFehlgeschlagen("Schlüsselbund hat ein unbekanntes Format", e)
            } catch (e: GeneralSecurityException) {
                throw EntschluesselnFehlgeschlagen("Schlüsselbund enthält ungültige Schlüssel", e)
            }

        private fun zugehoerig(kontoId: String): ByteArray = (Zweck.BACKUP + kontoId).toByteArray()
    }
}
