# Entwurf Etappe B2b: Daten-Backup für eine Person, Vorbereitung der App

> Stand: 2026-11-02. Entscheidungen von Kevin **bestätigt** am 2026-11-02 (auch in
> [anforderungen.md](anforderungen.md), Kap. 11). Grundlage: Anforderungen
> Kap. 3.3 (S5: eigene Geräte und Backup), 3.6 (Fotos und Audio später), 4.1
> (Tresorschlüssel), Review-Lauf 4 (A3) und die bekannten Grenzen aus
> [b2-entwurf.md](b2-entwurf.md).

## Ziel
Das Backend wird früh als **verschlüsseltes Backup** in die App eingebunden, lange
vor dem Play-Store-Release. So gibt es echte Tests der Mechanismen aus B1 und B2
(Anmeldung, Geräteschlüssel, Schlüssel-Backup) auf echten Geräten.

Am Ende von B2b:
- lädt der Test-Client eine beliebige Datei als **Daten-Backup** verschlüsselt hoch
  und holt sie auf einem anderen Gerät zurück,
- hält der Server die letzten Fassungen in einem **Cloud-Storage-Bucket in der EU**,
  ohne sie lesen zu können,
- sind die Formate maßgeblich beschrieben und mit festen Testvektoren gesichert
  (A3), damit die App sie für immer lesen kann,
- ist `client/` bereit für die App: Verbindungsmodul ohne CLI, Gradle-Prüfsummen.

Die Einbindung in die App selbst (Room-Datenbank sichern, Keystore, Anmeldung über
den Credential Manager) bekommt einen eigenen kurzen Entwurf im App-Repo. Sie
beginnt erst, wenn B2b fertig ist.

Weiter **nicht** in B2b: Synchronisation zwischen Geräten oder Personen (B3),
Fotos und Audio (3.6).

## Wichtig: kein verlässliches Backup
Das Backend gilt „Kosten vor Verfügbarkeit“: Der Kill-Switch kann den Dienst sperren
oder das Projekt vom Rechnungskonto trennen. Deshalb gilt für die App:
- Sie funktioniert ohne Backend vollständig. Das Backup läuft im Hintergrund und
  darf scheitern.
- Das lokale Backup (`VACUUM INTO` vor Migrationen) bleibt. Ein Export als Datei
  bleibt die eigentliche Sicherung, bis das Cloud-Backup eine Weile ohne Probleme
  läuft.
- Die App zeigt, wann die letzte Sicherung erfolgreich war.

## Entscheidungen (am 2026-11-02 bestätigt)

