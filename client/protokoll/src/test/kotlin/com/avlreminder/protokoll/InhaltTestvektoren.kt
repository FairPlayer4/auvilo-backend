package com.avlreminder.protokoll

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.security.SecureRandom
import java.time.Instant
import java.util.Base64

/**
 * Feste Testvektoren für die Formate, die nur Clients lesen (docs/formate.md,
 * Review-Empfehlung A3). Einmal erzeugt und eingecheckt; [InhaltTestvektorenTest]
 * prüft, dass jede künftige Version des Codes genau diese Bytes noch lesen kann.
 * Alle Schlüssel sind Wegwerf-Schlüssel nur für diese Datei.
 */
@Serializable
data class InhaltVektoren(
    val beschreibung: String,
    val konto_id: String,
    /** Wiederherstellungsschlüssel als Text (zum Ausdrucken). */
    val wiederherstellungsschluessel: String,
    /** Schlüssel-Backup (backup.proto), verschlüsselt mit dem Wiederherstellungsschlüssel. */
    val schluessel_backup_base64: String,
    /** Fingerabdruck des Personenschlüssels im Backup. */
    val fingerabdruck: String,
    val datensicherung: DatensicherungVektor,
    val umschlag: UmschlagVektor,
    val signatur: SignaturVektor,
)

@Serializable
data class DatensicherungVektor(
    /** Datensicherung (datensicherung.proto), verschlüsselt mit dem Tresorschlüssel aus dem Backup. */
    val chiffrat_base64: String,
    val zaehler: Long,
    val erstellt_unix: Long,
    val geraet_id: String,
    val schema_version: Int,
    val nutzlast_text: String,
)

@Serializable
data class UmschlagVektor(
    /** Ein Freigabe-Schlüssel, verpackt für den Personenschlüssel aus dem Backup. */
    val umschlag_base64: String,
    /** Probe, mit dem verpackten Schlüssel verschlüsselt; zugehörige Daten: [probe_zugehoerig]. */
    val probe_base64: String,
    val probe_zugehoerig: String,
    val probe_klartext: String,
)

@Serializable
data class SignaturVektor(
    val zweck: String,
    val daten_text: String,
    /** Signatur mit dem Personenschlüssel aus dem Backup. */
    val signatur_base64: String,
)

/** Erzeugt die Vektoren (nur über `./gradlew :protokoll:test -PtestvektorenErzeugen=ja`, siehe Test). */
internal object InhaltTestvektorenErzeuger {
    fun erzeugen(): InhaltVektoren {
        val b64 = Base64.getEncoder()
        val konto = "testkonto-0123456789abcdef"
        val wiederherstellung = Wiederherstellungsschluessel.ausBytes(ByteArray(32).also { SecureRandom().nextBytes(it) })
        val bund = Schluesselbund.erzeugen()

        val sicherung = Sicherung(Sicherungskopf(42, Instant.ofEpochSecond(1_793_000_000), "d0e5518e666349cb0a3637c6b536b1cd", 5), "Testdaten für die Sicherung".toByteArray())
        val freigabe = SymmetrischerSchluessel.erzeugen()
        val signaturDaten = "Operation zum Testen"

        return InhaltVektoren(
            beschreibung = "Feste Testvektoren für die Client-Formate (docs/formate.md). Einmal erzeugt; " +
                "InhaltTestvektorenTest prüft, dass der Code sie weiter lesen kann. Wegwerf-Schlüssel, keine echten Daten. " +
                "Nie neu erzeugen: Ändert sich ein Format, kommt eine neue Formatversion mit eigenen Vektoren dazu.",
            konto_id = konto,
            wiederherstellungsschluessel = wiederherstellung.alsText(),
            schluessel_backup_base64 = b64.encodeToString(bund.backupVerschluesseln(wiederherstellung, konto)),
            fingerabdruck = bund.person.oeffentlich.fingerabdruck(),
            datensicherung = DatensicherungVektor(
                chiffrat_base64 = b64.encodeToString(Datensicherung.verschluesseln(sicherung, bund.tresor, konto)),
                zaehler = sicherung.kopf.zaehler,
                erstellt_unix = sicherung.kopf.erstellt.epochSecond,
                geraet_id = sicherung.kopf.geraetId,
                schema_version = sicherung.kopf.schemaVersion,
                nutzlast_text = String(sicherung.nutzlast),
            ),
            umschlag = UmschlagVektor(
                umschlag_base64 = b64.encodeToString(bund.person.oeffentlich.verpacken(freigabe)),
                probe_base64 = b64.encodeToString(freigabe.verschluesseln("Probe".toByteArray(), "freigabe-1".toByteArray())),
                probe_zugehoerig = "freigabe-1",
                probe_klartext = "Probe",
            ),
            signatur = SignaturVektor(
                zweck = Signaturzweck.OPERATION.name,
                daten_text = signaturDaten,
                signatur_base64 = b64.encodeToString(bund.person.signieren(Signaturzweck.OPERATION, signaturDaten.toByteArray())),
            ),
        )
    }

    /**
     * Der Schlüsselbund aus dem v1-Backup in inhalt.json. Liest über den
     * v1-Rückfall von [Schluesselbund.ausBackup]; fällt der mit der
     * Sicherheitsphase weg, kommt der Bund aus backup-v2.json.
     */
    fun bundAusV1(verzeichnis: File): Schluesselbund {
        val v = Json.decodeFromString<InhaltVektoren>(File(verzeichnis, "inhalt.json").readText())
        return Schluesselbund.ausBackup(
            Base64.getDecoder().decode(v.schluessel_backup_base64),
            Wiederherstellungsschluessel.ausText(v.wiederherstellungsschluessel),
            v.konto_id,
        )
    }

    /** Schreibt die Datei, aber nie über eine vorhandene. */
    fun schreiben(datei: File) {
        check(!datei.exists()) { "$datei gibt es schon - Testvektoren werden nie neu erzeugt" }
        datei.writeText(json.encodeToString(InhaltVektoren.serializer(), erzeugen()) + "\n")
    }

    private val json = Json { prettyPrint = true }
}
