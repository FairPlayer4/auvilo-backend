package com.avlreminder.testclient

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.params.ParameterizedTest
import org.junit.jupiter.params.provider.MethodSource

class GoogleTest {

    data class Fall(val name: String, val parameter: Map<String, String>, val erwartet: Rueckleitung) {
        override fun toString() = name
    }

    // KRITISCH: Nur unsere Rückleitung (passender state) zählt; alles andere
    // wird ignoriert und kann keinen Code unterschieben.
    @ParameterizedTest(name = "{0}")
    @MethodSource("faelle")
    fun `wertet Rückleitung aus`(fall: Fall) {
        assertEquals(fall.erwartet, werteAus(fall.parameter, STATE))
    }

    companion object {
        private const val STATE = "unser-state"

        @JvmStatic
        fun faelle() = listOf(
            Fall("nimmt Code mit passendem state an", mapOf("state" to STATE, "code" to "c1"), Rueckleitung.Code("c1")),
            Fall("ignoriert Code mit fremdem state", mapOf("state" to "fremd", "code" to "c1"), Rueckleitung.Fremd),
            Fall("ignoriert Code ohne state", mapOf("code" to "c1"), Rueckleitung.Fremd),
            Fall("ignoriert state, der nur mit unserem beginnt", mapOf("state" to STATE + "x", "code" to "c1"), Rueckleitung.Fremd),
            Fall("ignoriert Fehler mit fremdem state", mapOf("state" to "fremd", "error" to "access_denied"), Rueckleitung.Fremd),
            Fall(
                "meldet Fehler von Google mit passendem state",
                mapOf("state" to STATE, "error" to "access_denied"),
                Rueckleitung.Fehler("Google meldet: access_denied"),
            ),
            Fall("meldet Rückleitung ohne Code", mapOf("state" to STATE), Rueckleitung.Fehler("Rückleitung ohne Code")),
            Fall("meldet Rückleitung mit leerem Code", mapOf("state" to STATE, "code" to ""), Rueckleitung.Fehler("Rückleitung ohne Code")),
        )
    }
}
