package com.avlreminder.testclient

import com.sun.net.httpserver.HttpServer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.FormBody
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import java.awt.Desktop
import java.io.File
import java.net.InetSocketAddress
import java.net.URI
import java.net.URLDecoder
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit

// Anmeldung bei Google wie in einer Desktop-App ("Loopback-Flow"):
//  1. Das Programm lauscht auf 127.0.0.1 an einem freien Port.
//  2. Der Browser öffnet die Google-Anmeldung; danach leitet Google mit einem
//     Code auf 127.0.0.1 zurück.
//  3. Das Programm tauscht den Code gegen Tokens und nimmt das ID-Token.
//
// PKCE bindet den Code an dieses Programm; state schützt vor fremden
// Rückleitungen; nonce bindet das ID-Token an den Geräteschlüssel.

/** Client-ID und -Secret des Desktop-Clients. Das Secret ist bei Desktop-Clients laut Google nicht geheim; ausgegeben wird es trotzdem nie. */
class GoogleClient(val id: String, val secret: String) {
    override fun toString() = "GoogleClient($id)"

    companion object {
        /** Sucht die von Google heruntergeladene client_secret_*.json in [ordner] (außerhalb des Repos). */
        fun aus(ordner: File): GoogleClient {
            val treffer = ordner.listFiles { f -> f.name.startsWith("client_secret_") && f.name.endsWith(".json") }.orEmpty()
            val datei = when (treffer.size) {
                0 -> throw Bedienfehler("Keine client_secret_*.json in $ordner (siehe docs/gcp-einrichtung.md)")
                1 -> treffer[0]
                else -> throw Bedienfehler("Mehrere client_secret_*.json in $ordner - bitte nur eine")
            }
            val installed = Json.parseToJsonElement(datei.readText()).jsonObject["installed"] as? JsonObject
                ?: throw Bedienfehler("$datei: Abschnitt \"installed\" fehlt - ist der Client vom Typ Desktop-App?")
            return GoogleClient(
                installed["client_id"]?.jsonPrimitive?.content ?: throw Bedienfehler("$datei: client_id fehlt"),
                installed["client_secret"]?.jsonPrimitive?.content ?: throw Bedienfehler("$datei: client_secret fehlt"),
            )
        }
    }
}

/** Was eine Rückleitung von Google bedeutet. */
sealed interface Rueckleitung {
    /** Nicht unsere (state passt nicht): ignorieren und weiter warten. */
    data object Fremd : Rueckleitung

    data class Fehler(val grund: String) : Rueckleitung

    data class Code(val code: String) : Rueckleitung
}

/**
 * Wertet die Parameter einer Rückleitung aus.
 *
 * KRITISCH: Nur eine Rückleitung mit unserem state zählt. Sonst könnte eine
 * fremde Webseite dem Programm einen Code unterschieben.
 */
fun werteAus(parameter: Map<String, String>, state: String): Rueckleitung =
    when {
        !MessageDigest.isEqual(parameter["state"].orEmpty().toByteArray(), state.toByteArray()) -> Rueckleitung.Fremd
        parameter["error"] != null -> Rueckleitung.Fehler("Google meldet: ${parameter["error"]}")
        parameter["code"].isNullOrEmpty() -> Rueckleitung.Fehler("Rückleitung ohne Code")
        else -> Rueckleitung.Code(parameter.getValue("code"))
    }

/** Führt den Loopback-Flow aus und liefert das ID-Token mit [nonce]. */
fun googleIdToken(client: GoogleClient, nonce: String, http: OkHttpClient): String {
    val state = zufall()
    val verifier = zufall() + zufall() // 43+ Zeichen, wie PKCE verlangt
    val challenge = base64Url(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray()))

    val ergebnis = ArrayBlockingQueue<Rueckleitung>(1)
    val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0) // Port 0: das System wählt
    server.createContext("/") { austausch ->
        val r = if (austausch.requestURI.path == "/") werteAus(parameter(austausch.requestURI.rawQuery), state) else Rueckleitung.Fremd
        val (status, text) = when (r) {
            is Rueckleitung.Code -> 200 to "Anmeldung abgeschlossen. Das Fenster kann geschlossen werden."
            is Rueckleitung.Fehler -> 400 to "Anmeldung fehlgeschlagen. Das Fenster kann geschlossen werden."
            Rueckleitung.Fremd -> 400 to "Unbekannte Anfrage."
        }
        val bytes = text.toByteArray()
        austausch.sendResponseHeaders(status, bytes.size.toLong())
        austausch.responseBody.use { it.write(bytes) }
        if (r != Rueckleitung.Fremd) ergebnis.offer(r) // nur das erste Ergebnis zählt
    }
    server.start()
    try {
        val umleitung = "http://127.0.0.1:${server.address.port}/"
        val adresse = "https://accounts.google.com/o/oauth2/v2/auth".toHttpUrl().newBuilder()
            .addQueryParameter("client_id", client.id)
            .addQueryParameter("redirect_uri", umleitung)
            .addQueryParameter("response_type", "code")
            .addQueryParameter("scope", "openid") // nur die Konto-ID, keine E-Mail, kein Name
            .addQueryParameter("state", state)
            .addQueryParameter("nonce", nonce)
            .addQueryParameter("code_challenge", challenge)
            .addQueryParameter("code_challenge_method", "S256")
            .addQueryParameter("prompt", "select_account")
            .build().toString()

        println("Anmeldung im Browser. Falls er sich nicht öffnet, diese Adresse aufrufen:")
        println(adresse)
        runCatching { Desktop.getDesktop().browse(URI(adresse)) }
            .onFailure { println("Browser ließ sich nicht öffnen: ${it.message}") }

        val r = ergebnis.poll(5, TimeUnit.MINUTES) ?: throw Bedienfehler("Keine Rückleitung von Google innerhalb von 5 Minuten")
        val code = when (r) {
            is Rueckleitung.Code -> r.code
            is Rueckleitung.Fehler -> throw Bedienfehler(r.grund)
            Rueckleitung.Fremd -> error("kommt nicht in die Warteschlange")
        }
        return codeEintauschen(client, code, verifier, umleitung, http)
    } finally {
        server.stop(0)
    }
}

private fun codeEintauschen(client: GoogleClient, code: String, verifier: String, umleitung: String, http: OkHttpClient): String {
    val anfrage = Request.Builder()
        .url("https://oauth2.googleapis.com/token")
        .post(
            FormBody.Builder()
                .add("code", code)
                .add("client_id", client.id)
                .add("client_secret", client.secret)
                .add("redirect_uri", umleitung)
                .add("grant_type", "authorization_code")
                .add("code_verifier", verifier)
                .build(),
        )
        .build()
    http.newCall(anfrage).execute().use { antwort ->
        val text = antwort.body.string()
        // Bei Fehlern nur den Status zeigen: Die Antwort könnte Tokens enthalten.
        if (!antwort.isSuccessful) throw Bedienfehler("Code eintauschen: HTTP ${antwort.code}")
        return Json.parseToJsonElement(text).jsonObject["id_token"]?.jsonPrimitive?.content
            ?: throw Bedienfehler("Google hat kein ID-Token geliefert")
    }
}

private fun parameter(query: String?): Map<String, String> =
    query.orEmpty().split("&").filter { "=" in it }.associate {
        val (k, v) = it.split("=", limit = 2)
        URLDecoder.decode(k, Charsets.UTF_8) to URLDecoder.decode(v, Charsets.UTF_8)
    }

private fun zufall(): String = base64Url(ByteArray(32).also { SecureRandom().nextBytes(it) })

private fun base64Url(bytes: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
