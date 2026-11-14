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
| E-B2b-4 | Wo liegt die Sicherung? | **Cloud Storage**, ein Bucket in `var.region`, ein Objekt je Konto (`sicherung/<konto_id>`). Objekt-Versionierung an, Lebenszyklus: ältere Fassungen **7 Tage** aufbewahren, höchstens 200 (geändert am 2026-11-03, vorher 3 Fassungen und 30 Tage; am 2026-11-04 von 100 auf 200). Nicht öffentlich (`public_access_prevention`), einheitlicher Zugriff; von den Diensten hat nur das Dienstkonto des Servers Zugriff (Projekt-Owner über ihre Projektrollen). | Firestore erlaubt höchstens 1 MiB je Dokument, eine Datenbank kann größer werden. Speicher kostet ca. 2 Cent je GB und Monat. Die älteren Fassungen schützen vor dem wichtigsten Fehler: Eine fehlerhafte App lädt kaputte Daten hoch und überschreibt damit das einzige Backup. Nach Alter statt Anzahl, weil ein Fehler oft erst nach Tagen auffällt. Die App sichert höchstens stündlich (7 × 24 = 168 Fassungen) plus manuell; die Höchstzahl 200 begrenzt nur den Fall, dass mehr gesichert wird als vorgesehen. 200 × 8 MiB im schlimmsten Fall kosten etwa 3 Cent im Monat je Konto. |
| E-B2b-5 | Nie ein neueres Backup überschreiben | Wie in B2: Hochladen nur mit der **erwarteten Version** (0 = noch keins). Der Server setzt das mit der Vorbedingung `ifGenerationMatch` von Cloud Storage um. Die Generation reicht er als Version durch. | Cloud Storage prüft das selbst, ohne Transaktion in Firestore. Gleiches Verhalten wie beim Schlüssel-Backup: `failed_precondition`, neu holen, nicht überschreiben. |
| E-B2b-6 | Zwei eigene Geräte sichern | Ohne Synchronisation gibt es **ein sicherndes Gerät**. Die App sichert automatisch nur, wenn die letzte Fassung von ihr selbst stammt (Geräte-ID im Kopf) oder es noch keine gibt. Sonst fragt sie einmal: „Dieses Gerät übernimmt die Sicherung?“ Bei „nein“ ruht die automatische Sicherung, ohne den Server zu fragen, bis der Mensch von Hand sichert, wiederherstellt oder löscht (2026-11-07). Kann die Rückfrage nicht beantwortet werden (Worker), gilt das wie „nein“; ebenso ruht es nach dem abgelehnten Wiederherstellen einer fremden Fassung (2026-11-09). | Sonst würden sich zwei Geräte mit verschiedenen Daten gegenseitig überschreiben, und jedes Mal ginge der Stand des anderen verloren. Das Zusammenführen ist Aufgabe von B3. |
| E-B2b-7 | Größe und Übertragung | Höchstens **8 MiB** Chiffrat je Sicherung, in **einer Anfrage** über den Server (Connect, eigenes Limit nur für diesen Dienst). Keine signierten URLs. | Der Server prüft Sitzung und Größe an einer Stelle. 8 MiB reichen für eine Datenbank ohne Fotos lange. Signierte URLs sparen nur Rechenzeit, die bei unserer Menge nichts kostet, machen aber Größenprüfung und Vorbedingung schwieriger. |
| E-B2b-8 | Schnittstelle | Neuer Dienst `DatensicherungDienst` mit `Speichern(chiffrat, erwartete_version)`, `Holen()`, `Loeschen()`. `Holen` liefert die neueste Fassung; ältere Fassungen holt **nur ein späterer Aufruf**, wenn er gebraucht wird. **Ergänzt 2026-11-10 (R-4):** `FassungenAuflisten()` (Version, Zeitpunkt, Größe; die neueste zuerst) und `SicherungHolen(version)` (0 = die neueste; eine unbekannte Version ist `not_found`). | Löschen gehört von Anfang an dazu (DSGVO, Art. 17), auch vor „Konto löschen“ in B4. Eigener Dienst und eigene Speicher-Schnittstelle statt einer Erweiterung von `SchluesselDienst`: Das ist der erste Schritt von A5. |
| E-B2b-9 | Formate festschreiben (A3, aus B3 vorgezogen) | `docs/formate.md` beschreibt **maßgeblich** Schlüssel-Backup und Datensicherung: Felder, Verfahren, zugehörige Daten, Formatversion. Dazu **feste Testvektoren**: einmal erzeugte Chiffrate mit Wegwerf-Schlüsseln; Tests prüfen, dass sie sich weiter entschlüsseln lassen. | Sobald echte Daten im Format v1 liegen, muss jede künftige App-Version sie lesen können. Ein Test, der nur verschlüsselt und gleich wieder entschlüsselt, merkt eine Formatänderung nicht; ein festes Chiffrat schon. |
| E-B2b-10 | Vorbereitung für die App | (a) **Gradle-Prüfsummen** (`verification-metadata.xml`) in `client/`. (b) Neues Modul **`verbindung`**: Connect-Clients, Sitzung, Adapter für `BackupAblage` und die Datensicherung, ohne CLI und ohne Dateiablage. `testclient` nutzt es. (c) Einbindung später per **Gradle Composite Build** (`includeBuild`) aus dem App-Repo. | (a) war für „vor der App“ entschieden. (b) Die App braucht dieselbe Anbindung; OkHttp und connect-kotlin laufen auf Android. (c) Beide Repos liegen nebeneinander; so braucht es keine veröffentlichten Pakete und keine Versionspflege. |
| E-B2b-12 | Upload-Limit (2026-11-05) | Höchstens **48 Sicherungen je Konto und Tag** (UTC); danach `resource_exhausted` bis Mitternacht UTC. Gezählt wird nach den Prüfungen (leer, zu groß) und vor dem Speichern, also auch ein Konflikt. Holen höchstens 50-mal, Löschen höchstens 20-mal am Tag (2026-11-06, nach Review-Lauf 8; eigene Arten `sicherung_holen`, `sicherung_loeschen`). Auflisten der Fassungen zählt mit dem Holen zusammen (2026-11-10). Derselbe Tageszähler wie beim Diagnose-Log (`kontingent`, Firestore mit TTL), eigene Art `sicherung`. Die App meldet „Tageslimit“, nicht „zu groß“ (`SicherungTageslimit`; unterschieden an der gesendeten Größe). | Stündlich und nur bei Änderung sind höchstens 24; die andere Hälfte deckt Sicherungen von Hand und zweite Versuche nach Konflikten. Eine App, die in einer Schleife sichert, lädt so höchstens 48 × 8 MiB am Tag hoch (Eingang kostenlos, Schreibvorgänge Bruchteile eines Cents) und verdrängt von den 200 älteren Fassungen höchstens 48: Die Rückfallebene der letzten Tage bleibt. Mehr Schutz (z. B. je Stunde) braucht es erst, wenn das nicht reicht. |
| E-B2b-11 | Wer darf das testen? | Nur Kevin und wenige Bekannte über den **internen Test-Track**, in einer eigenen App-Variante. Eine kurze Testerinformation reicht vorerst; die Datenschutzerklärung bleibt in B6. | Die Daten sind Ende-zu-Ende verschlüsselt; der Server sieht nur Chiffrat, Größe und Zeitpunkt. Konten kann zwar jedes Google-Konto anlegen, aber ohne die App-Variante nutzt das niemand. Größenlimit, ältere Fassungen begrenzt und Kill-Switch halten die Kosten bei Missbrauch klein; Play Integrity kommt in B6. **Präzisiert 2026-11-07:** Im Eigenbetrieb (nur Kevin) keine eigene Variante; das Backup kommt in den normalen Build hinter den Schalter, damit Kevins echte Daten gesichert werden. Variante und Test-Track kommen mit dem ersten Tester (App-Repo `docs/backup-entwurf.md`, A-1). |

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
automatisch höchstens stündlich und nur mit Netz (WorkManager). Dazu kommt ein Knopf
„Jetzt sichern“, den die App auf einmal pro Minute bremst.

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

