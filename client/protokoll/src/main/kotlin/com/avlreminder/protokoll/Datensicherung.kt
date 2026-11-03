package com.avlreminder.protokoll

import com.avlreminder.protokoll.inhalt.v1.Datensicherung as DatensicherungProto
import com.google.protobuf.ByteString
import com.google.protobuf.InvalidProtocolBufferException
import java.time.Instant

/**
 * Kopf einer Datensicherung: steht mit im Chiffrat, der Server kann ihn weder
 * lesen noch fälschen (Entscheidung E-B2b-2).
 */
data class Sicherungskopf(
    val zaehler: Long,
    val erstellt: Instant,
    val geraetId: String,
    val schemaVersion: Int,
)

/** Eine entschlüsselte Datensicherung. */
class Sicherung(val kopf: Sicherungskopf, val nutzlast: ByteArray)

/**
 * Verschlüsselt und entschlüsselt Datensicherungen mit dem Tresorschlüssel
 * (Format: proto/avlreminder/inhalt/v1/datensicherung.proto, docs/formate.md).
 */
object Datensicherung {
    /** Höchstens so groß darf das Chiffrat sein; der Server prüft dasselbe (E-B2b-7). */
    const val MAX_CHIFFRAT = 8 * 1024 * 1024

    fun verschluesseln(sicherung: Sicherung, tresor: SymmetrischerSchluessel, kontoId: String): ByteArray {
        val klartext = DatensicherungProto.newBuilder()
            .setZaehler(sicherung.kopf.zaehler)
            .setErstelltUnix(sicherung.kopf.erstellt.epochSecond)
            .setGeraetId(sicherung.kopf.geraetId)
            .setSchemaVersion(sicherung.kopf.schemaVersion)
            .setNutzlast(ByteString.copyFrom(sicherung.nutzlast))
            .build()
            .toByteArray()
        return tresor.verschluesseln(klartext, zugehoerig(kontoId))
    }

    /**
     * KRITISCH: Gelingt nur mit dem Tresorschlüssel dieser Person, der Sicherung
     * genau dieses Kontos und unveränderten Bytes. Sonst [EntschluesselnFehlgeschlagen].
     */
    fun entschluesseln(chiffrat: ByteArray, tresor: SymmetrischerSchluessel, kontoId: String): Sicherung {
        val klartext = tresor.entschluesseln(chiffrat, zugehoerig(kontoId))
        val p = try {
            DatensicherungProto.parseFrom(klartext)
        } catch (e: InvalidProtocolBufferException) {
            throw EntschluesselnFehlgeschlagen("Datensicherung hat ein unbekanntes Format", e)
        }
        return Sicherung(
            Sicherungskopf(p.zaehler, Instant.ofEpochSecond(p.erstelltUnix), p.geraetId, p.schemaVersion),
            p.nutzlast.toByteArray(),
        )
    }

    private fun zugehoerig(kontoId: String): ByteArray = (Zweck.DATENSICHERUNG + kontoId).toByteArray()
}
