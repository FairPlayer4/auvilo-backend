package com.avlreminder.testclient

import com.avlreminder.protokoll.GemerkteSicherung
import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.protokoll.LokaleAblage
import com.avlreminder.protokoll.LokalerStand
import com.avlreminder.protokoll.OffenesBackup
import com.avlreminder.protokoll.Schluesselbund
import com.avlreminder.protokoll.SicherungsStand
import com.avlreminder.verbindung.Geraeteschluessel
import com.avlreminder.verbindung.Sitzung
import com.avlreminder.verbindung.SitzungsAblage
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
class Geraet(private val privat: PrivateKey, val der: ByteArray) : Geraeteschluessel {
    val id: String get() = Geraetenachrichten.geraetId(der)

    override val oeffentlich: ByteArray get() = der

    /** Signatur im Format, das der Server erwartet (SHA-256, ASN.1/DER) – wie der Android Keystore. */
    override fun signiere(nachricht: ByteArray): ByteArray =
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
private data class GespeicherteSchluessel(
    val kontoId: String,
    val schluesselbund: String,
    /** Backup, das noch nicht sicher hochgeladen ist (Base64), sonst null. */
    val offenesBackup: String? = null,
    /** Der öffentliche Freischaltschlüssel dazu (Base64, E-B2b-13). */
    val offenerFreischaltschluessel: String? = null,
)

/** Was ein Gerät sich über die Datensicherung seines Kontos merkt. */
@Serializable
private data class GespeicherterStand(val kontoId: String, val zaehler: Long, val version: Long, val gesehen: Long = 0, val abgelehnt: Boolean = false)

/**
 * Legt je Gerätename Dateien in [ordner] ab (standardmäßig
 * %APPDATA%\avlreminder\testclient, also außerhalb des Repos):
 *
 *   geraet-<name>.pem         Geräteschlüssel (privat PKCS #8, öffentlich PKIX)
 *   sitzung-<name>.json       zuletzt erhaltene Sitzung
 *   schluessel-<name>.json    Personen- und Tresorschlüssel, ggf. offenes Backup
 *   stand-<name>.json         Zähler und Version der letzten Datensicherung
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

    /** Die Sitzung des Geräts für [com.avlreminder.verbindung.Anmeldung]. */
    fun sitzungsAblage(name: String): SitzungsAblage = object : SitzungsAblage {
        override suspend fun lesen(): Sitzung? {
            val datei = datei("sitzung", name, "json")
            if (!datei.exists()) return null
            val s = lies<GespeicherteSitzung>(datei)
            return Sitzung(s.kontoId, s.geraetId, s.token, s.laeuftAbUnix)
        }

        override suspend fun speichern(sitzung: Sitzung) =
            speichereSitzung(name, GespeicherteSitzung(sitzung.kontoId, sitzung.geraetId, sitzung.token, sitzung.laeuftAbUnix))
    }

    fun sitzung(name: String): GespeicherteSitzung {
        val datei = datei("sitzung", name, "json")
        if (!datei.exists()) throw Bedienfehler("Keine Sitzung für Gerät \"$name\" - zuerst anmelden.")
        return lies(datei)
    }

    /** Die Schlüssel des Geräts, oder null, wenn es noch keine hat. */
    fun schluessel(name: String, kontoId: String): LokalerStand? {
        val datei = datei("schluessel", name, "json")
        if (!datei.exists()) return null
        val s = lies<GespeicherteSchluessel>(datei)
        if (s.kontoId != kontoId) {
            throw Bedienfehler("Gerät \"$name\" hat Schlüssel eines anderen Kontos - anderen Gerätenamen wählen oder $datei löschen.")
        }
        val b64 = Base64.getDecoder()
        val offen = s.offenesBackup?.let { chiffrat ->
            // Dateien von vor E-B2b-13 haben den Schlüssel nicht; ohne ihn nimmt der Server das Backup nicht an.
            val fs = s.offenerFreischaltschluessel
                ?: throw Bedienfehler("$datei hat ein offenes Backup ohne Freischaltschlüssel (alte Version) - Datei löschen und neu einrichten.")
            OffenesBackup(b64.decode(chiffrat), b64.decode(fs))
        }
        return LokalerStand(Schluesselbund.ausGeheimenBytes(b64.decode(s.schluesselbund)), offen)
    }

    fun speichereSchluessel(name: String, kontoId: String, stand: LokalerStand) {
        val b64 = Base64.getEncoder()
        val gespeichert = GespeicherteSchluessel(
            kontoId,
            b64.encodeToString(stand.bund.geheimeBytes()),
            stand.offenesBackup?.chiffrat?.let(b64::encodeToString),
            stand.offenesBackup?.freischaltschluessel?.let(b64::encodeToString),
        )
        schreibe(datei("schluessel", name, "json"), json.encodeToString(gespeichert))
    }

    /** Die Schlüsselablage eines Geräts für ein Konto, wie die [com.avlreminder.protokoll.Einrichtung] sie braucht. */
    fun lokal(name: String, kontoId: String): LokaleAblage = object : LokaleAblage {
        override fun laden() = schluessel(name, kontoId)

        override fun speichern(stand: LokalerStand) = speichereSchluessel(name, kontoId, stand)

        override fun loeschen() {
            datei("schluessel", name, "json").delete()
        }
    }

    /** Der Sicherungsstand eines Geräts für ein Konto, wie der [com.avlreminder.protokoll.Sicherungsablauf] ihn braucht. */
    fun sicherungsStand(name: String, kontoId: String): SicherungsStand = object : SicherungsStand {
        override fun laden(): GemerkteSicherung? {
            val datei = datei("stand", name, "json")
            if (!datei.exists()) return null
            val s = lies<GespeicherterStand>(datei)
            if (s.kontoId != kontoId) {
                throw Bedienfehler("Gerät \"$name\" hat den Sicherungsstand eines anderen Kontos - anderen Gerätenamen wählen oder $datei löschen.")
            }
            return GemerkteSicherung(s.zaehler, s.version, s.gesehen, s.abgelehnt)
        }

        override fun speichern(stand: GemerkteSicherung) =
            schreibe(datei("stand", name, "json"), json.encodeToString(GespeicherterStand(kontoId, stand.zaehler, stand.version, stand.gesehen, stand.abgelehnt)))
    }

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

        /**
         * Liest eine JSON-Datei der Ablage.
         *
         * KRITISCH: Die Meldungen von kotlinx.serialization zitieren die Datei
         * („JSON input: …“), bei Sitzung und Schlüsseln also Token oder
         * Schlüssel. Deshalb ein eigener Bedienfehler ohne Ursache: Er nennt nur
         * die Datei und landet ohne Stacktrace im Terminal.
         */
        inline fun <reified T> lies(datei: File): T =
            try {
                json.decodeFromString<T>(datei.readText())
            } catch (_: IllegalArgumentException) { // auch SerializationException
                throw Bedienfehler("$datei ist unlesbar (kaputt oder von einer anderen Version) - löschen und neu anmelden.")
            }

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