| # | Frage | Empfehlung | Warum |
|---|---|---|---|
| E-B2b-1 | Was wird gesichert? | **Eine undurchsichtige Nutzlast**: Für `protokoll` und Server sind es nur Bytes. Die App legt dort ihre Room-Datenbank ab (Kopie per `VACUUM INTO`, gzip-komprimiert). Fotos und Audio nicht (3.6). | So bleibt `protokoll` ohne Android und ohne SQLite. Die App braucht kein zweites Datenformat: Beim Wiederherstellen laufen ihre Room-Migrationen wie bei einem Update. Ein eigenes, app-unabhängiges Format kommt erst mit der Synchronisation (B3), wo es ohnehin nötig ist. Verweise auf Fotos zeigen nach dem Wiederherstellen ins Leere; die App muss damit umgehen. |
| E-B2b-2 | Format der Sicherung | Klartext = `Datensicherung` (proto, `inhalt/v1`): **Kopf** (Formatversion, Schema-Version der App, Zeitpunkt, Geräte-ID, fortlaufender Zähler) und Nutzlast. Verschlüsselt mit dem **Tresorschlüssel** (XChaCha20-Poly1305), zugehörige Daten `"avlreminder/datensicherung/v1\n" + konto_id`. | Tresorschlüssel, weil es private Daten sind (4.1). Er liegt schon im Schlüssel-Backup; ein neues Gerät hat ihn nach der Einrichtung aus B2. Der Kopf steht **im** Chiffrat; so kann der Server ihn weder lesen noch fälschen. |
| E-B2b-3 | Schutz gegen ältere Fassung vom Server | Das Gerät merkt sich den höchsten Zähler, den es gesichert oder gesehen hat. Liefert der Server einen kleineren, **warnt** die App und stellt nicht still wieder her. | Behebt für Daten die bekannte Grenze aus B2 („älteres Backup vom Server“): Ein bösartiger Server kann eine alte, gültige Fassung ausliefern; der Zähler im Chiffrat verrät das. Ein ganz neues Gerät kennt noch keinen Zähler; dort zeigt die App den Zeitpunkt der Sicherung zur Kontrolle. |
| E-B2b-4 | Wo liegt die Sicherung? | **Cloud Storage**, ein Bucket in `var.region`, ein Objekt je Konto (`sicherung/<konto_id>`). Objekt-Versionierung an, Lebenszyklus: ältere Fassungen **7 Tage** aufbewahren, höchstens 100 (geändert am 2026-11-03, vorher 3 Fassungen und 30 Tage). Nicht öffentlich (`public_access_prevention`), einheitlicher Zugriff; von den Diensten hat nur das Dienstkonto des Servers Zugriff (Projekt-Owner über ihre Projektrollen). | Firestore erlaubt höchstens 1 MiB je Dokument, eine Datenbank kann größer werden. Speicher kostet ca. 2 Cent je GB und Monat. Die älteren Fassungen schützen vor dem wichtigsten Fehler: Eine fehlerhafte App lädt kaputte Daten hoch und überschreibt damit das einzige Backup. Nach Alter statt Anzahl, weil die App bis zu alle 15 Minuten sichert: 3 Fassungen reichten nur etwa eine Stunde zurück, ein Fehler fällt aber oft erst nach Tagen auf. 100 × 8 MiB im schlimmsten Fall kosten etwa 2 Cent im Monat je Konto. |
| E-B2b-5 | Nie ein neueres Backup überschreiben | Wie in B2: Hochladen nur mit der **erwarteten Version** (0 = noch keins). Der Server setzt das mit der Vorbedingung `ifGenerationMatch` von Cloud Storage um. Die Generation reicht er als Version durch. | Cloud Storage prüft das selbst, ohne Transaktion in Firestore. Gleiches Verhalten wie beim Schlüssel-Backup: `failed_precondition`, neu holen, nicht überschreiben. |
| E-B2b-6 | Zwei eigene Geräte sichern | Ohne Synchronisation gibt es **ein sicherndes Gerät**. Die App sichert automatisch nur, wenn die letzte Fassung von ihr selbst stammt (Geräte-ID im Kopf) oder es noch keine gibt. Sonst fragt sie einmal: „Dieses Gerät übernimmt die Sicherung?“ | Sonst würden sich zwei Geräte mit verschiedenen Daten gegenseitig überschreiben, und jedes Mal ginge der Stand des anderen verloren. Das Zusammenführen ist Aufgabe von B3. |
| E-B2b-7 | Größe und Übertragung | Höchstens **8 MiB** Chiffrat je Sicherung, in **einer Anfrage** über den Server (Connect, eigenes Limit nur für diesen Dienst). Keine signierten URLs. | Der Server prüft Sitzung und Größe an einer Stelle. 8 MiB reichen für eine Datenbank ohne Fotos lange. Signierte URLs sparen nur Rechenzeit, die bei unserer Menge nichts kostet, machen aber Größenprüfung und Vorbedingung schwieriger. |
| E-B2b-8 | Schnittstelle | Neuer Dienst `DatensicherungDienst` mit `Speichern(chiffrat, erwartete_version)`, `Holen()`, `Loeschen()`. `Holen` liefert die neueste Fassung; ältere Fassungen holt **nur ein späterer Aufruf**, wenn er gebraucht wird. | Löschen gehört von Anfang an dazu (DSGVO, Art. 17), auch vor „Konto löschen“ in B4. Eigener Dienst und eigene Speicher-Schnittstelle statt einer Erweiterung von `SchluesselDienst`: Das ist der erste Schritt von A5. |
| E-B2b-9 | Formate festschreiben (A3, aus B3 vorgezogen) | `docs/formate.md` beschreibt **maßgeblich** Schlüssel-Backup und Datensicherung: Felder, Verfahren, zugehörige Daten, Formatversion. Dazu **feste Testvektoren**: einmal erzeugte Chiffrate mit Wegwerf-Schlüsseln; Tests prüfen, dass sie sich weiter entschlüsseln lassen. | Sobald echte Daten im Format v1 liegen, muss jede künftige App-Version sie lesen können. Ein Test, der nur verschlüsselt und gleich wieder entschlüsselt, merkt eine Formatänderung nicht; ein festes Chiffrat schon. |
| E-B2b-10 | Vorbereitung für die App | (a) **Gradle-Prüfsummen** (`verification-metadata.xml`) in `client/`. (b) Neues Modul **`verbindung`**: Connect-Clients, Sitzung, Adapter für `BackupAblage` und die Datensicherung, ohne CLI und ohne Dateiablage. `testclient` nutzt es. (c) Einbindung später per **Gradle Composite Build** (`includeBuild`) aus dem App-Repo. | (a) war für „vor der App“ entschieden. (b) Die App braucht dieselbe Anbindung; OkHttp und connect-kotlin laufen auf Android. (c) Beide Repos liegen nebeneinander; so braucht es keine veröffentlichten Pakete und keine Versionspflege. |
| E-B2b-11 | Wer darf das testen? | Nur Kevin und wenige Bekannte über den **internen Test-Track**, in einer eigenen App-Variante. Eine kurze Testerinformation reicht vorerst; die Datenschutzerklärung bleibt in B6. | Die Daten sind Ende-zu-Ende verschlüsselt; der Server sieht nur Chiffrat, Größe und Zeitpunkt. Konten kann zwar jedes Google-Konto anlegen, aber ohne die App-Variante nutzt das niemand. Größenlimit, ältere Fassungen begrenzt und Kill-Switch halten die Kosten bei Missbrauch klein; Play Integrity kommt in B6. |

