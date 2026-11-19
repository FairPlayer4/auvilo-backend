# Review-Gedächtnis
Letzter Lauf: 2026-11-18 @ db22b1a (nur Backend; App zuletzt in Lauf 10 @ 4648692)   Laufzähler: 11
Letztes Architektur-Review: Lauf 8 (2026-11-05); nächstes in Lauf 12.

## Beabsichtigte Architektur
Vom Entwickler bestätigt am 2026-11-01.

- **Monorepo:** `proto/` (Schnittstelle, Protobuf/Connect mit buf), `server/` (Go),
  `client/` (Kotlin: `protokoll` ohne Netz/Android, `verbindung` mit Connect-Adaptern,
  `testclient` nur CLI und Dateiablage), `infra/` (OpenTofu und
  Hilfsfunktionen wie `killswitch`), `docs/`. Seit B2 (Lauf 4) gibt es alle
  fünf Teile.
- **Abhängigkeitsregeln:**
  - Server und Client hängen nur über `proto/` zusammen.
  - Krypto und Konfliktauflösung liegen ausschließlich im Client.
  - Der Server ist „dumm“: Anmeldung, Mitgliedschaft prüfen, Log anhängen,
    ausliefern, KI-Proxy. Er versteht keine Inhalte.
  - Funktionen in `infra/` sind vom Server unabhängig.
- **Vertrauensgrenzen:**
  - Internet → Cloud Run: nur mit Anmeldung.
  - Budget → Pub/Sub → Kill-Switch: nur intern.
  - GitHub → GCP über Workload Identity Federation: nur Repo plus `main`.
  - Klartext erreicht den Server nur über den KI-Proxy, nach Freigabe.
- **Konventionen:**
  - Reine Logik getrennt von GCP-Aufrufen (Interface beim Verwender, Attrappen in Tests).
  - Tests tabellengetrieben, deutsche Testfallnamen, Ablehnungsfälle.
  - Kritischer Code ist mit `// KRITISCH:` markiert.
  - Alle Ressourcen nutzen `var.region` (Positivliste EU-Regionen).
  - Keine Inhalte in Logs, Logs nur in der EU.
  - Die proto-Kommentare sind maßgeblich für Bytes, Kodierung und Fehlercodes (seit Lauf 3).

