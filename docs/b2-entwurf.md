# Entwurf Etappe B2: Kotlin-Protokollmodul, Schlüssel, Test-Client

> Stand: 2026-11-01. Entscheidungen von Kevin **bestätigt** am 2026-11-01 (auch in
> [anforderungen.md](anforderungen.md), Kap. 11). Grundlage: Anforderungen
> Kap. 3.1 (ID3, ID4), 4.1 (Schlüsselmodell), 5, 7 (Q5, Q6) und Etappe B2 in Kap. 9.

## Ziel
Am Ende von B2 gibt es unter `client/` ein Kotlin-Projekt mit zwei Teilen:

- **`protokoll`**: reines Kotlin ohne Android und ohne Netz. Es erzeugt das
  Schlüsselmodell aus Kap. 4.1, verpackt und entpackt Schlüssel und Daten und
  signiert und prüft. Dieses Modul soll später unverändert in die App wandern.
- **`testclient`**: Kommandozeile auf der JVM. Sie meldet sich wie `testanmeldung`
  mit Google an und kann dazu:
  - beim ersten Gerät eines Kontos Personen- und Tresorschlüssel erzeugen,
  - einen **Wiederherstellungsschlüssel** zum Ausdrucken ausgeben,
  - das verschlüsselte Schlüssel-Backup auf den Server laden,
  - auf einem zweiten Gerät die Schlüssel mit dem Wiederherstellungsschlüssel
    zurückholen.

Der Server bekommt dafür nur **eine Ablage für das Backup**: ein Chiffrat je Konto,
das er nicht lesen kann. Noch **keine** Kontakte, Freigaben oder Operationen (B3).

## Entscheidungen (am 2026-11-01 bestätigt)

| # | Frage | Empfehlung | Warum |
|---|---|---|---|
| E-B2-1 | Krypto-Bibliothek | **Google Tink** (Java, läuft auf JVM und Android) | Keine nativen Bibliotheken (libsodium bräuchte JNI/Lazysodium). Fertige, geprüfte Bausteine für alles in 4.1. Ein **Keyset** verwaltet mehrere Schlüsselversionen, also genau das, was Freigabe-Schlüssel mit Versionen brauchen. Kap. 4.1 nennt Tink schon als Empfehlung. |
| E-B2-2 | Verfahren | Personenschlüssel = zwei Schlüsselpaare: **HPKE mit X25519** (verpacken für mich) und **Ed25519** (signieren). Tresor- und Freigabe-Schlüssel: **XChaCha20-Poly1305**. | Wie in 4.1. Zwei Paare statt eines, weil Tink Verschlüsseln und Signieren bewusst trennt. XChaCha20 hat eine 192-Bit-Nonce: Zufällige Nonces sind damit auch bei sehr vielen Nachrichten sicher. Der Server prüft diese Signaturen nicht, er versteht keine Inhalte. |
| E-B2-3 | Form des Wiederherstellungsschlüssels | **256 Bit Zufall**, gedruckt als **Base32 in Vierergruppen**: 52 Zeichen Schlüssel und 4 Prüfzeichen, also 14 Gruppen (z. B. `K7QM-…`) | Ein echter Schlüssel statt eines Passworts: Es braucht keine Schlüsselableitung, und Raten ist aussichtslos. Base32 hat keine verwechselbaren Zeichen (0/O, 1/I/l). Das Prüfzeichen findet Tippfehler, bevor man entschlüsselt. Eine Wortliste (24 Wörter) wäre angenehmer zu tippen, braucht aber eine deutsche Liste. Das lohnt sich erst mit der App. |
| E-B2-4 | Was liegt auf dem Server? | Ein **Backup je Konto**: Personen- und Tresor-Keyset, mit dem Wiederherstellungsschlüssel verschlüsselt (AEAD), die Konto-ID als „zugehörige Daten“. Dazu eine **Versionsnummer**. | Der Server sieht nur Chiffrat und Größe. Die Konto-ID in den zugehörigen Daten verhindert, dass ein Angreifer mit Zugriff auf die Datenbank einem Konto das Backup eines anderen unterschiebt. Mit der Versionsnummer überschreibt ein Gerät nie unbemerkt ein neueres Backup (Schreiben nur, wenn die erwartete Version stimmt). |
| E-B2-5 | Wann entstehen die Schlüssel? | Meldet `Anmelden` **`neues_konto = true`**, erzeugt dieses Gerät die Schlüssel und lädt das Backup hoch. Jedes weitere Gerät holt das Backup und fragt nach dem Wiederherstellungsschlüssel. | Der Server stellt in B1 schon sicher, dass genau ein Gerät `neues_konto` bekommt (Vertragstest). Bricht das erste Gerät vor dem Hochladen ab, gibt es kein Backup. Dann erzeugt das nächste Gerät die Schlüssel. |
| E-B2-6 | Neues Gerät per QR von einem vorhandenen Gerät (ID3) | **Nicht in B2**, sondern in B3 zusammen mit den QR-Kontakten | Beides braucht dieselbe Technik: einen Schlüsselaustausch über einen Code, den man abtippen oder scannen kann. B2 bleibt beim Wiederherstellungsschlüssel, wie es Kap. 9 vorsieht. |
| E-B2-7 | Code aus der proto-Datei für Kotlin | **Im Gradle-Build erzeugen** (Protobuf-Gradle-Plugin, `protobuf-javalite` und `connect-kotlin`), **nicht** einchecken. buf bleibt für Lint und den Breaking-Check. | Der erzeugte Code kann so nicht veralten, und die CI braucht keinen Diff-Check. „Lite“ ist die Variante für Android, und die App kann denselben Weg nehmen. Für Go bleibt der eingecheckte Code, wie er ist. |
| E-B2-8 | Gleiche Bytes in Kotlin und Go | **Gemeinsame Testvektoren** (`proto/testvektoren/*.json`): einmal erzeugte Nachrichten mit Signatur eines Wegwerf-Schlüssels. Der Kotlin-Test baut dieselben Bytes und prüft die Signatur, der Go-Test ebenso. Ob der Test-Client selbst passend signiert, zeigt der Test von Hand. | So bricht ein Unterschied bei Präfix, Zeilenumbruch oder Kodierung zwischen den Sprachen sofort einen Test, ohne Google und ohne Netz. Den ganzen Ablauf mit echter Google-Anmeldung testen wir von Hand (lokal und in der Cloud, wie in B1). |
| E-B2-9 | Wo liegt der Geräteschlüssel im Test-Client? | **P-256 in einer Datei** je Gerätename unter `%APPDATA%\avlreminder\testclient\`, wie bei `testanmeldung` | In der App kommt er später in den Android Keystore (E-B1-2). Personen- und Tresor-Keyset liegen dort ebenfalls, im Klartext, nur für den Test-Client. In der App kommen sie später mit einem Keystore-Schlüssel verschlüsselt. |
| E-B2-10 | Tests in Kotlin | **JUnit 6** (wie die App), parametrisierte Tests mit deutschen Namen, zu jeder Erlaubnis ein Ablehnungsfall. `// KRITISCH:` wie in Go. Compiler-Warnungen gelten als Fehler. | Gleiche Lesart wie die Go-Tests. Property-Based-Tests (Q5) kommen erst mit der Konfliktauflösung (B3). |

