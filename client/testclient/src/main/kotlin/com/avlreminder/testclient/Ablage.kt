package com.avlreminder.testclient

import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.protokoll.Schluesselbund
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.PrivateKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.security.spec.PKCS8EncodedKeySpec
import java.util.Base64

/**
 * Ein simuliertes Gerät mit seinem Geräteschlüssel (ECDSA P-256). In der App
 * liegt der Schlüssel im Android Keystore; hier in einer Datei.
 */
class Geraet(private val privat: PrivateKey, val der: ByteArray) {
    val id: String get() = Geraetenachrichten.geraetId(der)

    /** Signatur im Format, das der Server erwartet (SHA-256, ASN.1/DER) – wie der Android Keystore. */
    fun signiere(nachricht: ByteArray): ByteArray =
        Signature.getInstance("SHA256withECDSA").run {
            initSign(privat)
            update(nachricht)
            sign()
        }
}

/** Sitzung, wie sie nach Anmelden oder Erneuern auf der Platte liegt. */
@Serializable
data class GespeicherteSitzung(
    val kontoId: String,
    val geraetId: String,
    val token: String,
    val laeuftAbUnix: Long,
)

/** Lokal abgelegte Schlüssel eines Geräts (Klartext, nur Test-Client, E-B2-9). */
@Serializable
private data class GespeicherteSchluessel(val kontoId: String, val schluesselbund: String)

/**
 * Legt je Gerätename Dateien in [ordner] ab (standardmäßig
 * %APPDATA%\avlreminder\testclient, also außerhalb des Repos):
 *
 *   geraet-<name>.pem         Geräteschlüssel (privat PKCS #8, öffentlich PKIX)
 *   sitzung-<name>.json       zuletzt erhaltene Sitzung
 *   schluessel-<name>.json    Personen- und Tresorschlüssel
 *
 * Mehrere Namen simulieren mehrere Geräte.
 */
class Ablage(private val ordner: File) {

    /** Lädt den Geräteschlüssel oder legt ihn beim ersten Mal an. */
    fun geraet(name: String): Geraet {
        val datei = datei("geraet", name, "pem")
        if (!datei.exists()) {
            val paar = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }.generateKeyPair()
            // Beide Teile in eine Datei: Java kann den öffentlichen Teil nicht
            // aus dem privaten berechnen.
            schreibe(datei, pem("PRIVATE KEY", paar.private.encoded) + pem("PUBLIC KEY", paar.public.encoded))
            return Geraet(paar.private, paar.public.encoded)
        }
        val text = datei.readText()
        val privat = KeyFactory.getInstance("EC").generatePrivate(PKCS8EncodedKeySpec(ausPem(text, "PRIVATE KEY", datei)))
        return Geraet(privat, ausPem(text, "PUBLIC KEY", datei))
    }

    fun speichereSitzung(name: String, sitzung: GespeicherteSitzung) =
        schreibe(datei("sitzung", name, "json"), json.encodeToString(sitzung))

    fun sitzung(name: String): GespeicherteSitzung {
        val datei = datei("sitzung", name, "json")
        if (!datei.exists()) throw Bedienfehler("Keine Sitzung für Gerät \"$name\" - zuerst anmelden.")
        return json.decodeFromString(datei.readText())
    }

    /** Die Schlüssel des Geräts, oder null, wenn es noch keine hat. */
    fun schluessel(name: String, kontoId: String): Schluesselbund? {
        val datei = datei("schluessel", name, "json")
        if (!datei.exists()) return null
        val s = json.decodeFromString<GespeicherteSchluessel>(datei.readText())
        if (s.kontoId != kontoId) {
            throw Bedienfehler("Gerät \"$name\" hat Schlüssel eines anderen Kontos - anderen Gerätenamen wählen oder $datei löschen.")
        }
        return Schluesselbund.ausGeheimenBytes(Base64.getDecoder().decode(s.schluesselbund))
    }

    fun speichereSchluessel(name: String, kontoId: String, bund: Schluesselbund) =
        schreibe(
            datei("schluessel", name, "json"),
            json.encodeToString(GespeicherteSchluessel(kontoId, Base64.getEncoder().encodeToString(bund.geheimeBytes()))),
        )

    private fun datei(art: String, name: String, endung: String): File {
        // Verhindert Pfade wie "../x" als Gerätename.
        if (!Regex("[a-z0-9-]{1,32}").matches(name)) {
            throw Bedienfehler("Gerätename \"$name\" ungültig: nur a-z, 0-9 und -, höchstens 32 Zeichen")
        }
        return File(ordner, "$art-$name.$endung")
    }

    private fun schreibe(datei: File, inhalt: String) {
        ordner.mkdirs()
        // Erst in eine Nachbardatei, dann umbenennen: Bricht das Programm ab,
        // bleibt die alte Datei ganz.
        // ATOMIC_MOVE ersetzt auch unter Windows in einem Schritt (File.renameTo
        // scheitert dort, wenn das Ziel existiert).
        val tmp = File(ordner, datei.name + ".tmp")
        tmp.writeText(inhalt)
        Files.move(tmp.toPath(), datei.toPath(), StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE)
    }

    private companion object {
        val json = Json { prettyPrint = true }

        fun pem(art: String, der: ByteArray) =
            "-----BEGIN $art-----\n" + Base64.getMimeEncoder(64, "\n".toByteArray()).encodeToString(der) + "\n-----END $art-----\n"

        fun ausPem(text: String, art: String, datei: File): ByteArray {
            val block = Regex("-----BEGIN $art-----(.*?)-----END $art-----", RegexOption.DOT_MATCHES_ALL).find(text)
                ?: throw Bedienfehler("$datei: Block \"$art\" fehlt")
            return Base64.getMimeDecoder().decode(block.groupValues[1].trim())
        }
    }
}

/** Fehler, den der Mensch beheben kann; wird ohne Stacktrace ausgegeben. */
class Bedienfehler(text: String) : Exception(text)
