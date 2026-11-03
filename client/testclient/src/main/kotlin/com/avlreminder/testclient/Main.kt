// Test-Client: spielt ein Gerät gegenüber dem Server – echte Google-Anmeldung
// im Browser, Geräteschlüssel in einer Datei, Schlüssel-Backup (B2),
// Datensicherung (B2b).
//
//   ./gradlew :testclient:run --args="[--server URL] [--geraet NAME] anmelden"
//
// Geräteschlüssel, Sitzungen und Schlüssel liegen in
// %APPDATA%\avlreminder\testclient, die Client-Datei von Google in
// %APPDATA%\avlreminder – nie im Repo.
package com.avlreminder.testclient

import com.avlreminder.protokoll.AeltereSicherung
import com.avlreminder.protokoll.Eingerichtet
import com.avlreminder.protokoll.Einrichtung
import com.avlreminder.protokoll.EinrichtungAbgebrochen
import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.protokoll.Gesichert
import com.avlreminder.protokoll.NeueresSchema
import com.avlreminder.protokoll.SicherungZuGross
import com.avlreminder.protokoll.Sicherungsablauf
import com.avlreminder.protokoll.Sicherungskopf
import com.avlreminder.protokoll.VersionVeraltet
import com.avlreminder.protokoll.Wiederhergestellt
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.schnittstelle.v1.GeraetSperrenRequest
import com.avlreminder.schnittstelle.v1.GeraeteAuflistenRequest
import com.avlreminder.schnittstelle.v1.Sitzung
import com.avlreminder.schnittstelle.v1.SitzungErneuernRequest
import com.avlreminder.verbindung.ServerBackupAblage
import com.avlreminder.verbindung.ServerSicherungsAblage
import com.avlreminder.verbindung.Serverfehler
import com.avlreminder.verbindung.Verbindung
import com.avlreminder.verbindung.ergebnis
import com.avlreminder.verbindung.mitSitzung
import com.google.protobuf.ByteString
import com.google.protobuf.Timestamp
import kotlinx.coroutines.runBlocking
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.time.Instant
import kotlin.system.exitProcess

private const val HILFE = """Befehle:
  anmelden          mit Google anmelden (Browser); richtet beim ersten Gerät die
                    Schlüssel ein, auf weiteren Geräten holt es das Backup
  erneuern          Sitzung mit dem Geräteschlüssel erneuern
  geraete           Geräte des Kontos auflisten
  sperren <id>      Gerät sperren (eigenes Gerät = abmelden)
  schluessel        Fingerabdruck des Personenschlüssels zeigen
  sichern <datei>   Inhalt der Datei als Datensicherung hochladen (verschlüsselt)
  wiederherstellen <datei>
                    neueste Datensicherung holen und in die Datei schreiben
  sicherung-loeschen
                    Datensicherung des Kontos mit allen Fassungen löschen
  schluessel-loeschen
                    Schlüssel-Backup des Kontos löschen (lokale Schlüssel bleiben)
Schalter:
  --server URL      Adresse des Servers (Standard http://localhost:8080)
  --geraet NAME     simuliertes Gerät (mehrere Namen = mehrere Geräte; Standard pc)
  --ordner PFAD     Ordner mit client_secret_*.json (Standard %APPDATA%\avlreminder)"""

fun main(args: Array<String>) {
    try {
        runBlocking { ausfuehren(args.toList()) }
        // OkHttp hält seine Threads nach der letzten Anfrage noch eine Minute offen;
        // ohne exitProcess würde die JVM so lange warten.
        exitProcess(0)
    } catch (e: Bedienfehler) {
        System.err.println("Fehler: ${e.message}")
        exitProcess(1)
    } catch (e: Serverfehler) {
        System.err.println("Fehler: ${e.message}")
        exitProcess(1)
    } catch (e: EinrichtungAbgebrochen) {
        System.err.println("Einrichtung abgebrochen: ${e.message}")
        exitProcess(1)
    } catch (e: Exception) {
        // Erwartete Meldungen des Sicherungsablaufs (protokoll); alles andere mit Stacktrace.
        if (e !is VersionVeraltet && e !is AeltereSicherung && e !is NeueresSchema && e !is SicherungZuGross) throw e
        System.err.println("Fehler: ${e.message}")
        exitProcess(1)
    }
}