## Abläufe

### Erstes Gerät eines Kontos
```
Test-Client                                              Server
  | Anmelden (wie B1) ------------------------------------>|
  |<------------------------------- neues_konto = true ----|
  | Personenschlüssel (X25519 + Ed25519) und
  | Tresorschlüssel erzeugen, Wiederherstellungsschlüssel
  | erzeugen und anzeigen ("bitte ausdrucken"),
  | Backup = AEAD(Wiederherstellungsschl., Keysets,
  |               zugehörig: "avlreminder/backup/v1\n" + konto_id)
  | BackupSpeichern(chiffrat, erwartet: Version 0) ------->|
  |<------------------------------------ Version 1 --------|
```

### Weiteres Gerät
```
  | Anmelden (wie B1) ------------------------------------>|
  |<------------------------------ neues_konto = false ----|
  | BackupHolen ------------------------------------------>|
  |<-------------------------------- chiffrat, Version ---|
  | Wiederherstellungsschlüssel eintippen, Prüfzeichen
  | prüfen, entschlüsseln → Personen- und Tresorschlüssel
```
Bei einem falschen Schlüssel oder veränderten Daten meldet der Client einen Fehler und
speichert nichts. Ein Backup eines anderen Kontos scheitert an den zugehörigen Daten.

### Was das Modul `protokoll` sonst kann (für B3 vorbereitet, getestet, noch nicht genutzt)
- **Freigabe-Schlüssel verpacken**: HPKE für den öffentlichen X25519-Schlüssel einer
  Person, mit Zweck-Präfix `avlreminder/umschlag/v1\n`. Nur diese Person kann ihn
  entpacken.
- **Signieren und prüfen** mit Ed25519, ebenfalls mit Zweck-Präfix, damit eine
  Signatur für das eine nie für das andere passt.
- **Daten verschlüsseln** mit Tresor- oder Freigabe-Schlüssel. Das Keyset wählt die
  Version, und alte Versionen bleiben entschlüsselbar.
- **Fingerabdruck** eines Personenschlüssels für den späteren Vergleich beim QR-Kontakt.

## Schnittstelle (neu, `proto/avlreminder/v1/schluessel.proto`)

```protobuf
// Ablage für das verschlüsselte Schlüssel-Backup. Braucht eine Sitzung.
// Der Server kann den Inhalt nicht lesen und prüft nur Größe und Version.
service SchluesselDienst {
  rpc BackupSpeichern(BackupSpeichernRequest) returns (BackupSpeichernResponse);
  rpc BackupHolen(BackupHolenRequest) returns (BackupHolenResponse);
}

message BackupSpeichernRequest {
  bytes chiffrat = 1;            // höchstens 16 KiB
  int64 erwartete_version = 2;   // 0 = es gibt noch keins
}
message BackupSpeichernResponse { int64 version = 1; }

message BackupHolenRequest {}
message BackupHolenResponse {
  bytes chiffrat = 1;
  int64 version = 2;             // not_found, wenn es keins gibt
}
```
Fehlercodes:
- `failed_precondition`: Die erwartete Version passt nicht; ein anderes Gerät war
  schneller.