## Abläufe

### Sichern (App oder Test-Client)
```
Gerät                                                    Server
  | Datensicherung bauen: Kopf (Zähler + 1, Zeit,
  | Geräte-ID, Schema-Version) + Nutzlast,
  | verschlüsseln mit Tresorschlüssel
  | Speichern(chiffrat, erwartet: zuletzt gesehene V.) --->|
  |                       Größe prüfen, Cloud Storage mit  |
  |                       ifGenerationMatch schreiben      |
  |<--------------------------------- neue Version --------|
  | Zähler und Version lokal merken
```
Meldet der Server `failed_precondition`, hat ein anderes Gerät gesichert. Das Gerät
holt die neueste Fassung, liest die Geräte-ID im Kopf und verfährt wie in E-B2b-6.
Lehnt der Mensch die Übernahme ab, merkt sich das Gerät den Zähler dieser Fassung als
**gesehen** (E-B2b-3): Sie ließ sich entschlüsseln, ist also echt, und eine ältere
Fassung vom Server gilt danach als veraltet.
Die App sichert nur, wenn sich die Daten seit der letzten Sicherung geändert haben,
höchstens alle 15 Minuten und nur mit Netz (WorkManager).

### Wiederherstellen
Nur auf ausdrücklichen Wunsch, nie automatisch über vorhandene lokale Daten:
1. Einrichtung wie in B2 (Wiederherstellungsschlüssel), damit ist der
   Tresorschlüssel da.
2. `Holen`, entschlüsseln, Kopf zeigen („Sicherung vom … von Gerät …“).
3. Erst nach Bestätigung die Nutzlast übergeben (App: Datenbank ersetzen, vorher
   die aktuelle lokal sichern).

Schlägt das Entschlüsseln fehl oder ist der Zähler kleiner als der gemerkte, wird
nichts ersetzt. Lehnt der Mensch in Schritt 3 ab, merkt sich das Gerät den Zähler als
gesehen (wie beim Sichern).

**Wiederherstellen heißt übernehmen (E-B2b-6).** Nach dem Wiederherstellen ist dieses
Gerät das sichernde Gerät: Es sichert ohne weitere Rückfrage weiter, auch wenn die
Fassung von einem anderen Gerät stammte. Die Bestätigung in Schritt 3 ist zugleich
die Rückfrage „Dieses Gerät übernimmt die Sicherung?“.

