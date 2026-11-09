# Review-Gedächtnis
Letzter Lauf: 2026-11-09 @ 6d04e02, App `feature/backup` @ 4648692 (Fix 82528b0 auf App-Branch review/2026-11-09, nicht gemergt)   Laufzähler: 10
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
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, stand.md | `*_test.go` | 2026-11-05 (Lauf 8, IAM-Sperrweg) | gut; Sperrweg am 2026-11-06 in der Cloud getestet |
| Infrastruktur | `infra/*.tf`, `infra/*.sh` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md; stand.md | `infra/tests/` (tofu test), `pruefe-regionen.sh` | 2026-11-05 (Lauf 8, Modul `oeffentlich`, Löschschutz) | gut nach Fixes; Deploy ohne CI-Gate, Textprüfung der Öffnung (E3, E4) |
| Server: Anmeldung/Sitzung | `server/internal/{geraeteschluessel,anmeldung,sitzung,dienst/dienst.go}`, `proto/` | b1-entwurf.md, proto-Kommentare, anforderungen.md Kap. 11 | `*_test.go` | 2026-11-05 (Lauf 8) | gut; Schlüsselausfall = abgelehnt (E14) |
| Server: Speicher/Start | `server/internal/speicher`, `server/cmd/server` | b1-entwurf.md (Daten, Betrieb) | `vertrag_test.go` (auch Emulator), `main_test.go` | 2026-11-05 (Lauf 8) | gut; `Speicher` 10 Methoden (E10, Trend) |
| Server: Datensicherung | `server/internal/datensicherung`, `dienst/datensicherung.go`, `datensicherung.proto` | b2b-entwurf.md, proto-Kommentare | `vertrag_test.go` (Arbeitsspeicher, fake-gcs), `datensicherung_test.go` | 2026-11-08 (Lauf 9, Tageslimits) | gut nach Fix (72faf01) |
| CI, Deploy und Container | `.github/`, `server/Dockerfile`, `.golangci.yml`, `proto/buf*.yaml` | anforderungen.md Q4, go-review.md, stand.md | CI-Job `container` | 2026-11-03 (Lauf 6, CI-Fix) | gut; Emulator ohne feste Version (Nit) |
| Doku | `docs/`, `CLAUDE.md`, `README.md`, beide `stand.md` | – | – | 2026-11-09 (Lauf 10, Kürzbarkeit beider Repos) | stand.md ist Tagebuch und teils falsch; Kürzungsplan in runs/2026-11-09.md |
| Kotlin `protokoll` | `client/protokoll`, `proto/avlreminder/inhalt`, `proto/testvektoren` | b2-entwurf.md, b2b-entwurf.md, backup.proto | `*Test.kt`; Go prüft nur `geraetenachrichten.json` | 2026-11-09 (Lauf 10, als Aufrufer der App) | gut; Rückfragen `suspend` (e114120); Reset-Folge und W-7 gehören hierher (R3 Lauf 10) |
| Kotlin `verbindung` | `client/verbindung` | b2b-entwurf.md, proto-Kommentare | `AdapterTest` (Connect-Nachbau) | 2026-11-08 (Lauf 9, mit App) | gut; ohne Netz `IOException`; Adapter mit festem Token (R2 Lauf 9) |
| Backup und Test-Client | `server/internal/dienst/schluessel.go`, Backup in `speicher`, `client/testclient`, `schluessel.proto` | b2-entwurf.md, proto-Kommentare | `schluessel_test.go`, `vertrag_test.go`, `EinrichtungTest`, `GoogleTest`, `AblageTest` | 2026-11-08 (Lauf 9, Test-Client) | gut; Meldung nach erfolgtem Löschen (minor) |
| App-Einbindung (App-Repo) | `../avlreminder/app/…/cloud/`, Einstellungen, `gradle/backend-commit.txt`, Composite Build, App-CI | App `docs/backup-entwurf.md` (A-1…A-15), b2b-entwurf.md | `CloudBackupTest` (Attrappen), `CloudBackupUiTest`, App-CI mit Lint; Gerätetest von Hand | 2026-11-09 (Lauf 10, Schritte 3–5, zweiter Blick) | 3 major offen (Löschen ohne Sitzung, Automatik nach abgelehnter Sitzung, andere Geräte beim Löschen); Attrappe ohne Serverregeln (R4) |
| App: Wiederherstellen (App-Repo) | `../avlreminder/app/…/restore/`, Prüfen/Wiederherstellen in `CloudBackup`, `AvlApp.finishRestore`, `RestoreViews` | App `docs/wiederherstellen-entwurf.md` (W-1…W-8, R-1…R-6), design.md 13 | `RestoreFilesTest` (echte DB-Dateien), `LocalRestoreTest`, `CorruptCopyTest` | 2026-11-09 (Lauf 10, neu, zweiter Blick) | critical gefixt (82528b0, App); major offen: Einstellungen blockieren den Auftrag (R1); Room löscht beschädigte DB (R2) |
| Server: Diagnose | `server/internal/{diagnose,kontingent}`, `dienst/diagnose.go`, `diagnose.proto` | diagnose-entwurf.md, formate.md, proto | `vertrag_test.go` (Arbeitsspeicher, Emulatoren), `diagnose_test.go` | 2026-11-05 (Lauf 8, mit Datensicherung) | gut |
| Diagnose: Skript und Kotlin | `werkzeuge/diagnose-holen.sh`, `client/*/Diagnose*`, `ServerDiagnoseAblage`, `diagnose-senden` | diagnose-entwurf.md E-D-6 | `DiagnoseTest`, `AdapterTest`, `TestDiagnoseTest`; Skript nur von Hand | 2026-11-04 (Lauf 7, neu) | gut nach Fixes; App sendet seit 2026-11-08 |
| Kotlin-Build | `client/*.gradle.kts`, `client/gradle/`, CI-Job `kotlin` | b2-entwurf.md (CI) | CI | 2026-11-05 (Lauf 8) | gut; Prüfsummen aller Abhängigkeiten |
| Freischaltung (E-B2b-13) | `server/internal/{freischaltung,dienst/freischaltung.go}`, Freischaltung in `speicher`, `geraete.proto`, Kotlin `Einrichtung` | b2b-entwurf.md, geraete.proto | `freischaltung_test.go`, `vertrag_test.go`, `EinrichtungTest` | 2026-11-08 (Lauf 9) | major offen: zweites Gerät hängt ohne gültigen Schlüssel (Frage 1 Lauf 9); Selbstsperren umgeht 10 offene Geräte (Frage 4) |

