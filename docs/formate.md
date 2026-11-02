# Client-Formate (maßgeblich)

> Stand: 2026-11-02 (Etappe B2b, Review-Empfehlung A3). Diese Datei beschreibt
> die Bytes, die nur Clients lesen. Der Server speichert sie, versteht sie aber
> nicht. Code: `client/protokoll`. Proto-Dateien: `proto/avlreminder/inhalt/v1`.

## Regeln
- **Nie still ändern.** Was einmal mit echten Daten geschrieben ist, muss jede
  künftige App-Version lesen können. Ein neues Format bekommt einen neuen
  Zweck-Präfix (`…/v2\n`) und eigene Testvektoren; die alten bleiben.
- **Feste Testvektoren** in [`proto/testvektoren/inhalt.json`](../proto/testvektoren/inhalt.json),
  einmal erzeugt, nie neu erzeugt. `InhaltTestvektorenTest` prüft, dass der Code
  sie liest. Der Erzeuger verweigert das Überschreiben.
- **Zweck-Präfix in den zugehörigen Daten bzw. vor der Signatur.** So passt ein
  Chiffrat oder eine Signatur für das eine nie für das andere.
- Proto-Felder werden nur ergänzt, nie umnummeriert oder umgedeutet (wie bei
  jeder Protobuf-Schnittstelle; `buf breaking` prüft das auch für `inhalt`).

## Bausteine

| Baustein | Verfahren (Tink) | Gespeichert als |
|---|---|---|
| Wiederherstellungsschlüssel | 32 Bytes Zufall, direkt als Schlüssel für XChaCha20-Poly1305 (ohne Tink-Präfix) | nur auf Papier: Crockford-Base32, 52 Zeichen + 4 Prüfzeichen (die ersten 20 Bit von SHA-256 über die 32 Bytes), Vierergruppen mit `-`. Beim Einlesen zählen Groß-/Kleinschreibung, `-` und Leerzeichen nicht; O = 0, I und L = 1 |
| Tresor- und Freigabe-Schlüssel | XChaCha20-Poly1305, Tink-Keyset (Variante TINK: 5 Bytes Präfix mit Schlüssel-ID vor jedem Chiffrat), mehrere Versionen möglich | Tink-Keyset binär (`TinkProtoKeysetFormat`) |
| Personenschlüssel, Entpacken | HPKE (RFC 9180): DHKEM X25519 + HKDF-SHA256, ChaCha20-Poly1305, Variante TINK | Tink-Keyset binär |
| Personenschlüssel, Signieren | Ed25519, Variante TINK | Tink-Keyset binär |

Tink-Konfiguration: `TinkConfig2026`, ausdrücklich je Aufruf (keine globale Registry).

## Schlüssel-Backup (`avlreminder/backup/v1`)
- Klartext: `SchluesselBackup` ([backup.proto](../proto/avlreminder/inhalt/v1/backup.proto)):
  die drei Keysets mit privatem Teil.
- Verschlüsselt: XChaCha20-Poly1305 mit dem Wiederherstellungsschlüssel.
  Chiffrat = 24 Bytes Nonce ‖ Chiffretext ‖ 16 Bytes Tag (kein Tink-Präfix).
- Zugehörige Daten: `"avlreminder/backup/v1\n" + konto_id` (UTF-8). Ein Backup passt
  so nur zu seinem Konto.
- Auf dem Server: `SchluesselDienst` ([schluessel.proto](../proto/avlreminder/v1/schluessel.proto)),
  höchstens 16 KiB, mit Version.

## Datensicherung (`avlreminder/datensicherung/v1`)
- Klartext: `Datensicherung` ([datensicherung.proto](../proto/avlreminder/inhalt/v1/datensicherung.proto)):
  `zaehler`, `erstellt_unix`, `geraet_id`, `schema_version`, `nutzlast`.
- Verschlüsselt: mit dem Tresorschlüssel (Keyset, also mit Tink-Präfix der
  Schlüsselversion). Nach einem Schlüsselwechsel lassen sich alte Sicherungen mit
  dem erweiterten Keyset weiter öffnen.
- Zugehörige Daten: `"avlreminder/datensicherung/v1\n" + konto_id` (UTF-8).
- `zaehler` steigt mit jeder Sicherung des Kontos. Ein Gerät merkt sich den höchsten
  Wert und lehnt kleinere ab (Schutz gegen eine zurückgehaltene ältere Fassung).
- `nutzlast`: für dieses Format undurchsichtig. Die App legt dort ihre Room-Datenbank
  ab (gzip); deren Format beschreibt das App-Repo, Version in `schema_version`.
- Auf dem Server höchstens 8 MiB Chiffrat (`DatensicherungDienst`).

## Umschlag (`avlreminder/umschlag/v1`, vorbereitet für B3)
- Ein Freigabe-Schlüssel (Tink-Keyset binär, mit geheimem Teil), mit HPKE für den
  öffentlichen Personenschlüssel des Empfängers verschlüsselt.
- Kontext-Info von HPKE: `"avlreminder/umschlag/v1\n"`. Bindung an eine Freigabe
  oder einen Empfänger kommt mit B3 (Review A4).

## Signatur (vorbereitet für B3)
- Ed25519 über `präfix + daten`; Präfixe: `"avlreminder/operation/v1\n"`,
  `"avlreminder/kontakt/v1\n"`.
- Signatur mit Tink-Präfix (5 Bytes, Schlüssel-ID) vor den 64 Bytes Ed25519.

## Fingerabdruck des Personenschlüssels
SHA-256 über beide öffentlichen Keysets (zuerst Entpacken, dann Signieren), je mit
4 Bytes Länge (Big Endian) davor; die ersten 16 Bytes als Crockford-Base32 in
Vierergruppen (26 Zeichen).

## Gerätenachrichten
Was ein Gerät mit seinem Geräteschlüssel signiert, prüft der Server; das steht in
[anmeldung.proto](../proto/avlreminder/v1/anmeldung.proto) mit eigenen gemeinsamen
Testvektoren für Kotlin und Go (`proto/testvektoren/geraetenachrichten.json`).