**Wiederherstellen ersetzt nicht ungefragt (App W-7, im Ablauf seit 2026-11-10).**
Stammt die Fassung von einem anderen Gerät, merkt sich dieses Gerät die Version 0. Die
nächste Sicherung trifft so auf die Fassung des anderen Geräts und fragt, bevor sie sie
ersetzt (E-B2b-6). Ein frisches Testgerät überschreibt also nie die Sicherung, aus der
es eingespielt wurde. Nach dem Einspielen der eigenen Fassung sichert es ohne Rückfrage
weiter. Eine frühere Ablehnung gilt nach dem Wiederherstellen nicht mehr.

### Löschen
`Sicherungsablauf.loeschen` löscht die Sicherung mit allen älteren Fassungen. Das
Gerät behält seinen Zähler (nur die Version wird 0), die nächste Sicherung zählt
also weiter. Finge sie bei 1 an, lehnten andere Geräte, die einen höheren Zähler
kennen, sie als ältere Sicherung ab.

### Konto löschen (B1 in Kap. 3.5, seit 2026-11-12)
`KontoLoeschen` (`konto.proto`) setzt erst den Termin `loeschen_ab`: sofort bei einem
freigeschalteten Gerät, sonst 7 Tage später (ein früherer Termin bleibt). Ist er
erreicht, löscht der Server sofort, sonst antwortet er mit dem Termin. Gelöscht wird
in dieser Reihenfolge (`dienst.Loescher`):
1. **Zugang abschneiden** (`KontoSperren`): Kennung, Sitzungen, Geräte gesperrt.
   Eine gleichzeitige Anmeldung bekommt danach ein neues, leeres Konto.