## Bereiche
| Bereich | Pfade | Anforderungen/Doku | Tests | Zuletzt reviewt | Zustand |
|---|---|---|---|---|---|
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, ID8, stand.md | `*_test.go` | 2026-11-18 (Lauf 11, nur `EntscheideAlarm` und Anfrage-Alarm in `alarm.tf`) | gut; Sperrweg am 2026-11-06 in der Cloud getestet; Alarm-Pfad ungetestet, kein Renotify (Frage 3 Lauf 11) |
| Infrastruktur | `infra/*.tf`, `infra/*.sh` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md; stand.md | `infra/tests/` (tofu test), `pruefe-regionen.sh` | 2026-11-05 (Lauf 8, Modul `oeffentlich`, Löschschutz) | gut nach Fixes; CI-Gate und Textprüfung der Öffnung seit 2026-11-10 |
| Server: Anmeldung/Sitzung | `server/internal/{geraeteschluessel,anmeldung,sitzung,dienst/dienst.go,dienst/freigabe.go}`, `proto/` | b1-entwurf.md (E-B1-3 veraltet), anforderungen.md ID7/ID8, proto-Kommentare | `*_test.go` | 2026-11-18 (Lauf 11, ID7/ID8, signierte Token) | gut; Token solide, minor offen (Token v2, Erneuern liest vor Prüfung, Kennung im Log) |
| Konto löschen und Fristen | `server/internal/dienst/konto.go`, Konto/Antrag/Grabsteine in `speicher`, `Loeschen` beider Ablagen, `cmd/server` (aufraeumen), `infra/aufraeumen.tf`, Kotlin `KontoLoeschen` | anforderungen.md B1, P5, Kap. 11 2026-11-12/14; `docs/datenschutz/`; konto.proto | `konto_test.go`, `aufraeumen_test.go`, `vertrag_test.go`, `KontoLoeschenTest` | 2026-11-18 (Lauf 11, neu) | major offen (Frage 1 Lauf 11: kein `geloescht` nach Teilfehler); Mutanten M1–M4 alle rot |
| Server: Speicher/Start | `server/internal/speicher`, `server/cmd/server` | b1-entwurf.md (Daten, Betrieb) | `vertrag_test.go` (auch Emulator), `main_test.go` | 2026-11-05 (Lauf 8) | gut; Entscheidungen als reine Funktionen (E10, 2026-11-10) |
| Server: Datensicherung | `server/internal/datensicherung`, `dienst/datensicherung.go`, `datensicherung.proto` | b2b-entwurf.md, proto-Kommentare | `vertrag_test.go` (Arbeitsspeicher, fake-gcs), `datensicherung_test.go` | 2026-11-08 (Lauf 9, Tageslimits) | gut nach Fix (72faf01) |
| CI, Deploy und Container | `.github/`, `server/Dockerfile`, `.golangci.yml`, `proto/buf*.yaml` | anforderungen.md Q4, go-review.md, stand.md | CI-Job `container` | 2026-11-03 (Lauf 6, CI-Fix) | gut; Emulator ohne feste Version (Nit) |
| Doku | `docs/`, `CLAUDE.md`, `README.md`, beide `stand.md` | – | – | 2026-11-09 (Lauf 10, Kürzbarkeit beider Repos) | stand.md ist Tagebuch und teils falsch; Kürzungsplan in runs/2026-11-09.md |
| Kotlin `protokoll` | `client/protokoll`, `proto/avlreminder/inhalt`, `proto/testvektoren` | b2-entwurf.md, b2b-entwurf.md, backup.proto | `*Test.kt`; Go prüft nur `geraetenachrichten.json` | 2026-11-18 (Lauf 11, Krypto-Kern; davor als Krypto nur Lauf 4) | gut nach Fixes (c7f6b0a, fb5e626); Formate unabhängig in Python nachgerechnet; R-4 fehlt hier noch |
| Kotlin `verbindung` | `client/verbindung` | b2b-entwurf.md, proto-Kommentare | `AdapterTest` (Connect-Nachbau) | 2026-11-08 (Lauf 9, mit App) | gut; ohne Netz `IOException`; Adapter mit Token-Lieferant (R2, 2026-11-10) |
| Backup und Test-Client | `server/internal/dienst/schluessel.go`, Backup in `speicher`, `client/testclient`, `schluessel.proto` | b2-entwurf.md, proto-Kommentare | `schluessel_test.go`, `vertrag_test.go`, `EinrichtungTest`, `GoogleTest`, `AblageTest` | 2026-11-08 (Lauf 9, Test-Client) | gut; Meldung nach erfolgtem Löschen (minor) |
| App-Einbindung (App-Repo) | `../avlreminder/app/…/cloud/`, Einstellungen, `gradle/backend-commit.txt`, Composite Build, App-CI | App `docs/backup-entwurf.md` (A-1…A-15), b2b-entwurf.md | `CloudBackupTest` (Attrappen), `CloudBackupUiTest`, App-CI mit Lint; Gerätetest von Hand | 2026-11-09 (Lauf 10, Schritte 3–5, zweiter Blick) | 3 major offen (Löschen ohne Sitzung, Automatik nach abgelehnter Sitzung, andere Geräte beim Löschen); Attrappe ohne Serverregeln (R4) |
| App: Wiederherstellen (App-Repo) | `../avlreminder/app/…/restore/`, Prüfen/Wiederherstellen in `CloudBackup`, `AvlApp.finishRestore`, `RestoreViews` | App `docs/wiederherstellen-entwurf.md` (W-1…W-8, R-1…R-6), design.md 13 | `RestoreFilesTest` (echte DB-Dateien), `LocalRestoreTest`, `CorruptCopyTest` | 2026-11-09 (Lauf 10, neu, zweiter Blick) | critical gefixt (82528b0, App); major offen: Einstellungen blockieren den Auftrag (R1); Room löscht beschädigte DB (R2) |
| Server: Diagnose | `server/internal/{diagnose,kontingent}`, `dienst/diagnose.go`, `diagnose.proto` | diagnose-entwurf.md, formate.md, proto | `vertrag_test.go` (Arbeitsspeicher, Emulatoren), `diagnose_test.go` | 2026-11-05 (Lauf 8, mit Datensicherung) | gut |
| Diagnose: Skript und Kotlin | `werkzeuge/diagnose-holen.sh`, `client/*/Diagnose*`, `ServerDiagnoseAblage`, `diagnose-senden` | diagnose-entwurf.md E-D-6 | `DiagnoseTest`, `AdapterTest`, `TestDiagnoseTest`; Skript nur von Hand | 2026-11-04 (Lauf 7, neu) | gut nach Fixes; App sendet seit 2026-11-08 |
| Kotlin-Build | `client/*.gradle.kts`, `client/gradle/`, CI-Job `kotlin` | b2-entwurf.md (CI) | CI | 2026-11-05 (Lauf 8) | gut; Prüfsummen aller Abhängigkeiten |
| Freischaltung (E-B2b-13) | `server/internal/{freischaltung,dienst/freischaltung.go}`, Freischaltung in `speicher`, `geraete.proto`, Kotlin `Einrichtung` | b2b-entwurf.md, geraete.proto | `freischaltung_test.go`, `vertrag_test.go`, `EinrichtungTest` | 2026-11-08 (Lauf 9) | major offen: zweites Gerät hängt ohne gültigen Schlüssel (Frage 1 Lauf 9); Selbstsperren umgeht 10 offene Geräte (Frage 4) |

