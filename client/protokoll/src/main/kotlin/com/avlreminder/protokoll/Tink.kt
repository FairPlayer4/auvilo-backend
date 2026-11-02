package com.avlreminder.protokoll

import com.google.crypto.tink.Configuration
import com.google.crypto.tink.config.TinkConfig2026

/**
 * Welche Verfahren Tink kennt. Jeder Tink-Aufruf dieses Moduls bekommt diese
 * Konfiguration ausdrücklich mit (statt einer globalen Registrierung) – so
 * kann nichts anderes im Prozess die Liste verändern.
 */
internal object Tink {
    val konfiguration: Configuration = TinkConfig2026.get()
}