2. **Inhalte**: Datensicherung mit allen Fassungen, Diagnosen (`diagnose/<konto>/`).
3. **Rest** (`KontoEntfernen`): Geräte, Schlüssel-Backup und zuletzt das Konto mit
   dem Termin.

Jeder Schritt ist wiederholbar. Bricht einer ab, bleibt der Termin, und der
regelmäßige Lauf (`server aufraeumen`, später auch für die Fristen aus P5) holt das
Konto nach. Deshalb lehnt `KontoLoeschenAbbrechen` ab dem Termin ab. Die App verwirft
Sitzung, Geräteschlüssel und Schlüssel nur nach `geloescht = true`
(`protokoll.KontoLoeschen`); bei einem Antrag zeigt sie den Termin, den alle Geräte
in Anmelden, SitzungErneuern und GeraeteAuflisten sehen. Test-Client:
`konto-loeschen`, `konto-loeschen-abbrechen`.

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
- Logs: Geräte-ID, Größe, Version, Ergebnis. Nie Inhalt.
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

## Vor dem ersten weiteren Nutzer (Beginn der Sicherheitsphase)
Entschieden 2026-11-07: Bis dahin gilt **Eigenbetrieb**. Echte Daten gibt es nur von
Kevin; die App darf schon gegen die Cloud sichern, mit Schlüsseln, die Claude kennen
darf (`CLAUDE.md`, „Geheimnisse und Sicherheitsphase“). Alle Schlüssel und der
Pfeffer gelten daher als bekannt: Sie lagen im Klartext in `%APPDATA%`, und Werte
standen in Ausgaben, die Claude gelesen hat. Die Sicherheitsphase beginnt **vor**
dem ersten dieser Schritte: Teilen mit einer anderen Person (B3, auch Familie),
erster Tester, Veröffentlichung im Play Store.

**Vorher fertig** (Sicherheits- und Datenschutzprüfung vom 2026-11-12,
[anforderungen.md](anforderungen.md) Kap. 11):
- **Konto löschen** in Server, `protokoll` und App (B1 in Kap. 3.5), auch als Antrag
  mit 7 Tagen Wartezeit für Geräte ohne Freischaltung; damit lassen
  sich auch Punkt 1 und 2 unten je Konto erledigen. Server und Client fertig
  (2026-11-12, Abschnitt „Konto löschen“ oben); Infrastruktur in
  `infra/aufraeumen.tf` (angewendet 2026-11-13); in der Cloud getestet am
  2026-11-13 (Antrag mit Job und sofort, `stand.md`); offen ist die App.