## Trends
Das eigentliche Gedächtnis: wiederkehrende Muster, nach denen jeder Lauf gezielt schaut.
- **Test hängt an der geprüften Konstante** (Lauf 2–5, wieder Lauf 11: Kotlin
  `toString verrät den Schlüssel nicht` prüft `"…(***)"`). Regel seit 2026-11-02 in
  go-review.md und CLAUDE.md; alter Code nie gezielt durchsucht (E1 Lauf 5).
- **Test kann nicht rot werden / trifft die Grenze nicht** (Lauf 5–7). Bei neuen Tests
  einen Gegenwert wählen.
- **Schutz nur positiv geprüft** (Lauf 7–11; Lauf 11: KRITISCH „leere Kennung nie“
  ungetestet (M7), Erneuern-Ablehnung prüft keine Seiteneffekte). Bei
  Erlaubnis-Tests die Wirkung prüfen, bei Schutz-Tests auch „zusätzlich zu weit“.
- **KRITISCH-Zweig ohne Test** (Lauf 11, neu: M7, M9, `KontoSperren`-Rennen, fälliger
  Termin ohne Freischaltung). Mutanten gezielt an `// KRITISCH:`-Stellen setzen.
- **Testattrappe bildet Serverregel nicht nach** (Lauf 9, 10, 11 – Trend): `FalscheAblage`,
  App `FakeServer` (R4 Lauf 10), `datensicherung.Arbeitsspeicher.Loeschen("")` (Lauf 11).
  Gemeinsamer Fake mit Serverregeln empfohlen.
- **Entwürfe und Datenschutz-Doku laufen dem Code hinterher** (Lauf 11, neu): b1-entwurf
  E-B1-3, b2-entwurf Schnittstelle, E-B2b-2, datenschutz/README „Kennung in keinem Log“,
  konto.proto „zwei Tage“. Bei Datenschutz-Dokumenten zählt jede Abweichung als Fund.
- **Abläufe außerhalb von `protokoll`** (Lauf 4, 5, 8, 10): Einrichtung, Sitzung,
  Rückfragen, Reset-Folge (`CloudLoeschen`) und W-7 sind umgezogen (zuletzt 2026-11-10).
  Beim nächsten App-Review prüfen, ob neue Regeln wieder in der App entstehen.
- **Drift: `speicher.Speicher` und `dienst` wachsen** (Lauf 4 → 8: 6 → 10 Methoden).
  Am 2026-11-10 (Phase D) angegangen: Richtlinie je Prozedur (E2), ein Typ je Dienst mit
  kleinen Schnittstellen, Speicher-Entscheidungen als reine Funktionen (E10). Beim
  nächsten Architektur-Review prüfen, ob es so bleibt.
- **stand.md wird Tagebuch** (Lauf 6, 8, 10). Kürzungsplan in runs/2026-11-09.md.
  Erledigtes steht nur in Berichten und git, nicht in state.md oder stand.md.

## Offen (Details in den Berichten)
- Lauf 11 (runs/2026-11-18.md, Fokus Datenschutz und Verschlüsselung): Fragen 1–4
  umgesetzt am 2026-11-19 (c3a18da, 1204855, 2f42377, 3e92b78, 3508e95); offen davon
  nur `tofu apply` (Renotify) und der Cloud-Test des Anfrage-Alarms, beide nach Kevins
  Go, und `ServerKontoLoeschen` mit Anmeldung in der App. Offen: Empfehlungen Token v2 (Sicherheitsphase), Erneuern-Reihenfolge,
  Doku-Drift, Testlücken, Fingerabdruck-Vergleich in der Einrichtung, R-4 in `protokoll`.
  Gefixt auf review/2026-11-18: f0e54ea, c7f6b0a, fb5e626.
