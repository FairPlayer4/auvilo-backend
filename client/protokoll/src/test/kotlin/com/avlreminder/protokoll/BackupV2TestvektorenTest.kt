package com.avlreminder.protokoll

import com.google.crypto.tink.InsecureSecretKeyAccess
import com.google.crypto.tink.aead.XChaCha20Poly1305Key
import com.google.crypto.tink.aead.subtle.XChaCha20Poly1305Aead
import com.google.crypto.tink.util.SecretBytes
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.condition.EnabledIfSystemProperty
import java.io.File
import java.util.Base64
import java.util.HexFormat
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * Feste Testvektoren für das Schlüssel-Backup im Format v2 (SE6, docs/formate.md).
 * Derselbe Wegwerf-Schlüsselbund und Wiederherstellungsschlüssel wie das
 * v1-Backup in inhalt.json, nur neu verschlüsselt.
 */
@Serializable
data class BackupV2Vektoren(
    val beschreibung: String,
    val konto_id: String,
    val wiederherstellungsschluessel: String,
    /** Dieselben 32 Bytes als Hex, für die unabhängige Ableitung im Test. */
    val wiederherstellungsschluessel_hex: String,
    /** HKDF-SHA256(Wiederherstellungsschlüssel, Salz leer, info "avlreminder/backupschluessel/v2", 32). */
    val backupschluessel_hex: String,
    val schluessel_backup_base64: String,
    /** Fingerabdruck des Personenschlüssels; derselbe wie in inhalt.json. */
    val fingerabdruck: String,
)

/**
 * KRITISCH: Die App muss v2-Backups lesen können, die eine ältere Version
 * geschrieben hat, und der Backup-Schlüssel muss genau die dokumentierte
 * Ableitung sein. Schlägt ein Test hier fehl, ist das Format kaputt – nicht
 * die Testdatei.
 */
class BackupV2TestvektorenTest {

    private val verzeichnis = File(System.getProperty("testvektoren"))
    private val datei = File(verzeichnis, "backup-v2.json")
    private val v: BackupV2Vektoren by lazy { Json.decodeFromString(datei.readText()) }
    private val b64 = Base64.getDecoder()
    private val hex = HexFormat.of()
    private val schoen = Json { prettyPrint = true }

    @Test
    fun `das feste v2-Backup öffnet sich mit dem festen Wiederherstellungsschlüssel`() {
        val bund = Schluesselbund.ausBackup(
            b64.decode(v.schluessel_backup_base64),
            Wiederherstellungsschluessel.ausText(v.wiederherstellungsschluessel),
            v.konto_id,
        )
        assertEquals(v.fingerabdruck, bund.person.oeffentlich.fingerabdruck())
    }

    @Test
    fun `das feste v2-Backup enthält denselben Schlüsselbund wie das v1-Backup`() {
        val wh = Wiederherstellungsschluessel.ausText(v.wiederherstellungsschluessel)
        val ausV2 = Schluesselbund.ausBackup(b64.decode(v.schluessel_backup_base64), wh, v.konto_id)
        assertTrue(ausV2.gleicheSchluessel(InhaltTestvektorenErzeuger.bundAusV1(verzeichnis)))
    }

    // KRITISCH: Die Ableitung ist hier unabhängig von Tink nachgerechnet
    // (HKDF nach RFC 5869 mit javax.crypto), mit Label und zugehörigen Daten im
    // Klartext. Ändert jemand Label, Länge oder Zweck, wird dieser Test rot.
    @Test
    fun `der Backup-Schlüssel ist HKDF-SHA256 mit eigenem Label`() {
        val roh = hex.parseHex(v.wiederherstellungsschluessel_hex)
        assertEquals(v.wiederherstellungsschluessel, Wiederherstellungsschluessel.ausBytes(roh).alsText())

        val abgeleitet = hkdfSha256(roh, "avlreminder/backupschluessel/v2".toByteArray(), 32)
        assertEquals(v.backupschluessel_hex, hex.formatHex(abgeleitet))

        val aead = XChaCha20Poly1305Aead.create(XChaCha20Poly1305Key.create(SecretBytes.copyFrom(abgeleitet, InsecureSecretKeyAccess.get())))
        val klartext = aead.decrypt(b64.decode(v.schluessel_backup_base64), ("avlreminder/backup/v2\n" + v.konto_id).toByteArray())
        assertEquals(v.fingerabdruck, Schluesselbund.ausGeheimenBytes(klartext).person.oeffentlich.fingerabdruck())
    }

    // Kein Test, sondern das Werkzeug zum einmaligen Erzeugen; läuft nur mit
    // ./gradlew :protokoll:test -PtestvektorenErzeugen=ja --tests '*BackupV2TestvektorenTest*'
    @Test
    @EnabledIfSystemProperty(named = "testvektoren.erzeugen", matches = "ja")
    fun `erzeugt die Testvektoren einmalig`() {
        check(!datei.exists()) { "$datei gibt es schon - Testvektoren werden nie neu erzeugt" }
        val inhalt = Json.decodeFromString<InhaltVektoren>(File(verzeichnis, "inhalt.json").readText())
        val wh = Wiederherstellungsschluessel.ausText(inhalt.wiederherstellungsschluessel)
        val roh = Base32.dekodiere(inhalt.wiederherstellungsschluessel.replace("-", "").take(52), 32)
        val bund = InhaltTestvektorenErzeuger.bundAusV1(verzeichnis)
        val vektoren = BackupV2Vektoren(
            beschreibung = "Feste Testvektoren für das Schlüssel-Backup im Format v2 (docs/formate.md, SE6): " +
                "derselbe Wegwerf-Schlüsselbund wie das v1-Backup in inhalt.json, verschlüsselt mit dem " +
                "abgeleiteten Backup-Schlüssel. Keine echten Daten. Nie neu erzeugen.",
            konto_id = inhalt.konto_id,
            wiederherstellungsschluessel = inhalt.wiederherstellungsschluessel,
            wiederherstellungsschluessel_hex = hex.formatHex(roh),
            backupschluessel_hex = hex.formatHex(hkdfSha256(roh, "avlreminder/backupschluessel/v2".toByteArray(), 32)),
            schluessel_backup_base64 = Base64.getEncoder().encodeToString(bund.backupVerschluesseln(wh, inhalt.konto_id)),
            fingerabdruck = bund.person.oeffentlich.fingerabdruck(),
        )
        datei.writeText(schoen.encodeToString(BackupV2Vektoren.serializer(), vektoren) + "\n")
    }

    /** HKDF-SHA256 nach RFC 5869 mit leerem Salz; für [laenge] ≤ 32 reicht ein Block. */
    private fun hkdfSha256(ikm: ByteArray, info: ByteArray, laenge: Int): ByteArray {
        require(laenge <= 32)
        val extract = Mac.getInstance("HmacSHA256").apply { init(SecretKeySpec(ByteArray(32), "HmacSHA256")) }
        val prk = extract.doFinal(ikm)
        val expand = Mac.getInstance("HmacSHA256").apply { init(SecretKeySpec(prk, "HmacSHA256")) }
        return expand.doFinal(info + byteArrayOf(1)).copyOf(laenge)
    }
}