- **Bestätigung mit der Displaysperre** für *Cloud-Daten löschen*, *Konto löschen*
  und *Gerät sperren* (SE5).
- **Format v2:** Schlüssel aus dem Wiederherstellungsschlüssel per HKDF mit eigenem
  Label (SE6). Danach muss kein v1-Backup mehr gelesen werden.
- **App A-15 neu:** Papier ist der Standard, der Passwortdienst nur nach dem Hinweis
  (Kap. 11, 2026-11-12).
- **`DIAGNOSE_KONTEN`** enthält nach Punkt 5 nur Kevins neues Konto (P9).
- **Zugang nur nach Freigabe** (ID7) und **Kostengrenzen** (ID8, signierte
  Sitzungs-Token, Anfragealarm löst den Kill-Switch aus). Nach dem neuen Pfeffer
  (Punkt 3) die Liste der Kennungen neu füllen, zuerst Kevins.
- **GitHub-Konto** mit Passkey oder Sicherheitsschlüssel: Ohne Branch-Schutz kann ein
  übernommenes Konto über `main` deployen.
- **Dokumente** vor dem ersten Tester bzw. vor B3: Datenschutzerklärung oder
  Testerinformation (P4, P6), Verzeichnis der Verarbeitungstätigkeiten, TOMs,
  DSFA (P7), Google Cloud DPA akzeptiert (P3), Angaben zur Datensicherheit im Play
  Store passend zu A-15.

Zurückgesetzt wird so, dass nichts mit einem alten Schlüssel Verschlüsseltes übrig
bleibt; dann sind die alten Werte in Gesprächsverläufen wertlos. Kevins Daten liegen
dabei nur noch auf dem Handy: nur mit einem funktionierenden Handy beginnen und
gleich danach neu sichern. Checkliste:
1. **Daten auf dem Server löschen:** für jedes Konto die Datensicherung mit allen
   Fassungen (`sicherung-loeschen`) und das Schlüssel-Backup (`schluessel-loeschen`),
   dazu alle Diagnosen im Bucket `…-diagnose`. Danach prüfen, dass die Buckets leer
   sind (auch ältere Fassungen: `gcloud storage ls --all-versions`). Dann
   `werkzeuge/diagnose-holen.sh` einmal laufen lassen: Die lokale Kopie in
   `.diagnose/` folgt dem leeren Bucket.
2. **Firestore leeren:** die Sammlungen `konten`, `google_kennungen`, `geraete`,
   `sitzungen`, `backups` und `kontingente`. Mit dem neuen Pfeffer (Punkt 3)
   entstehen ohnehin neue Konto-IDs; die alten Dokumente wären verwaist.
3. **Neuer Pfeffer:** Kevin legt allein eine neue Version an, der Server liest fest
   die neue Version (Änderung in `infra/server.tf`, tofu-Test anpassen), die alte
   wird zerstört. Danach die Offline-Kopie im Passwortmanager ersetzen (Ablauf in
   [gcp-einrichtung.md](gcp-einrichtung.md)).
4. **Testclient-Dateien löschen:** `%APPDATA%\avlreminder\testclient`. Den Test-Client
   abschalten: Desktop-Client-ID aus `google_client_ids` (`infra/variablen.tf`)
   entfernen, `tofu apply`, und den Desktop-OAuth-Client in der Google-Konsole löschen.
   Bis dahin braucht er die Cloud-Tests (Limits, Trennen) und das Räumen von Konten.
5. **App neu einrichten:** Vorher in der App *Dieses Gerät abmelden*: Das verwirft
   die lokalen Cloud-Schlüssel und die Sitzung, die nach dem neuen Pfeffer zu keinem
   Konto mehr passen (sonst endet die Einrichtung in „Schlüssel eines anderen
   Kontos“). Dann erzeugt die App auf dem Gerät **neue** Geräte- und
   Personenschlüssel und einen neuen Wiederherstellungsschlüssel. Den sieht nur
   Kevin, auf Papier; kein Terminal, kein Screenshot, kein Gespräch mit Claude.
   Danach sofort sichern und einmal wiederherstellen (Probe). Einen alten
   Wiederherstellungsschlüssel im Passwortmanager löschen.
