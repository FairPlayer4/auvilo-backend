package com.avlreminder.testclient

import com.avlreminder.protokoll.Diagnose
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
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
}
