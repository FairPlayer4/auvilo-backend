package com.avlreminder.verbindung

import com.avlreminder.protokoll.inhalt.v1.Backup
import com.avlreminder.schnittstelle.v1.AnmeldenRequest
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * KRITISCH: Jede erzeugte Protobuf-Klasse gibt es genau einmal. Die Formate
 * (inhalt) kommen aus protokoll, nicht zusätzlich aus verbindung; sonst bräche
 * D8 den Android-Build der App ab („defined multiple times“).
 */
class ModulgrenzeTest {
    private fun ort(klasse: Class<*>) = klasse.protectionDomain.codeSource.location.toString()

    @Test
    fun `Die Formate kommen aus protokoll`() {
        val ort = ort(Backup::class.java)
        assertTrue("/protokoll/" in ort, "Backup geladen aus $ort")
    }

    @Test
    fun `Die Schnittstelle kommt aus verbindung`() {
        val ort = ort(AnmeldenRequest::class.java)
        assertTrue("/verbindung/" in ort, "AnmeldenRequest geladen aus $ort")
    }

    @Test
    fun `verbindung erzeugt keine Formate`() {
        val eigene = this::class.java.classLoader.getResources("com/avlreminder/protokoll/inhalt/v1/Backup.class").toList()
        assertFalse(eigene.any { "/verbindung/" in it.toString() }, "Backup auch in verbindung: $eigene")
    }
}
