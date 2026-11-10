package com.avlreminder.testclient

import com.avlreminder.protokoll.Diagnose
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.CsvSource
import java.time.Instant

class TestDiagnoseTest {

    private val zeilen = testDiagnose(Instant.parse("2026-11-04T09:12:03.120456Z")).map { Json.parseToJsonElement(it).jsonObject }

    private fun JsonObject.text(feld: String) = getValue(feld).jsonPrimitive.content

    @Test
    fun `beginnt mit dem Kopf samt Formatkennung (docs-formate)`() {
        val kopf = zeilen.first()
        assertEquals("kopf", kopf.text("art"))
        assertEquals(Diagnose.FORMAT, kopf.text("format"))
        assertEquals("knopf", kopf.text("anlass"))
    }

    @Test
    fun `Ereignis hat Zeit in UTC auf Millisekunden, Stufe, Bereich und Ereignisname`() {
        val ereignis = zeilen[1]
        assertEquals("2026-11-04T09:12:03.120Z", ereignis.text("zeit"))
        assertEquals("INFO", ereignis.text("stufe"))
        assertEquals("testclient", ereignis.text("bereich"))
        assertEquals("diagnose_test", ereignis.text("ereignis"))
    }

    // E18 (Review-Lauf 8): Instant.toString lässt Nullen am Ende weg; die Zeit muss
    // trotzdem immer genau drei Nachkommastellen haben (docs/formate.md).
    @ParameterizedTest(name = "{0} wird {1}")
    @CsvSource(
        "2026-11-04T09:12:03Z, 2026-11-04T09:12:03.000Z",
        "2026-11-04T09:12:03.100Z, 2026-11-04T09:12:03.100Z",
        "2026-11-04T09:12:03.000999Z, 2026-11-04T09:12:03.000Z",
        "2026-11-04T09:12:03.999999Z, 2026-11-04T09:12:03.999Z",
    )
    fun `Zeit hat immer drei Nachkommastellen`(zeit: String, erwartet: String) {
        val ereignis = Json.parseToJsonElement(testDiagnose(Instant.parse(zeit))[1]).jsonObject
        assertEquals(erwartet, ereignis.text("zeit"))
    }
}