private suspend fun ausfuehren(args: List<String>) {
    var server = "http://localhost:8080"
    var name = "pc"
    var ordner = File(System.getenv("APPDATA") ?: System.getProperty("user.home"), "avlreminder")
    val rest = mutableListOf<String>()
    val it = args.iterator()
    while (it.hasNext()) {
        when (val a = it.next()) {
            "--server" -> server = wert(it, a)
            "--geraet" -> name = wert(it, a)
            "--ordner" -> ordner = File(wert(it, a))
            "-h", "--hilfe" -> return println(HILFE)
            else -> rest += a
        }
    }
    val werkzeug = Werkzeug(Verbindung(server.trimEnd('/')), Ablage(File(ordner, "testclient")), ordner, name)
    when {
        rest == listOf("anmelden") -> werkzeug.anmelden()
        rest == listOf("erneuern") -> werkzeug.erneuern()
        rest == listOf("geraete") -> werkzeug.geraete()
        rest.size == 2 && rest[0] == "sperren" -> werkzeug.sperren(rest[1])
        rest == listOf("schluessel") -> werkzeug.schluessel()
        rest.size == 2 && rest[0] == "sichern" -> werkzeug.sichern(File(rest[1]))
        rest.size == 2 && rest[0] == "wiederherstellen" -> werkzeug.wiederherstellen(File(rest[1]))
        rest == listOf("sicherung-loeschen") -> werkzeug.sicherungLoeschen()
        rest == listOf("schluessel-loeschen") -> werkzeug.schluesselLoeschen()
        else -> {
            println(HILFE)
            throw Bedienfehler(if (rest.isEmpty()) "Befehl fehlt" else "unbekannter Befehl: ${rest.joinToString(" ")}")
        }
    }
}

private fun wert(it: Iterator<String>, schalter: String): String =
    if (it.hasNext()) it.next() else throw Bedienfehler("$schalter braucht einen Wert")

