# Entwurf: Diagnose-Log aus der App

> Stand: 2026-11-04. Entscheidungen von Kevin **bestätigt** am 2026-11-04 (auch in
> [anforderungen.md](anforderungen.md), Kap. 11). Anlass: Die App wird
> komplexer und hat kleinere Fehler; Kevin möchte Diagnosedaten direkt aus der App
> nach GCP schicken, damit Claude sie lesen kann. EU, automatische Löschung.
>
> **Umgesetzt am 2026-11-04:** Server (`DiagnoseDienst.DiagnoseSenden`, Tageszähler
> `server/internal/kontingent`), Infrastruktur (`infra/diagnose.tf`) und Skript
> (`werkzeuge/diagnose-holen.sh`). Das Format steht in [formate.md](formate.md).
> Die App (Teil 3) fehlt noch.

## Ziel
- Die App sammelt **technische Ereignisse ohne Inhalte** in einem Ringpuffer auf
  dem Gerät.
- Auf Knopfdruck (und nach einem Absturz auf Nachfrage) schickt sie den Puffer
  **unverschlüsselt** an den Server – lesbar für Kevin und Claude.
- Der Server legt jede Sendung in einem Bucket in `var.region` ab; nach **14 Tagen**
  löscht Google sie von selbst.
- Ein Skript holt die Sendungen auf den Rechner; Claude liest sie dort.

Bewusst **nicht**: Analytics, Nutzungsstatistik, automatisches Senden ohne Knopf,
Inhalte (Erinnerungstexte, Titel, Notizen, Uhrzeiten von Erinnerungen).

## Entscheidungen (am 2026-11-04 bestätigt)
| Nr. | Frage | Entscheidung | Begründung |
|---|---|---|---|
| E-D-1 | Was steht im Log? | Ereignisse mit Zeit, Stufe, Bereich, **festem Ereignisnamen** und typisierten Feldern (Zahl, Wahrheitswert, Aufzählung). Dazu je Sendung: App-Version, Variante, Android-Version, Gerätemodell, Schema-Version der Datenbank. Bei Ausnahmen Klasse und Stacktrace, **ohne Meldungstext**. | Meldungstexte und freie Zeichenketten können Inhalte enthalten (z. B. SQL-Fehler mit Werten). Die Logger-API nimmt deshalb keine freien Zeichenketten an; was nicht hineinkommt, kann nicht durchsickern. |
| E-D-2 | Wann wird gesendet? | Nur auf Wunsch: Knopf „Diagnose senden“ im Debug-Menü; nach einem Absturz fragt die App beim nächsten Start. | Einwilligung je Sendung, keine Überraschung für Tester; kein Hintergrundverkehr. |
| E-D-3 | Wer darf senden? | Am Anfang **nur freigeschaltete Konten** (Liste von Konto-IDs in einer Umgebungsvariable des Servers, z. B. `DIAGNOSE_KONTEN`). Alle anderen bekommen `permission_denied`. | Nur Kevin liest; fremde Konten können den Bucket nicht füllen, und es entstehen keine Diagnosedaten von Personen, die nicht eingewilligt haben. Tester kommen später einzeln dazu. |
| E-D-4 | Wo liegt es? | Eigener Bucket `…-diagnose` in `var.region`, ohne Versionierung und ohne Soft Delete, Lebenszyklus „löschen nach 14 Tagen“. Objekt `diagnose/<konto_id>/<zeit>-<zufall>.jsonl.gz`. | Getrennt von den Datensicherungen (andere Rechte, andere Frist). Der Präfix je Konto erlaubt, beim Löschen eines Kontos auch seine Diagnosen zu löschen. Cloud Logging scheidet aus: Einträge lassen sich nicht einzeln löschen, und es mischte sich mit den Server-Logs. |
| E-D-5 | Server | Neuer Dienst `DiagnoseDienst.Senden` (Connect) mit Sitzung; Nutzlast gzip, höchstens **256 KiB**; höchstens **10 Sendungen je Konto und Tag** (gleicher Zähler-Mechanismus wie das Upload-Limit der Datensicherung). Der Server prüft nur Größe und gzip-Kopf, er liest den Inhalt nicht. | Klein, billig, im selben Server; das Limit schützt die Kosten. Das Server-Log nennt nur Gerät, Größe und Ergebnis. |
| E-D-6 | Lesen | Skript `werkzeuge/diagnose-holen.sh`: lädt neue Sendungen nach `.diagnose/` (in `.gitignore`) und entpackt sie. Claude liest sie dort und nie per Konsole. | Ein Befehl für Kevin und Claude, nichts landet im Repo. |
| E-D-7 | Darf Claude das lesen? | Ja, als Ausnahme in `CLAUDE.md`: **Diagnose-Logs freigeschalteter Konten** (E-D-3), weil sie per Konstruktion keine Inhalte enthalten. Fällt Claude doch ein Inhalt auf, meldet es das als Fehler im Logger und liest nicht weiter. | Die heutige Regel verbietet „Daten von Testern“; ohne Ausnahme könnte Claude die Logs von Kevins Handy mit echten Daten nicht lesen. |
| E-D-8 | Datenschutz | Testerinformation um einen Absatz ergänzen (was, wozu, 14 Tage, nur auf Knopfdruck). Konto löschen löscht auch die Diagnosen. | Pseudonyme Gerätedaten mit Einwilligung je Sendung; kurze Frist. |