### Löschen
`Sicherungsablauf.loeschen` löscht die Sicherung mit allen älteren Fassungen. Das
Gerät behält seinen Zähler (nur die Version wird 0), die nächste Sicherung zählt
also weiter. Finge sie bei 1 an, lehnten andere Geräte, die einen höheren Zähler
kennen, sie als ältere Sicherung ab.

## Server und Infrastruktur
- `infra/` (`datensicherung.tf`): Bucket (`var.region`, Versionierung, Lebenszyklus,
  ohne Soft Delete, `public_access_prevention = "enforced"`, `uniform_bucket_level_access`),
  `roles/storage.objectUser` nur für das Dienstkonto des Servers und nur auf diesem
  Bucket. Kein neuer Cloud-Run-Dienst. `tofu test` und `pruefe-regionen.sh` prüfen
  die Region mit.
- `server/internal/datensicherung` (oder `dienst`): Dienst mit eigener
  Speicher-Schnittstelle, im Arbeitsspeicher für Unit-Tests und mit Cloud Storage.
  Den Test gegen Cloud Storage lokal mit `fake-gcs-server` (Docker), falls dieser
  `ifGenerationMatch` korrekt nachbildet; sonst gegen einen eigenen Test-Bucket in
  der CI. Das klären wir im ersten Schritt.
- Logs: Konto-Pseudonym, Größe, Version, Ergebnis. Nie Inhalt.
- Kosten: bei einer Person und wenigen Testern praktisch null (Speicher im
  Megabyte-Bereich, wenige Tausend Operationen im Monat).

## Client
```
client/
  protokoll/   + Datensicherung (bauen, verschlüsseln, prüfen: Konto, Zähler),
                 Ablauf Sichern/Wiederherstellen hinter Schnittstellen (wie die
                 Einrichtung)
  verbindung/  NEU: Connect-Clients, Sitzung, Adapter (aus testclient herausgelöst)
  testclient/  CLI und Dateiablage; neue Befehle:
                 sichern <datei>, wiederherstellen <datei>, sicherung-loeschen
```
Tests wie bisher: parametrisiert, deutsche Namen, zu jeder Erlaubnis ein
Ablehnungsfall. Ablehnungsfälle unter anderem: Sicherung eines anderen Kontos, mit
falschem Tresorschlüssel, kleinerer Zähler, verändertes Chiffrat, zu groß.

## Reihenfolge
1. `docs/formate.md` und feste Testvektoren für das bestehende Schlüssel-Backup
   (A3). Gradle-Prüfsummen.
2. `Datensicherung` in `protokoll` mit Tests und Testvektoren. Ohne Server.
3. Klären: `fake-gcs-server` oder Test-Bucket. Server-Dienst mit Tests.
4. Modul `verbindung` herauslösen, Befehle im Test-Client. Lokal von Ende zu Ende.
5. Infrastruktur (Bucket) per OpenTofu, **nach Kevins Go** (kostet Geld, wenn auch
   kaum). Deploy über PR nach `main`, Test in der Cloud.
6. Review-Lauf.
7. Danach im App-Repo: eigener Entwurf für die Einbindung (Composite Build, Keysets
   mit Keystore-Schlüssel verschlüsselt, Anmeldung über den Credential Manager,
   WorkManager, Anzeige „letzte Sicherung“, eigene App-Variante).

## Vor echten Daten (Beginn der Sicherheitsphase)
Bis hierher sind alle Schlüssel Testschlüssel: Sie lagen im Klartext in `%APPDATA%`,
und ihre Wiederherstellungsschlüssel standen zum Teil in Terminal-Ausgaben, die Claude
gelesen hat. Bevor die App-Variante echte Daten sichert:
1. Schlüssel-Backup und Datensicherung des Testkontos auf dem Server löschen. Dafür
   bekommt `SchluesselDienst` in B2b einen Aufruf `BackupLoeschen` und der Test-Client
   den Befehl `schluessel-loeschen`.