private class Werkzeug(
    private val verbindung: Verbindung,
    private val ablage: Ablage,
    private val googleOrdner: File,
    private val name: String,
) {
    suspend fun anmelden() {
        val geraet = ablage.geraet(name)
        val idToken = googleIdToken(GoogleClient.aus(googleOrdner), Geraetenachrichten.nonce(geraet.der), verbindung.http)
        val antwort = verbindung.anmelde.anmelden(
            AnmeldenRequest.newBuilder()
                .setGoogleIdToken(idToken)
                .setGeraeteschluessel(ByteString.copyFrom(geraet.der))
                .setSignatur(ByteString.copyFrom(geraet.signiere(Geraetenachrichten.anmelden(idToken))))
                .build(),
        ).ergebnis()
        speichereSitzung(antwort.kontoId, antwort.geraetId, antwort.sitzung)
        println("Angemeldet: Konto ${antwort.kontoId}, Gerät ${antwort.geraetId}" + if (antwort.neuesKonto) " (neues Konto)" else "")

        val ergebnis = Einrichtung(
            antwort.kontoId,
            ServerBackupAblage(verbindung, antwort.sitzung.token),
            ablage.lokal(name, antwort.kontoId),
            frageSchluessel = {
                print("Wiederherstellungsschlüssel (leer = Abbruch): ")
                readlnOrNull()?.takeIf { it.isNotBlank() }
            },
            zeigeWiederherstellung = { schluessel ->
                println()
                println("Neue Schlüssel erzeugt. Diesen Wiederherstellungsschlüssel aufschreiben -")
                println("ohne ihn sind die Daten weg, wenn alle Geräte verloren sind:")
                println()
                println("    ${schluessel.alsText()}")
                println()
                print("Aufgeschrieben? \"ja\" eingeben (sonst Abbruch): ")
                readlnOrNull()?.trim()?.lowercase() == "ja"
            },
            meldung = ::println,
        ).ausfuehren()
        when (ergebnis) {
            is Eingerichtet.Neu -> println("Schlüssel eingerichtet, Backup hochgeladen.")
            is Eingerichtet.AusBackup -> println("Schlüssel aus dem Backup übernommen.")
            is Eingerichtet.Vorhanden -> println("Schlüssel sind auf diesem Gerät schon eingerichtet.")
        }
        println("Fingerabdruck: ${ergebnis.bund.person.oeffentlich.fingerabdruck()}")
    }

    suspend fun erneuern() {
        val alt = ablage.sitzung(name)
        val geraet = ablage.geraet(name)
        val jetzt = Instant.now().epochSecond
        val antwort = verbindung.anmelde.sitzungErneuern(
            SitzungErneuernRequest.newBuilder()
                .setGeraetId(geraet.id)
                .setZeitpunkt(Timestamp.newBuilder().setSeconds(jetzt))
                .setSignatur(ByteString.copyFrom(geraet.signiere(Geraetenachrichten.sitzung(geraet.id, jetzt))))
                .build(),
        ).ergebnis()
        speichereSitzung(alt.kontoId, geraet.id, antwort.sitzung)
        println("Sitzung erneuert, gültig bis ${Instant.ofEpochSecond(antwort.sitzung.laeuftAb.seconds)}")
    }

    suspend fun geraete() {
        val s = ablage.sitzung(name)
        val liste = verbindung.geraete.geraeteAuflisten(GeraeteAuflistenRequest.getDefaultInstance(), mitSitzung(s.token)).ergebnis()
        for (g in liste.geraeteList) {
            val merkmale = listOfNotNull("gesperrt".takeIf { g.gesperrt }, "dieses Gerät".takeIf { g.diesesGeraet })
            println("${g.geraetId}  seit ${Instant.ofEpochSecond(g.angemeldetAm.seconds)}" + if (merkmale.isEmpty()) "" else "  (${merkmale.joinToString()})")
        }
    }

    suspend fun sperren(geraetId: String) {
        val s = ablage.sitzung(name)
        verbindung.geraete.geraetSperren(GeraetSperrenRequest.newBuilder().setGeraetId(geraetId).build(), mitSitzung(s.token)).ergebnis()
        println(if (geraetId == s.geraetId) "Dieses Gerät ist gesperrt (abgemeldet)." else "Gerät $geraetId gesperrt.")
    }

    fun schluessel() {
        val s = ablage.sitzung(name)
        val stand = ablage.schluessel(name, s.kontoId) ?: throw Bedienfehler("Auf diesem Gerät sind noch keine Schlüssel - zuerst anmelden.")
        println("Fingerabdruck: ${stand.bund.person.oeffentlich.fingerabdruck()}")
        println("Tresorschlüssel: ${stand.bund.tresor.versionen} Version(en)")
        if (stand.offenesBackup != null) println("Backup noch nicht hochgeladen - erneut anmelden.")
    }

    suspend fun sichern(datei: File) {
        if (!datei.isFile) throw Bedienfehler("$datei gibt es nicht")
        val ergebnis = sicherungsablauf { kopf -> frage("Die Sicherung stammt von ${beschreibe(kopf)}. Überschreiben?") }
            .sichern(datei.readBytes(), SCHEMA_VERSION)
        when (ergebnis) {
            is Gesichert.Ja -> println("Gesichert: Zähler ${ergebnis.zaehler}, Version ${ergebnis.version}")
            is Gesichert.AnderesGeraet -> println("Nicht gesichert: Die Sicherung von ${beschreibe(ergebnis.kopf)} bleibt.")
        }
    }

    suspend fun wiederherstellen(datei: File) {
        // Beim Wiederherstellen wird nichts hochgeladen, also auch nichts überschrieben.
        val ergebnis = sicherungsablauf { false }.wiederherstellen(
            SCHEMA_VERSION,
            bestaetigen = { kopf -> frage("Sicherung von ${beschreibe(kopf)} nach $datei schreiben?") },
            anwenden = { sicherung -> schreibeDatei(datei, sicherung.nutzlast) },
        )
        when (ergebnis) {
            is Wiederhergestellt.Ja -> println("Wiederhergestellt: Zähler ${ergebnis.kopf.zaehler} nach $datei")
            Wiederhergestellt.KeineSicherung -> println("Das Konto hat keine Datensicherung.")
            Wiederhergestellt.Abgelehnt -> println("Nichts geändert.")
        }
    }

    suspend fun sicherungLoeschen() {
        val s = ablage.sitzung(name)
        ServerSicherungsAblage(verbindung, s.token).loeschen()
        // Der Sicherungsstand bleibt: Die nächste Sicherung zählt vom gemerkten
        // Zähler weiter. Finge sie wieder bei 1 an, lehnten andere Geräte, die
        // schon einen höheren Zähler gesehen haben, sie als ältere Sicherung ab.
        println("Datensicherung gelöscht (mit allen älteren Fassungen).")
    }

    suspend fun schluesselLoeschen() {
        val s = ablage.sitzung(name)
        ServerBackupAblage(verbindung, s.token).loeschen()
        println("Schlüssel-Backup gelöscht. Die Schlüssel auf diesem Gerät bleiben.")
    }

    /** Der Ablauf für das angemeldete Gerät; braucht Sitzung und eingerichtete Schlüssel. */
    private fun sicherungsablauf(uebernehmen: (Sicherungskopf) -> Boolean): Sicherungsablauf {
        val s = ablage.sitzung(name)
        val stand = ablage.schluessel(name, s.kontoId) ?: throw Bedienfehler("Auf diesem Gerät sind noch keine Schlüssel - zuerst anmelden.")
        return Sicherungsablauf(
            s.kontoId,
            s.geraetId,
            stand.bund.tresor,
            ServerSicherungsAblage(verbindung, s.token),
            ablage.sicherungsStand(name, s.kontoId),
            uebernehmen,
        )
    }

    private fun beschreibe(kopf: Sicherungskopf) =
        "Gerät ${kopf.geraetId} (Zähler ${kopf.zaehler}, ${kopf.erstellt}, Schema ${kopf.schemaVersion})"

    private fun frage(text: String): Boolean {
        print("$text \"ja\" eingeben: ")
        return readlnOrNull()?.trim()?.lowercase() == "ja"
    }

    private fun speichereSitzung(kontoId: String, geraetId: String, sitzung: Sitzung) =
        ablage.speichereSitzung(name, GespeicherteSitzung(kontoId, geraetId, sitzung.token, sitzung.laeuftAb.seconds))
}

/** Der Test-Client sichert Dateien unverändert; ihr Format ist Schema-Version 1. */
private const val SCHEMA_VERSION = 1

/** Schreibt erst in eine Nachbardatei und benennt dann um, damit nie eine halbe Datei entsteht. */
private fun schreibeDatei(datei: File, inhalt: ByteArray) {
    val ziel = datei.absoluteFile
    val tmp = File(ziel.parentFile, ziel.name + ".tmp")
    tmp.writeBytes(inhalt)
    Files.move(tmp.toPath(), ziel.toPath(), StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE)
}