## Trends
Das eigentliche Gedächtnis: wiederkehrende Muster, nach denen jeder Lauf gezielt schaut.
- **Test hängt an der geprüften Konstante** (Lauf 2–5). Regel seit 2026-11-02 in
  go-review.md und CLAUDE.md; alter Code nie gezielt durchsucht (E1 Lauf 5).
- **Test kann nicht rot werden / trifft die Grenze nicht** (Lauf 5–7). Bei neuen Tests
  einen Gegenwert wählen.
- **Schutz nur positiv geprüft** (Lauf 7–10). Lauf 10: W-7 ohne „zu weit“-Fall. Bei
  Erlaubnis-Tests die Wirkung prüfen, bei Schutz-Tests auch „zusätzlich zu weit“.
- **Testattrappe bildet Serverregel nicht nach** (Lauf 9, 10): `FalscheAblage`, App
  `FakeServer` (R4 Lauf 10). Gemeinsamer Fake mit Serverregeln empfohlen.
- **Abläufe außerhalb von `protokoll`** (Lauf 4, 5, 8, 10): Einrichtung, Sitzung und
  Rückfragen sind umgezogen; offen W-7 und Reset-Folge in der App (R3 Lauf 10).
- **Drift: `speicher.Speicher` und `dienst` wachsen** (Lauf 4 → 8: 6 → 10 Methoden,
  Freischaltprüfung opt-in je Handler; E2, E10 Lauf 8). Vor B3 handeln.
- **stand.md wird Tagebuch** (Lauf 6, 8, 10). Kürzungsplan in runs/2026-11-09.md.
  Erledigtes steht nur in Berichten und git, nicht in state.md oder stand.md.

## Offen (Details in den Berichten)
- Lauf 10 (runs/2026-11-09.md): Fragen 1–3; R1–R5; minor-Funde zu Löschen, Mutex,
  Wiederherstellen; wackliger Test `aPendingOrFailedRestoreHoldsTheBackup`; Doku kürzen.
- Lauf 9 (runs/2026-11-08.md): R2 (Adapter mit festem Token), R3 (Ausweg „nicht
  freigeschaltet“, in Lauf 10 erneut gesehen), R4 (Schlüssel verwerfen – evtl. durch
  „Dieses Gerät abmelden“ erledigt), R5 (Cloud-Kerntests in App `geraetetest.md`).
- Lauf 8 (runs/2026-11-05.md): E2/E10 (Drift), E3 (Deploy ohne CI-Gate), E4 (Textprüfung
  der Öffnung); Stand der übrigen E-Punkte nicht festgehalten.
- Lauf 6 (runs/2026-11-03-2.md): E4 (30 s bei 8 MiB nicht gemessen), E5 (Skript-Check
  Cloud-Run-Regeln, Leerlauf 0 €), E6 (CLAUDE.md nennt formate.md, b1-entwurf.md nicht).
- Lauf 5 (runs/2026-11-03.md): Testlücken Server (Streaming im Interceptor, Sperr-Rennen
  in `SitzungErneuern`, Speicherfehler im Gerätedienst) – seit Lauf 2.
- Lauf 4 (runs/2026-11-01-4.md): A2–A5 im B3-Entwurf; Ausnahme `FILE_SAME_JAVA_*` in
  `proto/buf.yaml` noch da (nötig?).
- Lauf 1–3: Drossel beim Laden der Google-Schlüssel, Desktop-Client-ID entfernen
  (`infra/variablen.tf`), WARN „Trennen beginnt“, E7 Lauf 1 (Herkunft der Budget-Meldung,
  optional).
- Nicht in der Cloud getestet: Trennen ab 150 %, Geräte- und Tageslimits.

## Offene Fragen an den Entwickler
Lauf 10: runs/2026-11-09.md, Fragen 1–3.
Lauf 9 (bisher nirgends beantwortet; runs/2026-11-08.md):
1. Erstes Backup eines nicht freigeschalteten Geräts: Server prüft „geschützt“ vor der
   Version (`firestore.go`); war „vor Schritt 5“ gewünscht, Schritt 5 ist installiert.
2. Abgelehntes Wiederherstellen einer fremden Fassung lässt das Sichern ruhen – teils
   durch W-7 überholt.
3. Worker: unbeantwortete Rückfrage gilt wie „nein“ – in der App so umgesetzt
   (backup-entwurf.md, Ergänzung zu A-10), im Backend nicht festgehalten.
4. Zusätzlich höchstens 10 neue Geräte je Konto und Tag (vor der Sicherheitsphase)?

## Getroffene Entscheidungen
- 2026-11-01: Architekturbild bestätigt; `.review/` wird versioniert.
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