2. Die Dateien in `%APPDATA%\avlreminder\testclient` löschen.
3. Die App erzeugt auf dem Gerät **neue** Schlüssel und einen neuen
   Wiederherstellungsschlüssel. Den sieht nur Kevin, auf Papier; kein Terminal, kein
   Screenshot, kein Gespräch mit Claude.
4. **Löschschutz an:** Firestore `deletion_policy = "ABANDON"` und
   `delete_protection_state`, Cloud Run `deletion_protection = true`, Bucket mit
   `force_destroy = false`. Bis dahin muss das Lernprojekt vollständig löschbar sein.
5. **Neue Geräte:** entscheiden und umsetzen, dass ein neu angemeldetes Gerät erst
   nach Freischaltung (QR oder Wiederherstellungsschlüssel) andere Geräte sperren und
   Backup oder Datensicherung löschen darf, und dass vorhandene Geräte von jedem neuen
   Gerät erfahren (Grenze in [b1-entwurf.md](b1-entwurf.md), „Gerät sperren“).

Claude kündigt diesen Schritt vorher ausdrücklich an (siehe `CLAUDE.md`,
„Geheimnisse und Sicherheitsphase“). Das Rotieren der Schlüssel selbst kommt erst in
B4; bis dahin heißt „rotieren“ hier: zurücksetzen und neu erzeugen.

## Bekannte Grenzen (bewusst)
- **Ein sicherndes Gerät.** Zwei Geräte mit verschiedenen Daten führt erst B3
  zusammen.
- **Fotos und Audio** werden nicht gesichert (3.6).
- **Ganz neues Gerät und alte Fassung.** Ohne gemerkten Zähler erkennt das Gerät
  eine vom Server zurückgehaltene neuere Fassung nicht; es sieht nur den Zeitpunkt.
- **Löschen und ein ganz neues Gerät.** Den Zähler kennt jedes Gerät nur lokal. Wird
  die Sicherung gelöscht und sichert danach zuerst ein Gerät ohne gemerkten Zähler,
  beginnt es bei 1; Geräte mit höherem Zähler melden dann „ältere Sicherung“, bis
  sie selbst wieder sichern dürfen (Sicherung erneut löschen oder auf dem neuen Gerät
  wiederherstellen und dort weitersichern). Selten, weil gelöscht nur bei Neuanfang wird.
- **Gestohlenes Gerät mit Google-Login** kann sich nach dem Sperren neu anmelden und
  die Sicherung löschen (siehe „Vor echten Daten“, Punkt 5).
- **Schlüssel-Backup nach dem Löschen.** Die Version beginnt wieder bei 1. Ein Gerät,
  das noch Version 1 erwartet, könnte so ein neues Backup überschreiben. Heute
  harmlos, weil Clients nur mit `erwartet = 0` speichern (Kommentar in
  `schluessel.proto`); ein Grabstein kommt, wenn Clients höhere Versionen nutzen.
- **Kein Zugriff, solange der Kill-Switch greift.** Die Daten bleiben im Bucket, sind
  aber nicht erreichbar.
- **Format der Nutzlast ist die Room-Datenbank.** Eine App-Version kann nur Sicherungen
  mit gleicher oder älterer Schema-Version einspielen. Eine neuere Sicherung lehnt sie
  ab und bittet um ein Update.
- **Höchstens 8 MiB je Sicherung, ein Platz je Konto.** Gemessen (synthetische DB,
  Schema 7, gzip): etwa 0,7 MiB je Jahr bei normaler, 1,7 MiB bei sehr starker Nutzung.
  Die App warnt ab etwa 75 %. Ausweg, falls das nicht reicht: Grenze bis knapp 32 MiB
  anheben (Cloud Run), darüber direkter Upload in den Bucket mit signierter Adresse,
  sehr groß ein Format v2 mit Streaming AEAD. Nichts davon bricht vorhandene Sicherungen.

## Bewusst nicht in B2b
- Synchronisation, Freigaben, Konfliktauflösung (B3)
- Ältere Fassungen in der App auswählen (sie liegen bereit; die Auswahl kommt, wenn
  sie gebraucht wird)
- Fotos und Audio (3.6), Konto löschen insgesamt (B4)