**Abgelehnt:** Firebase Crashlytics (Verarbeitung in den USA, eigenes SDK),
Sentry o. Ä. (weiterer Anbieter und Vertrag), Cloud Logging (siehe E-D-4).

## Format einer Sendung
Eine gzip-Datei mit JSON-Zeilen. Die erste Zeile beschreibt die Sendung, jede
weitere ein Ereignis:

```json
{"art":"kopf","format":"avlreminder/diagnose/v1","app":"1.8.0 (42)","variante":"cloud","android":35,"modell":"Pixel 8","schema":7,"anlass":"knopf"}
{"zeit":"2026-11-04T09:12:03.120Z","stufe":"WARN","bereich":"sicherung","ereignis":"hochladen_fehlgeschlagen","felder":{"versuch":3,"code":"unavailable"}}
{"zeit":"2026-11-04T09:12:04.002Z","stufe":"ERROR","bereich":"erinnerung","ereignis":"alarm_nicht_gesetzt","ausnahme":{"klasse":"java.lang.SecurityException","stapel":["…AlarmPlaner.setze(AlarmPlaner.kt:58)","…"]}}
```

`code` ist ein Wert aus einer Aufzählung, keine freie Zeichenkette. Maßgeblich ist
[formate.md](formate.md).

## App (eigener kurzer Entwurf im App-Repo)
- Logger-API in der App, z. B. `diagnose.warn(Bereich.SICHERUNG, Ereignis.HOCHLADEN_FEHLGESCHLAGEN, "versuch" to 3)`;
  Feldwerte nur `Int`, `Long`, `Boolean` oder `enum`. Ein Lint- oder Unit-Test
  stellt sicher, dass `android.util.Log` nur noch über diesen Logger läuft.
- Ringpuffer in einer Datei, höchstens etwa 500 KiB unkomprimiert; älteste
  Einträge fallen heraus. Beim Senden wird gzip-komprimiert.
- Absturz: Ein `UncaughtExceptionHandler` schreibt Klasse und Stapel in den Puffer;
  beim nächsten Start fragt die App, ob sie die Diagnose senden soll.
- Senden braucht eine Sitzung, also die Anmeldung aus B2b. Bis dahin lässt sich der
  Puffer im Debug-Menü als Datei teilen (ohne Server).

## Server und Infrastruktur
- `proto/avlreminder/v1/diagnose.proto`: `DiagnoseDienst.DiagnoseSenden(DiagnoseSendenRequest{bytes nutzlast})`.
- `server/internal/dienst/diagnose.go` mit Ablage-Schnittstelle (Arbeitsspeicher für
  Tests, Cloud Storage); tabellengetriebene Tests: nicht freigeschaltet, zu groß,
  kein gzip, Limit erreicht, ohne Sitzung – je mit Ablehnungsfall.
- `infra/diagnose.tf`: Bucket, Lebenszyklus 14 Tage, `objectCreator` für das
  Server-Konto (nur schreiben, nicht lesen oder löschen); Umgebungsvariablen
  `DIAGNOSE_BUCKET` und `DIAGNOSE_KONTEN`. `tofu test` prüft Region, Frist und Rechte.
- Konto löschen (später): Präfix `diagnose/<konto_id>/` mit löschen; dafür bekäme
  der Server zusätzlich Löschrechte oder ein eigener Ablauf übernimmt das.

## Kosten
Praktisch null: wenige Sendungen zu je unter 256 KiB, 14 Tage gespeichert. Selbst
1000 Sendungen im Monat lägen weit unter einem Cent Speicher.

## Aufwand und Reihenfolge
1. Server, proto und Infrastruktur: etwa ein halber bis ganzer Tag samt Tests.
2. Skript zum Holen und Ausnahme in `CLAUDE.md`: eine Stunde.
3. App: Logger, Ringpuffer, Absturz, Knopf: etwa ein Tag, im App-Repo.

Teil 1 und 2 kommen **vor** Schritt 7 von B2b (App-Einbindung), Teil 3 als
erstes Stück von Schritt 7, gleich nach der Anmeldung. Dann hilft das Diagnose-Log
schon beim Einbinden des Backups. Den Logger selbst (ohne Senden) kann die App
sofort bekommen.

## Bekannte Grenzen
- Unverschlüsselt: Google und alle mit Lesezugriff aufs Projekt sehen die Logs.
  Darum keine Inhalte (E-D-1).
- Ein Stapel kann Namen eigener Klassen und Methoden zeigen, aber keine Daten.
- Abstürze im nativen Code oder vor dem Start des Loggers erfasst der Puffer nicht.
