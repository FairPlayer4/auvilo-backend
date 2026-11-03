package com.avlreminder.protokoll

import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.condition.EnabledIfSystemProperty
import java.io.File
import java.time.Instant
import java.util.Base64

/**
 * KRITISCH: Die App muss Backups und Sicherungen lesen können, die eine ältere
 * Version geschrieben hat. Ein Test, der nur verschlüsselt und gleich wieder
 * entschlüsselt, merkt eine Formatänderung nicht; diese festen Bytes schon.
 * Schlägt ein Test hier fehl, ist das Format kaputt – nicht die Testdatei.
 */
class InhaltTestvektorenTest {

    private val datei = File(System.getProperty("testvektoren"), "inhalt.json")
    private val v: InhaltVektoren by lazy { Json.decodeFromString(datei.readText()) }
    private val b64 = Base64.getDecoder()
    private val bund by lazy {
        Schluesselbund.ausBackup(
            b64.decode(v.schluessel_backup_base64),
            Wiederherstellungsschluessel.ausText(v.wiederherstellungsschluessel),
            v.konto_id,
        )
    }

    @Test
    fun `das feste Schlüssel-Backup öffnet sich mit dem festen Wiederherstellungsschlüssel`() {
        assertEquals(v.fingerabdruck, bund.person.oeffentlich.fingerabdruck())
    }

    @Test
    fun `die feste Datensicherung öffnet sich mit dem Tresorschlüssel aus dem Backup`() {
        val s = Datensicherung.entschluesseln(b64.decode(v.datensicherung.chiffrat_base64), bund.tresor, v.konto_id)
        val d = v.datensicherung
        assertEquals(Sicherungskopf(d.zaehler, Instant.ofEpochSecond(d.erstellt_unix), d.geraet_id, d.schema_version), s.kopf)
        assertArrayEquals(d.nutzlast_text.toByteArray(), s.nutzlast)
    }

    @Test
    fun `der feste Umschlag öffnet sich mit dem Personenschlüssel aus dem Backup`() {
        val u = v.umschlag
        val freigabe = bund.person.entpacken(b64.decode(u.umschlag_base64))
        assertArrayEquals(u.probe_klartext.toByteArray(), freigabe.entschluesseln(b64.decode(u.probe_base64), u.probe_zugehoerig.toByteArray()))
    }

    @Test
    fun `die feste Signatur gilt für den Personenschlüssel aus dem Backup`() {
        val s = v.signatur
        assertTrue(bund.person.oeffentlich.pruefen(Signaturzweck.valueOf(s.zweck), s.daten_text.toByteArray(), b64.decode(s.signatur_base64)))
    }

    // Kein Test, sondern das Werkzeug zum einmaligen Erzeugen; läuft nur mit
    // ./gradlew :protokoll:test -PtestvektorenErzeugen=ja --tests '*InhaltTestvektorenTest*'
    @Test
    @EnabledIfSystemProperty(named = "testvektoren.erzeugen", matches = "ja")
    fun `erzeugt die Testvektoren einmalig`() {
        InhaltTestvektorenErzeuger.schreiben(datei)
    }
}