6. **Prüfen:** Ab jetzt meldet Claude, wenn eine Aufgabe einen der Werte bräuchte,
   statt ihn zu lesen. Die alten Claude-Gesprächsverläufe beider Repos löschen
   (`*.jsonl` unter `~/.claude/projects/E--src-avlreminder-backend` und
   `…/E--src-avlreminder`, **nicht** den Ordner `memory/`): Die Werte darin sind
   jetzt wertlos, aber es gibt keinen Grund, sie aufzuheben.

Außerdem, damals unter „Vor echten Daten“ (erledigt bis auf die Offline-Kopie des
Pfeffers, die mit den ersten echten Daten fällig wird):
- **Löschschutz an:** Firestore `deletion_policy = "ABANDON"` und
   `delete_protection_state`, Cloud Run `deletion_protection = true`, Bucket mit
   `force_destroy = false`. (In OpenTofu erledigt 2026-11-05, per `tofu test`
   abgesichert; „vollständig löschbar“ bleibt über das Löschen des Projekts.)
- **Neue Geräte:** entscheiden und umsetzen, dass ein neu angemeldetes Gerät erst
   nach Freischaltung (QR oder Wiederherstellungsschlüssel) andere Geräte sperren und
   Backup oder Datensicherung löschen darf, und dass vorhandene Geräte von jedem neuen
   Gerät erfahren (Grenze in [b1-entwurf.md](b1-entwurf.md), „Gerät sperren“).
   Erledigt 2026-11-05: E-B2b-13 (unten), Server, Schnittstelle, `protokoll`,
   Adapter und Test-Client. Der Hinweis „Neues Gerät angemeldet“ kommt mit der App.
- **Pfeffer offline sichern:** Die Kennung, über die ein Gerät seine (zufällige)
  Konto-ID findet, ist ein HMAC der Google-Konto-ID mit dem Pfeffer. Geht er verloren, findet kein Gerät Schlüssel-Backup und
  Datensicherung wieder. Kevin legt eine Kopie im Passwortmanager ab, sobald die
  App echte Daten sichert (im Eigenbetrieb darf Claude dabei helfen), und ersetzt
  sie in Punkt 3. Ablauf und Wiederherstellen in [gcp-einrichtung.md](gcp-einrichtung.md).
- **Kosten und Rechte:** Upload-Limit je Konto und Tag im Server (erledigt
   2026-11-05, E-B2b-12); der Kill-Switch sperrt ohne actAs, indem er `allUsers`
   entfernt (erledigt 2026-11-05, [stand.md](stand.md)).

Claude kündigt die Sicherheitsphase vorher ausdrücklich an (siehe `CLAUDE.md`,
„Geheimnisse und Sicherheitsphase“). Das Rotieren der Schlüssel selbst kommt erst in
B4; bis dahin heißt „rotieren“ hier: zurücksetzen und neu erzeugen.

### E-B2b-13: Neue Geräte erst nach Freischaltung (bestätigt 2026-11-05)

**Problem:** Wer das Google-Konto auf einem gestohlenen Gerät noch angemeldet hat,
meldet sich mit einem neuen Geräteschlüssel als neues Gerät an und darf heute
alles: andere Geräte sperren, Backup und Datensicherung löschen oder überschreiben.

**Empfehlung:**
- Jedes Gerät hat neben `aktiv`/`gesperrt` das Merkmal **freigeschaltet**. Das erste
  Gerät eines neuen Kontos ist es sofort; jedes weitere startet ohne. Genauer
  (umgesetzt): Ein neues Gerät ist freigeschaltet, wenn das Konto weder ein
  freigeschaltetes, ungesperrtes Gerät noch ein Schlüssel-Backup mit
  Freischaltschlüssel hat; dann gibt es nichts zu schützen. Vorhandene Geräte
  behalten ihr Merkmal beim erneuten Anmelden.