- Lauf 10 (runs/2026-11-09.md): erledigt am 2026-11-09 (Phase A) R1, R2, R4 (Regel fürs
  erste Schlüssel-Backup in beiden Attrappen, kein gemeinsames Modul), Fragen 1–3, die
  minor-Funde `setUp`/`unlocked` und Mutex in `work`, der wacklige Test; am 2026-11-10
  (Phase C) R3 und W-7 „zu weit“ aus R5. Offen: R5 (übrige Tests), übrige minor-Funde (Rückfrage hält den Mutex,
  halb gescheitertes Löschen, `cannotActOnServer`, `compareNow`, W-7-Reihenfolge,
  `beforeName`, `takeIfDue`, `RestoreFiles`), Doku kürzen.
- Lauf 9 (runs/2026-11-08.md): Fragen 1 und 4 und R3 umgesetzt und deployt am 2026-11-09; R2 am 2026-11-10 (Phase D); R5 (Cloud-Fälle in App `geraetetest.md`)
  kommt mit der Überarbeitung der Gerätetests.
- Lauf 8 (runs/2026-11-05.md), Stand am 2026-11-09 im Code geprüft: erledigt E1, E5, E12;
  weitgehend E6; erledigt am 2026-11-10 (Phase B) E3, E4, E9, E14, E16, E18, (Phase C)
  E7 und E8 (`Fehlergrund` als Error-Detail zurückgestellt, anforderungen.md 2026-11-10),
  (Phase D) E2, E10, E11 (als Vorgabe B3-1), E13. Offen E15 (Plätze je Konto, U2), E17
  (stand.md, mit dem Kürzungsplan aus Lauf 10).
- Lauf 6 (runs/2026-11-03-2.md): E4 (30 s bei 8 MiB nicht gemessen), E5 (Skript-Check
  Cloud-Run-Regeln, Leerlauf 0 €), E6 (CLAUDE.md nennt formate.md, b1-entwurf.md nicht).
- Lauf 5 (runs/2026-11-03.md): Testlücken Server (Streaming im Interceptor, Sperr-Rennen
  in `SitzungErneuern`, Speicherfehler im Gerätedienst) – seit Lauf 2.
- Lauf 4 (runs/2026-11-01-4.md): A2 und A4 als Vorgaben für B3 (anforderungen.md 4.6),
  A5 als E10 erledigt (2026-11-10).
- Lauf 1–3: Drossel beim Laden der Google-Schlüssel, WARN „Trennen beginnt“, E7 Lauf 1 (Herkunft der Budget-Meldung,
  optional).

## Offene Fragen an den Entwickler
Keine.

## Getroffene Entscheidungen
- 2026-11-01: Architekturbild bestätigt; `.review/` wird versioniert.
- 2026-11-18: Der review-loop darf `review/<datum>` anlegen (Kevin auf Rückfrage, trotz
  „Feature-Branches nur nach Bestätigung“ in CLAUDE.md). Kein Push, kein Merge.
- 2026-11-18: Bekannt und nicht erneut melden: Umschlag ohne Absenderbindung und
  Typprüfung (B3-2 bis B3-4), SE6, Rollback des Schlüssel-Backups, Desktop-Client-ID
  bis zur Sicherheitsphase.
- 2026-11-19 (Lauf 11, Fragen 1–4, Kevin: „Alle Fragen nach Vorschlag umsetzen“):
  `KontoLoeschen` meldet gelöscht, sobald der Zugang gesperrt ist, der Rest kommt im
  Lauf; der Client prüft nach einem Fehler per `SitzungErneuern`. Die Google-Kennung
  kommt nie ins Log, auch nicht gekürzt. Der Anfrage-Alarm bleibt bei 10.000/h und ist
  für jeden billig auslösbar (bewusst, anforderungen.md Kap. 11); nicht erneut melden.
  Der Lauf löscht Sicherungen und Schlüssel-Backups ohne Konto nach 1 h.
- 2026-11-01: Keine Pflicht-Statuschecks (GitHub Free, privat) – Konvention reicht.
  Nicht erneut als Fund melden.
