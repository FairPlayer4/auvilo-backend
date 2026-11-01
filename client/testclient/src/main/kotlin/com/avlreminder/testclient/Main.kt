// Test-Client: spielt ein Gerät gegenüber dem Server – echte Google-Anmeldung
// im Browser, Geräteschlüssel in einer Datei, Schlüssel-Backup (B2).
//
//   ./gradlew :testclient:run --args="[--server URL] [--geraet NAME] anmelden"
//
// Geräteschlüssel, Sitzungen und Schlüssel liegen in
// %APPDATA%\avlreminder\testclient, die Client-Datei von Google in
// %APPDATA%\avlreminder – nie im Repo.
package com.avlreminder.testclient

import com.avlreminder.protokoll.Eingerichtet
import com.avlreminder.protokoll.Einrichtung
import com.avlreminder.protokoll.EinrichtungAbgebrochen
import com.avlreminder.protokoll.Geraetenachrichten
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import com.avlreminder.schnittstelle.v1.GeraetSperrenRequest
import com.avlreminder.schnittstelle.v1.GeraeteAuflistenRequest
import com.avlreminder.schnittstelle.v1.Sitzung
import com.avlreminder.schnittstelle.v1.SitzungErneuernRequest
import com.google.protobuf.ByteString
import com.google.protobuf.Timestamp
import kotlinx.coroutines.runBlocking
import java.io.File
import java.time.Instant
import kotlin.system.exitProcess

private const val HILFE = """Befehle:
  anmelden          mit Google anmelden (Browser); richtet beim ersten Gerät die
                    Schlüssel ein, auf weiteren Geräten holt es das Backup
  erneuern          Sitzung mit dem Geräteschlüssel erneuern
  geraete           Geräte des Kontos auflisten
  sperren <id>      Gerät sperren (eigenes Gerät = abmelden)
  schluessel        Fingerabdruck des Personenschlüssels zeigen
Schalter:
  --server URL      Adresse des Servers (Standard http://localhost:8080)
  --geraet NAME     simuliertes Gerät (mehrere Namen = mehrere Geräte; Standard pc)
  --ordner PFAD     Ordner mit client_secret_*.json (Standard %APPDATA%\avlreminder)"""

fun main(args: Array<String>) {
    try {
        runBlocking { ausfuehren(args.toList()) }
    } catch (e: Bedienfehler) {
        System.err.println("Fehler: ${e.message}")
        exitProcess(1)
    } catch (e: Serverfehler) {
        System.err.println("Fehler: ${e.message}")
        exitProcess(1)
    } catch (e: EinrichtungAbgebrochen) {
        System.err.println("Einrichtung abgebrochen: ${e.message}")
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

    private fun speichereSitzung(kontoId: String, geraetId: String, sitzung: Sitzung) =
        ablage.speichereSitzung(name, GespeicherteSitzung(kontoId, geraetId, sitzung.token, sitzung.laeuftAb.seconds))
}
