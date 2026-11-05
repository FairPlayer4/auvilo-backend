package com.avlreminder.protokoll

/**
 * Zweck-Präfixe: Vor allem, was verschlüsselt oder signiert wird, steht der
 * Zweck. So passt ein Chiffrat oder eine Signatur für das eine nie für das
 * andere – auch wenn die Bytes dahinter zufällig gleich wären. Dasselbe Muster
 * wie beim Geräteschlüssel (proto/avlreminder/v1/anmeldung.proto).
 */
internal object Zweck {
    /** Schlüssel-Backup, verschlüsselt mit dem Wiederherstellungsschlüssel; dahinter die Konto-ID. */
    const val BACKUP = "avlreminder/backup/v1\n"

    /** Datensicherung, verschlüsselt mit dem Tresorschlüssel; dahinter die Konto-ID. */
    const val DATENSICHERUNG = "avlreminder/datensicherung/v1\n"

    /** Ein Schlüssel, verpackt für den Personenschlüssel eines anderen. */
    const val UMSCHLAG = "avlreminder/umschlag/v1\n"

    /**
     * Signatur, mit der sich ein Gerät beim Server freischaltet; dahinter die
     * Geräte-ID (geraete.proto, MitWiederherstellungFreischalten).
     */
    const val FREISCHALTEN = "avlreminder/freischalten/v1\n"

    /** HKDF-info: leitet aus dem Wiederherstellungsschlüssel den Freischaltschlüssel ab (schluessel.proto). */
    const val FREISCHALTSCHLUESSEL = "avlreminder/freischaltschluessel/v1"
}

/**
 * Wofür eine Signatur mit dem Personenschlüssel gilt. Vorläufig für B3
 * angelegt: Operationen in einer Freigabe und die Kontaktkarte beim QR-Kontakt.
 */
enum class Signaturzweck(internal val praefix: String) {
    OPERATION("avlreminder/operation/v1\n"),
    KONTAKT("avlreminder/kontakt/v1\n"),
}