- `invalid_argument`: Das Backup ist leer oder zu groß.
- `not_found`: Für das Konto gibt es kein Backup.
- Dazu die Codes der Sitzung wie bisher.

## Daten in Firestore (neu)

| Sammlung | Dokument-ID | Felder |
|---|---|---|
| `backups` | Konto-ID | `chiffrat`, `version`, `geaendert_am` |

Gelesen und geschrieben wird es nur in einer Transaktion: Version prüfen, dann
schreiben (`// KRITISCH:`, Vertragstest mit Emulator, auch gleichzeitig). Konto
löschen (B4) löscht das Backup mit. Die Kosten sind vernachlässigbar: ein Dokument je
Konto, nur bei Einrichtung und neuem Gerät gelesen.

## Code

**Server** (`server/internal/dienst`, `speicher`):
- neuer Dienst und die Speicher-Methoden `BackupSpeichern`/`BackupHolen` mit
  Version, in beiden Speichern (Arbeitsspeicher und Firestore),
- Tests wie bisher.

**Client** (`client/`, eigenes Gradle-Projekt, Kotlin/JVM 21, Gradle-Wrapper wie die
App):
```
client/
  settings.gradle.kts, gradle/libs.versions.toml (Versionen an einer Stelle)
  protokoll/   Schlüsselmodell, Wiederherstellungsschlüssel, Backup, Umschlag,
               Signatur – nur Tink und Kotlin-Standardbibliothek
  testclient/  CLI: Google-Login (Browser, PKCE, nonce), Geräteschlüssel,
               Connect-Aufrufe (connect-kotlin + OkHttp), Befehle
```
Befehle des Test-Clients, angelehnt an `testanmeldung`:
- `anmelden`: erstes Gerät richtet die Schlüssel ein, weitere holen das Backup.
- `erneuern`, `geraete`, `sperren <id>`
- `schluessel`: zeigt den Fingerabdruck des Personenschlüssels.

Hat der Kotlin-Client denselben Umfang, kann `testanmeldung` (Go) weg.

## CI und Werkzeuge
- Neuer Job **„Kotlin – client“**:
  - prüft den Gradle-Wrapper,
  - führt `./gradlew check` aus, mit JDK 21.
- Go-Job: liest dieselben Testvektoren.
- Dependabot: Ökosystem `gradle` für `client/`.
- `entwicklungsumgebung.md`: Gradle kommt über den Wrapper, nichts installieren.

## Reihenfolge
1. `client/`-Gerüst, `protokoll` mit Tests: Schlüssel, Wiederherstellungsschlüssel,
   Backup, Umschlag, Signatur. Testvektoren und der Go-Gegentest. Ohne Server.
2. Server: `SchluesselDienst` mit Tests gegen den Emulator.
3. Test-Client: Anmelden (Google), Backup einrichten und holen. Lokal gegen den
   Emulator von Ende zu Ende, dabei meldest du dich im Browser an.
4. CI (Kotlin-Job, Dependabot), Doku.
5. Deploy über PR nach `main`, dann Test in der Cloud. Infrastruktur ändert sich
   nicht: Firestore gibt es schon, und die neue Sammlung braucht keinen Index.
6. Review-Lauf 4.

## Bekannte Grenzen (bewusst)
- **Gesperrtes Gerät behält die Schlüssel.** Ein gestohlenes Gerät hat Personen- und
  Tresorschlüssel. Sperren schließt es vom Server aus (B1), aber die Schlüssel zu
  wechseln gehört zu „Person entfernen und faule Rotation“ in B4.
- **Wiederherstellungsschlüssel verloren und kein Gerät mehr**: Private Daten sind
  weg. Das ist so gewollt (ID4). Einen neuen Wiederherstellungsschlüssel erzeugen,
  solange noch ein Gerät da ist, kommt später, mit der App.
- **Älteres Backup vom Server.** Die Version schützt nur gegen gleichzeitige ehrliche
  Geräte; sie steht nicht im verschlüsselten Inhalt. Ein bösartiger Server könnte ein
  älteres, gültiges Backup ausliefern. In B2 gibt es nur eine Fassung; sobald sich
  das Backup ändert (Tresor-Rotation in B4, neuer Wiederherstellungsschlüssel), muss
  die Version in den Inhalt und beim Einlesen geprüft werden.
- Der Test-Client speichert die Keysets unverschlüsselt in `%APPDATA%`. Das reicht
  zum Testen und gilt nicht für die App.

## Bewusst nicht in B2
- Kontakte, öffentliche Personenschlüssel auf dem Server, Freigaben, Operationen,
  Konfliktauflösung (alles B3)
- Neues Gerät per QR (B3, E-B2-6)
- Android-Code und die Anbindung an die App