- 2026-11-01: Kosten vor Verfügbarkeit: `tofu apply` darf gesperrte Dienste nicht wieder
  öffnen (seit 2026-11-05 über `infra/module/oeffentlich`). Hohe Kosten gelten als
  Zeichen für Fehler/Angriff.
- 2026-11-01 (Lauf 2): Wiederholte Anfragen im Zeitfenster sind bewusst erlaubt
  (anforderungen.md Kap. 11). Nicht erneut als Fund melden.
- 2026-11-01 (Lauf 2): Geräte-IDs dürfen in Logs stehen (anforderungen.md Kap. 11).
  Nicht erneut als Fund melden; Token, `sub` und Inhalte bleiben verboten.
- 2026-11-01 (nach Lauf 3): IP-Adressen in den Anfrage-Logs 30 Tage sind gewollt
  (anforderungen.md Kap. 11). Nicht erneut als Fund melden.
- 2026-11-01: `EXCLUDE_ALL_CREDITS` bleibt (auch Gratis-Kontingent zählt). Nicht erneut
  als Fund melden.
- Pfeffer: Schutz seit 2026-11-01; Wiederherstellung am 2026-11-07 geprobt; Offline-Kopie
  seit 2026-11-08 (die frühere Entscheidung „keine bis zu echten Nutzern“ ist überholt).
- 2026-11-02 (nach Lauf 4): Wiederherstellungsschlüssel vor dem Hochladen zeigen und
  bestätigen lassen. Regel „Tests prüfen Verhalten, nicht die Konstante“. A2/A3 werden
  im B3-Entwurf geklärt.
- 2026-11-03 (nach Lauf 5, alle zehn Fragen, anforderungen.md Kap. 11): Neuanmeldung
  nach Sperren bekannte Grenze bis vor echten Daten; Sitzung vor dem Lesen geprüft;
  Schlüssel-Backup-Version ab 1 nach Löschen (kein Grabstein); Wiederherstellen =
  Übernehmen, gesehene Zähler; Löschen in `protokoll` behält den Zähler; Fassungen
  (seit Lauf 6: 200, stündlich); Kill-Switch-WARN nur bei Eingriff (ab 100 %);
  Löschschutz vor echten Daten; SE1 meint Sync-Operationen ab B3; SE3 ohne eigenen
  Scanner. Nicht erneut als Fund melden.
- 2026-11-03: Bucket ohne Soft Delete; `GOMEMLIMIT=192MiB`, Parallelität 80;
  Absenz-Alarm 23,5 h.
- 2026-11-05: Adapter unterscheiden „zu groß“ und „Tageslimit“ an der gesendeten Größe;
  Upload-Limit 48 je Tag (E-B2b-12); Sperren ohne actAs über `allUsers`.
- 2026-11-06 (nach Lauf 8, Fragen 1–5): Diagnosen nur von freigeschalteten Geräten;
  erstes Backup eines nicht freigeschalteten Geräts nur, wenn das Konto kein
  freigeschaltetes, ungesperrtes Gerät hat; Holen 50, Löschen 20, Sitzungen 200 je Gerät
  und Tag; Interceptor prüft „gesperrt“, Sperren löscht in Stapeln; höchstens 10 offene
  Geräte je Konto; Egress-Alarm 1 GiB in 23 h. Nicht erneut als Frage stellen.
- 2026-11-07 (Lauf 8, Fragen 6, 7): Ablehnung gemerkt, automatische Sicherung ruht;
  2 Instanzen bleiben.
- 2026-11-09 (Lauf 9 Fragen 1–4, Lauf 10 Fragen 1–3, R2–R4; anforderungen.md Kap. 11,
  App Kap. 9): Server prüft die Version vor „geschützt“ und schaltet beim ersten Backup
  frei; abgelehnte fremde Fassung ruht; Worker: unbeantwortet = nein; höchstens 10 neue
  Geräte je Konto und Tag; *Cloud-Daten löschen* sperrt alle Geräte; abgelehnte Sitzung
  benachrichtigt, 3-Tage-Hinweis am Konto; lokales Wiederherstellen wie W-7; R2 bis B3;
  R4 erledigt durch *Dieses Gerät abmelden*. `FILE_SAME_JAVA_*` in `proto/buf.yaml` ist
  gewollt (B2). Nicht erneut als Frage stellen.