- Ein **nicht freigeschaltetes** Gerät darf nur: Sitzung erneuern, Geräte auflisten,
  sich selbst sperren (abmelden), Schlüssel-Backup und Datensicherung **holen**, das
  Schlüssel-Backup anlegen, wenn es noch keins gibt (E-B2-5) und das Konto kein
  freigeschaltetes, ungesperrtes Gerät hat (2026-11-06), und sich freischalten.
  Alles andere (andere Geräte sperren oder freischalten, Backup überschreiben oder
  löschen, Sicherung speichern oder löschen, Diagnosen senden) lehnt der Server
  mit `permission_denied` ab. Holen ist harmlos: Es sind Chiffrate.
- **Grenzen gegen Missbrauch** (2026-11-06, nach Review-Lauf 8): höchstens 10 nicht
  freigeschaltete, ungesperrte Geräte je Konto (sonst `resource_exhausted` beim
  Anmelden) und höchstens 200 Sitzungen je Gerät und Tag. Gesperrt wirkt sofort:
  Der Interceptor liest bei jedem Aufruf das Gerät; `GeraetSperren` setzt erst das
  Merkmal und löscht danach die Sitzungen in Stapeln.
- **Freischalten auf zwei Wegen:**
  1. Ein freigeschaltetes Gerät bestätigt das neue (`GeraetFreischalten(Geräte-ID)`).
     Dafür braucht es noch keinen QR: Der Server muss nur wissen, dass ein
     vertrautes Gerät zustimmt. Der QR kommt in B3 für das Übertragen der Schlüssel.
  2. Mit dem **Wiederherstellungsschlüssel**: Aus ihm leitet das Gerät (HKDF, eigener
     Zweck) einen Signierschlüssel ab; dessen öffentlichen Teil lädt das erste Gerät
     mit dem Schlüssel-Backup hoch. Das neue Gerät signiert
     `avlreminder/freischalten/v1` + Geräte-ID, der Server prüft gegen den
     gespeicherten öffentlichen Schlüssel (Verfahren in [formate.md](formate.md),
     gemeinsame Testvektoren in `proto/testvektoren/freischaltung.json`). Der Wiederherstellungsschlüssel verlässt
     das Gerät nie, und der Server kann aus dem öffentlichen Schlüssel nichts
     entschlüsseln. Das hilft, wenn kein vertrautes Gerät mehr da ist.
- **Vorhandene Geräte erfahren davon:** `GeraeteAuflisten` liefert, ob ein Gerät
  freigeschaltet ist und wann es sich angemeldet hat. Die App zeigt beim Start einen
  Hinweis „Neues Gerät angemeldet – freischalten oder sperren“. Eine Push-Nachricht
  (FCM) kommt erst mit der App-Einbindung.

**Warum:** Ein Angreifer mit dem Google-Konto, aber ohne vertrautes Gerät und ohne
Wiederherstellungsschlüssel kann dann nichts mehr zerstören, nur Chiffrate holen.
Der Weg über den Wiederherstellungsschlüssel braucht keinen neuen Geheimwert: Er
nutzt, was der Mensch für ein neues Gerät ohnehin eintippt.

**Aufwand:** Server (Feld am Gerät, Prüfungen in den Diensten, zwei neue Aufrufe),
Schnittstelle, `protokoll` (Ableitung und Signatur), Adapter, Test-Client-Befehle;
etwa so groß wie das Diagnose-Log. Die Testgeräte in Firestore haben das Feld noch
nicht; sie werden beim Zurücksetzen vor dem ersten weiteren Nutzer ohnehin gelöscht
(Punkt 2). Bis dahin mit neuem Gerätenamen anmelden (so am 2026-11-07, `r-pc`).

## Bekannte Grenzen (bewusst)
- **Ein sicherndes Gerät.** Zwei Geräte mit verschiedenen Daten führt erst B3
  zusammen.
- **Fotos und Audio** werden nicht gesichert (3.6).
- **Ganz neues Gerät und alte Fassung.** Ohne gemerkten Zähler erkennt das Gerät
  eine vom Server zurückgehaltene neuere Fassung nicht; es sieht nur den Zeitpunkt.
