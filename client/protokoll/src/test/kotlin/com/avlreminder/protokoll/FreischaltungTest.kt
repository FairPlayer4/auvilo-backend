package com.avlreminder.protokoll

import com.google.crypto.tink.subtle.Ed25519Verify
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertDoesNotThrow
import org.junit.jupiter.api.assertThrows
import java.io.File
import java.security.GeneralSecurityException
import java.util.HexFormat

/**
 * Freischaltung mit dem Wiederherstellungsschlüssel (E-B2b-13). Prüft gegen die
 * gemeinsamen Testvektoren in proto/testvektoren; der Go-Server prüft dieselbe
 * Datei. Laufen beide grün, leiten App und Server denselben Schlüssel ab, und
 * der Server nimmt die Signatur der App an.
 */
class FreischaltungTest {

    @Serializable
    data class Vektoren(
        val beschreibung: String,
        val wiederherstellungsschluessel_hex: String,
        val freischaltschluessel_hex: String,
        val geraet_id: String,
        val nachricht_hex: String,
        val signatur_hex: String,
    )

    private val v: Vektoren = Json.decodeFromString(
        File(System.getProperty("testvektoren"), "freischaltung.json").readText(),
    )
    private val schluessel = Wiederherstellungsschluessel.ausBytes(hex(v.wiederherstellungsschluessel_hex))

    @Test
    fun `Freischaltschlüssel stimmt mit dem Server überein`() {
        assertArrayEquals(hex(v.freischaltschluessel_hex), schluessel.freischaltschluessel())
    }

    @Test
    fun `Nachricht stimmt Byte für Byte`() {
        assertArrayEquals(hex(v.nachricht_hex), Wiederherstellungsschluessel.freischaltNachricht(v.geraet_id))
    }

    // Ed25519 ist deterministisch: gleiche Eingabe, gleiche Signatur.
    @Test
    fun `Signatur stimmt Byte für Byte`() {
        assertArrayEquals(hex(v.signatur_hex), schluessel.signiereFreischaltung(v.geraet_id))
    }

    @Test
    fun `Signatur gilt mit dem Freischaltschlüssel`() {
        assertDoesNotThrow { pruefe(schluessel.freischaltschluessel(), v.geraet_id, schluessel.signiereFreischaltung(v.geraet_id)) }
    }

    // KRITISCH: Die Signatur schaltet nur das Gerät frei, für das sie gemacht ist.
    @Test
    fun `Signatur gilt nicht für ein anderes Gerät`() {
        assertThrows<GeneralSecurityException> {
            pruefe(schluessel.freischaltschluessel(), "00000000000000000000000000000000", schluessel.signiereFreischaltung(v.geraet_id))
        }
    }

    // KRITISCH: Ein anderer Wiederherstellungsschlüssel ergibt einen anderen
    // Freischaltschlüssel, und seine Signatur gilt nicht.
    @Test
    fun `anderer Wiederherstellungsschlüssel schaltet nicht frei`() {
        val anderer = Wiederherstellungsschluessel.erzeugen()
        assertFalse(anderer.freischaltschluessel().contentEquals(schluessel.freischaltschluessel()))
        assertThrows<GeneralSecurityException> {
            pruefe(schluessel.freischaltschluessel(), v.geraet_id, anderer.signiereFreischaltung(v.geraet_id))
        }
    }

    private fun pruefe(freischaltschluessel: ByteArray, geraetId: String, signatur: ByteArray) =
        Ed25519Verify(freischaltschluessel).verify(signatur, Wiederherstellungsschluessel.freischaltNachricht(geraetId))

    private fun hex(s: String) = HexFormat.of().parseHex(s)
}
