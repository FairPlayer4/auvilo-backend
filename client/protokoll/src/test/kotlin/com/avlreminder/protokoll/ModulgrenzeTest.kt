package com.avlreminder.protokoll

import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

/**
 * KRITISCH: Jede erzeugte Protobuf-Klasse gibt es genau einmal. protokoll bringt
 * nur die Formate (inhalt) mit, die Schnittstelle (v1) kommt aus verbindung.
 * Erzeugten beide Module alles, hätte die App jede Klasse doppelt, und D8 bräche
 * den Android-Build ab (auf der JVM fällt das nicht auf).
 */
class ModulgrenzeTest {
    @Test
    fun `Die Schnittstelle zum Server ist nicht in protokoll`() {
        assertThrows(ClassNotFoundException::class.java) {
            Class.forName("com.avlreminder.schnittstelle.v1.AnmeldenRequest")
        }
    }

    @Test
    fun `Die Formate sind in protokoll`() {
        Class.forName("com.avlreminder.protokoll.inhalt.v1.Backup")
    }
}
