package com.avlreminder.protokoll

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import java.security.KeyFactory
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.util.Base64
import java.util.HexFormat

/**
 * Prüft gegen die gemeinsamen Testvektoren in proto/testvektoren. Der Go-Server
 * prüft dieselbe Datei; laufen beide Tests grün, signieren App und Server
 * Byte für Byte dasselbe.
 */
class GeraetenachrichtenTest {

    @Serializable
    data class Nachricht(
        val id_token: String = "",
        val zeitpunkt_unix: Long = 0,
        val nachricht_hex: String,
        val signatur_base64: String,
    )

    @Serializable
    data class Vektoren(
        val beschreibung: String,
        val geraeteschluessel_der_base64: String,
        val geraet_id: String,
        val nonce: String,
        val anmelden: Nachricht,
        val sitzung: Nachricht,
    )

    private val v: Vektoren = Json.decodeFromString(
        File(System.getProperty("testvektoren"), "geraetenachrichten.json").readText(),
    )
    private val der = Base64.getDecoder().decode(v.geraeteschluessel_der_base64)

    @Test
    fun `Geräte-ID stimmt mit dem Server überein`() {
        assertEquals(v.geraet_id, Geraetenachrichten.geraetId(der))
    }

    @Test
    fun `Nonce stimmt mit dem Server überein`() {
        assertEquals(v.nonce, Geraetenachrichten.nonce(der))
    }

    @Test
    fun `Anmelde-Nachricht stimmt Byte für Byte`() {
        assertArrayEquals(hex(v.anmelden.nachricht_hex), Geraetenachrichten.anmelden(v.anmelden.id_token))
    }

    @Test
    fun `Sitzungs-Nachricht stimmt Byte für Byte`() {
        assertArrayEquals(hex(v.sitzung.nachricht_hex), Geraetenachrichten.sitzung(v.geraet_id, v.sitzung.zeitpunkt_unix))
    }

    // Java prüft die Signaturen aus dem Vektor mit genau dem Verfahren, mit dem
    // Android Keystore und Test-Client signieren. Passt das Format nicht, wird es hier rot.
    @Test
    fun `Signaturen im Vektor gelten mit SHA256withECDSA`() {
        assertTrue(pruefe(hex(v.anmelden.nachricht_hex), v.anmelden.signatur_base64))
        assertTrue(pruefe(hex(v.sitzung.nachricht_hex), v.sitzung.signatur_base64))
    }

    @Test
    fun `Signatur der Anmeldung gilt nicht für die Sitzung`() {
        assertFalse(pruefe(hex(v.sitzung.nachricht_hex), v.anmelden.signatur_base64))
    }

    private fun pruefe(nachricht: ByteArray, signaturBase64: String): Boolean {
        val schluessel = KeyFactory.getInstance("EC").generatePublic(X509EncodedKeySpec(der))
        return Signature.getInstance("SHA256withECDSA").run {
            initVerify(schluessel)
            update(nachricht)
            verify(Base64.getDecoder().decode(signaturBase64))
        }
    }

    private fun hex(s: String) = HexFormat.of().parseHex(s)
}