- **Löschen und ein ganz neues Gerät.** Den Zähler kennt jedes Gerät nur lokal. Wird
  die Sicherung gelöscht und sichert danach zuerst ein Gerät ohne gemerkten Zähler,
  beginnt es bei 1; Geräte mit höherem Zähler melden dann „ältere Sicherung“, bis
  sie selbst wieder sichern dürfen. Ausweg: auf einem Gerät mit dem höheren Zähler
  erneut löschen und dort neu sichern (es zählt weiter, die anderen fragen dann nach).
  Selten, weil gelöscht nur bei Neuanfang wird.
- **Gestohlenes Gerät mit Google-Login** kann sich nach dem Sperren neu anmelden,
  ist dann aber nicht freigeschaltet (E-B2b-13): Es kann Chiffrate holen, aber nichts
  sperren, überschreiben oder löschen.
- **Erstes Backup ohne vertrautes Gerät.** Hat das Konto noch kein Schlüssel-Backup
  und kein freigeschaltetes, ungesperrtes Gerät, darf auch ein nicht
  freigeschaltetes Gerät das erste anlegen (E-B2-5), mit seinem eigenen
  Freischaltschlüssel, und sich damit freischalten. Gibt es ein vertrautes Gerät,
  lehnt der Server ab (seit 2026-11-06; vorher war das eine Lücke zwischen
  Anmelden und Hochladen). Die Einrichtung lädt das Backup trotzdem sofort auf dem
  ersten Gerät hoch: Sperrt der Mensch sein einziges Gerät, bevor es ein Backup
  gibt, ist das Konto wieder ungeschützt.
- **Prüfen und Ausführen sind zwei Schritte.** Der Server liest die Freischaltung
  des anfragenden Geräts vor dem Aufruf, nicht in derselben Transaktion. Sperrt
  ein Gerät ein anderes, kann eine Anfrage, die das gesperrte Gerät im selben
  Moment (Millisekunden) gestellt hat, noch ausgeführt werden. Dasselbe gilt für
  die Sitzung (Interceptor).
- **Zwei neue Geräte gleichzeitig.** Melden sich zwei Geräte eines Kontos ohne
  freigeschaltetes Gerät im selben Moment an, kann Firestore beide freischalten
  (die Abfrage der Geräte ist kein Teil der Sperre der Transaktion). Ohne vertrautes
  Gerät gibt es dann ohnehin nichts zu schützen.
  Aus demselben Grund kann die Grenze von 10 nicht freigeschalteten Geräten bei
  gleichzeitigen Anmeldungen um wenige überschritten werden.
- **Testgeräte von vor E-B2b-13** sind in Firestore nicht freigeschaltet (das Feld
  fehlt). Mit einem neuen Gerätenamen anmelden; gelöscht werden sie beim
  Zurücksetzen vor dem ersten weiteren Nutzer (Punkt 2).
- **Schlüssel-Backup nach dem Löschen.** Die Version beginnt wieder bei 1. Ein Gerät,
  das noch Version 1 erwartet, könnte so ein neues Backup überschreiben. Heute
  harmlos, weil Clients nur mit `erwartet = 0` speichern (Kommentar in
  `schluessel.proto`); ein Grabstein kommt, wenn Clients höhere Versionen nutzen.
- **Konto löschen und ein laufender Upload.** Eine Datensicherung, die schon vor dem
  Sperren angenommen war, kann nach dem Löschen der Inhalte noch ankommen und bleibt
  dann liegen, ohne Konto (nur Chiffrat). Selten (Millisekunden); ein Aufräumen
  verwaister Objekte gehört zu den Fristen aus P5.
- **Konto löschen und die Tageszähler.** Sie bleiben bis zu ihrem Ablauf (TTL,
  höchstens zwei Tage); sie enthalten nur die Konto-ID und Zahlen.
- **Zwischen Sperren und Entfernen** (nur nach einem Abbruch) meldet sich ein altes
  Gerät mit seinem Schlüssel als fremdes Gerät; ein neuer Geräteschlüssel bekommt
  ein neues Konto.
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
- Fotos und Audio (3.6). Konto löschen ist seit 2026-11-12 vorgezogen (B1 in
  Kap. 3.5); Mitgliedschaften und Operationen kommen mit B3 dazu.
