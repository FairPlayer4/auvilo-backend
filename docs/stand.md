# Stand und nächste Schritte

Stand: 2026-11-12. Bei jedem größeren Schritt aktualisieren; Erledigtes gehört in git
und die Review-Berichte, nicht hierher.

## In GCP (Projekt `avlreminder-exp-1ef2`, Region `europe-west1`)
Etappe **B0** ist angelegt (OpenTofu, `infra/`):
- APIs, Firestore `(default)` (Native Mode, europe-west1), Artifact Registry
  `container` mit Aufräumregeln
- Budget 20 €/Monat nur für dieses Projekt, Kosten **vor** Guthaben; E-Mail bei
  25/50/90/100 % und bei Prognose 100 %
- **Kill-Switch** (Cloud Function `killswitch`, Go): ab 100 % wird allen
  Cloud-Run-Diensten das öffentliche Aufrufrecht (`allUsers`) entzogen, ab 150 % wird das Projekt vom
  Rechnungskonto getrennt. Er wird per `tofu apply` aus dem **lokalen** Stand gebaut,
  nicht über `main`: nur von einem geprüften, committeten Stand anwenden.
- **Alarm per E-Mail**, wenn der Kill-Switch eingreift (WARNING) oder scheitert
  (ERROR): Log-Alarmregel in Cloud Monitoring (`infra/alarm.tf`), ca. 0,10 $/Monat.
  Empfänger in `infra/werte.auto.tfvars` (`alarm_email`). Getestet am 2026-11-01:
  Mail kam an. Eine neu angelegte Regel braucht einige Minuten, bis sie greift –
  der erste Test 70 s nach dem Anlegen löste keine Mail aus.
- Logs gehen in den Bucket `logs-eu` (europe-west1, 30 Tage); der globale
  `_Default`-Bucket bekommt nichts mehr. `_Required` (Admin-Audit) bleibt global.
- Workload Identity Federation für GitHub Actions (nur Repo
  `FairPlayer4/auvilo-backend` über die Repo-ID, nur Branch `main`; bis 2026-11-23
  `FairPlayer4/avlreminder-backend`), Dienstkonto `deploy` darf
  Images ins Repo `container` hochladen und den Dienst `server` ändern (B1)
Etappe **B1** ist angelegt (2026-11-01, `infra/server.tf`):
- Cloud-Run-Dienst `server` (0–2 Instanzen, 256 MiB, CPU nur bei Anfragen),
  öffentlich aufrufbar, eigenes Dienstkonto `server` (Firestore, nur Secret `pfeffer`).
  Adresse: `tofu output server_url`.
- Secret `pfeffer` (nur europe-west1, feste Version 1, Wert nicht im Zustand).
  Seit 2026-11-07 verwaltet OpenTofu nur das Secret, nicht die Version mit dem
  Wert (Pfeffer-Probe: ein Import läse ihn in den Zustand; Ablauf in
  [gcp-einrichtung.md](gcp-einrichtung.md)). Angewendet: 1 vergessen, sonst nichts.
  Schutz auch außerhalb von OpenTofu: Zerstörte Versionen bleiben 30 Tage
  wiederherstellbar (`version_destroy_ttl`, angewendet 2026-11-01). Das gilt für
  einzelne Versionen; gegen das Löschen des ganzen Secrets (Console, `gcloud
  secrets delete`) schützt es nach unserem Verständnis nicht, `prevent_destroy`
  wirkt nur in OpenTofu.
- Firestore-TTL auf `sitzungen.laeuft_ab` (aktiv)
- Deploy aus GitHub Actions (`.github/workflows/deploy.yml`) bei Merge nach `main`,
  wenn sich `server/**` oder der Workflow ändert (sonst von Hand starten). Beim
  Anlegen lief ein Platzhalter-Image von Google, das erste echte Image kam per
  Handstart des Workflows. Konto `deploy` darf Images ins Repo `container`
  schreiben und nur den Dienst `server` ändern.
- Repo `funktionen` für das Kill-Switch-Image (E5); Kill-Switch mit eigenem Zeitlimit
  fürs Sperren vor dem Trennen (E8) neu deployt.
Etappe **B2b** ist angelegt (2026-11-03 bis 11-10):
- Bucket `…-datensicherung` (`infra/datensicherung.tf`: versioniert, ältere Fassungen
  7 Tage und höchstens 200, ohne Soft Delete, nur das Server-Konto schreibt),
  Bucket `…-diagnose` (`infra/diagnose.tf`, 14 Tage, Server darf nur anlegen),
  Firestore-TTL der Tageszähler, `GOMEMLIMIT=192MiB` am Server.
- Egress-Alarm (`infra/alarm.tf`: Mail bei mehr als 1 GiB in 23 Stunden) und
  Absenz-Alarm („Kill-Switch wird nicht mehr aufgerufen“, 23,5 h).
- Deploy nur nach grüner CI auf `main` (WIF-Bedingung auf `deploy.yml`, seit 2026-11-10).
  Die CI läuft auf `develop`, `main` und bei PRs (ohne reine Doku-Änderungen) und ist
  seit 2026-11-24 die maßgebliche Prüfung, mit actionlint und Image-Test;
  `werkzeuge/pruefen.sh` ist nur noch Werkzeug.
- Zustand von OpenTofu: Bucket `avlreminder-exp-1ef2-tfstate` (europe-west1,
  versioniert), angelegt per `gcloud`

## Geprüft
- `tofu plan` ohne Abweichungen; kein Konto im Projekt hat mehr die Editor-Rolle
- Kill-Switch in der Cloud: Meldung 3 €/20 € → nichts; 22 €/20 € → Testdienst
  gesperrt (Log als WARNING in `logs-eu`), eigene Funktion nicht gesperrt.
  Testdienst danach gelöscht.
- Trennen vom Rechnungskonto (ab 150 %) am 2026-11-10 in einem Wegwerfprojekt
  getestet (gleicher Code, gleiche Rechte, eigene OpenTofu-Konfiguration, danach
  gelöscht): Meldung 30 €/20 € → nach etwa 40 s getrennt, Log `abrechnungGetrennt: true`.
  Beobachtet: Direkt nach dem Anlegen lehnte Cloud Run die Zustellungen gut eine Minute
  lang ab (Aufrufrecht noch nicht wirksam); Eventarc stellte erneut zu. Betrifft nur
  einen Neuaufbau des Projekts.
- **Bekannte Grenze:** Ab 100 % sperrt der Kill-Switch nur Cloud-Run-**Dienste** in
  `var.region`. Cloud-Run-Jobs und Dienste in anderen Regionen bleiben offen; dafür
  greift erst das Trennen ab 150 %. Kommen solche dazu, den Kill-Switch erweitern.
  Ebenso kostet Speicher (Bucket der Datensicherungen) nach der Sperre weiter, bis
  zum Trennen; die Lebenszyklusregeln greifen erst verzögert. Das Hochladen
  begrenzt der Server auf 48 Sicherungen je Konto und Tag
  ([b2b-entwurf.md](b2b-entwurf.md), E-B2b-12); Holen auf 50 und Löschen auf 20
  je Konto und Tag (seit 2026-11-06), dazu höchstens 4 gleichzeitige Aufrufe und
  der Egress-Alarm (1 GiB in 23 Stunden). Die Grenze von 4 gilt je Instanz; mit
  bis zu 2 Instanzen laufen also höchstens 8 Sicherungen gleichzeitig (entschieden
  2026-11-07).
- **Bekannte Grenze (Absenz-Alarm):** schlägt erst an, nachdem der Kill-Switch
  einmal aufgerufen wurde. Nach einem Neuaufbau einmal eine Meldung unter der
  Schwelle schicken (`gcloud pubsub topics publish budget-meldungen
  --message='{"costAmount":1,"budgetAmount":20}'`) und den Aufruf im Log prüfen.
- **Bekannte Grenze (Deploy-Konto):** Es darf den Dienst `server` ändern
  (`roles/run.developer`) und könnte damit technisch mehr als das Image setzen:
  den Ingress, Mindestinstanzen, CPU
  dauerhaft, bis zu 100 Instanzen, oder den Dienst löschen. Mindestinstanzen
  kosten auch nach dem Sperren weiter, bis ab 150 % getrennt wird. Der Deploy
  selbst ändert nur das Image; `tofu apply` setzt alles außer dem Image zurück.
  Einen vom Kill-Switch gesperrten Dienst kann es nicht öffnen (dafür fehlt
  `setIamPolicy`). Voraussetzung wäre fremder Code auf `main`.
- **Kill-Switch ohne actAs** (seit 2026-11-05): Er sperrt über die IAM-Richtlinie
  (`allUsers` entfernen) und darf nur Dienste auflisten und deren Richtlinie
  lesen und setzen. Er kann keinen Dienst ändern und kein Image starten.
  **Grenze:** Kompromittierter Code im Kill-Switch könnte Dienste sperren oder
  Rechte auf Dienste vergeben, auch sich selbst (z. B. `run.admin`, dann könnte
  er Dienste löschen). Ein fremdes Image starten kann er auch so nicht (kein
  actAs); die Dienste prüfen die Anmeldung ohnehin selbst.
  **In der Cloud getestet** am 2026-11-06 (Meldung 21/20, Testdienst `kstest`
  mit Zusatzbindung): `allUsers` an `kstest` und `server` nach ca. 10 s entfernt,
  die anderen Bindungen (`deploy` an beiden) blieben, `killswitch` selbst nicht
  gesperrt, Log-WARNING „2 gefunden, 2 gesperrt“, nicht getrennt, Mail kam an.
  `kstest` antwortete sofort mit 403, `server` erst nach ca. 2 Minuten
  (IAM-Verzögerung). `tofu plan` brach danach wie gewollt ab; mit
  `-var=gesperrte_dienste_oeffnen=true` kam nur die Bindung `allUsers` zurück,
  danach `tofu plan` ohne Änderungen und Testclient-Aufrufe wieder möglich.
- **Löschschutz** (seit 2026-11-05, vor echten Daten): Firestore, der Dienst
  `server` und der Bucket der Datensicherungen lassen sich nicht mit `tofu destroy`
  löschen. In GCP selbst schützt nur Firestore (`delete_protection_state`): Den
  Dienst (ohne Daten) und den Bucket (mit den Sicherungen, ohne Soft Delete, also
  endgültig) kann ein Owner in der Konsole oder mit `gcloud` weiter löschen. Das
  Projekt zu löschen geht weiterhin (Q7).
- Go-Tests: Entscheidung, Ausführung, Verarbeitung und Sperr-Logik (`sperren.go`)
  sind getestet. `gcp.go` enthält nur noch die Cloud-Aufrufe und wird durch den
  Test in der Cloud geprüft.
- `tofu test` (simulierte Provider) prüft, dass alle Standorte in der EU liegen.
- „Vollständig löschbar“ (Q7) heißt: **Projekt löschen**. `tofu destroy` allein lässt
  Reste, die Google selbst anlegt (z. B. Bucket `gcf-v2-sources-…`, Standard-Dienstkonten),
  und der Log-Bucket `logs-eu` bleibt nach dem Löschen 7 Tage reserviert.

## Etappen
Einzelheiten stehen in den Entwürfen, in git und in den Review-Berichten
(`.review/runs/`); hier nur der Stand.

| Etappe | Inhalt | Stand |
|---|---|---|
| B1 | Go-Server auf Cloud Run, Google-Login ([b1-entwurf.md](b1-entwurf.md)) | fertig, in der Cloud getestet (2026-11-01) |
| B2 | Schlüsselmodell, Schlüssel-Backup, Kotlin-Client ([b2-entwurf.md](b2-entwurf.md)) | fertig, in der Cloud getestet (2026-11-02) |
| B2b | Daten-Backup einer Person: Datensicherung in Cloud Storage, Freischaltung neuer Geräte (E-B2b-13), Tageslimits, Fassungen (R-4), Diagnose-Log ([b2b-entwurf.md](b2b-entwurf.md), [diagnose-entwurf.md](diagnose-entwurf.md)) | Server und Client fertig, zuletzt in der Cloud getestet am 2026-11-10 (Limits, Fassungen, Trennen ab 150 %); App-Einbindung: App-Repo `docs/stand.md` |
| B3 | Teilen zwischen Personen | nicht begonnen; Vorgaben in [anforderungen.md](anforderungen.md) Kap. 4.6; davor die Sicherheitsphase |

Letzter Deploy: Probe-Deploy von Hand aus dem neuen Repo `auvilo-backend` am 2026-11-23
(gleicher Code wie PR #27), Revision `server-00026-6j4`, bereit; der Job `aufraeumen` hat
dasselbe Image; danach `tofu plan` ohne Änderungen. Davor: PR #27 am 2026-11-19 (Review Lauf 11), Revision `server-00025-gh4`,
bereit; der Job `aufraeumen` hat dasselbe Image, ein Lauf von Hand endete mit
„Aufräumen beendet“ (`verwaist_geloescht=0`).

## Offen
1. **Eigenbetrieb** seit 2026-11-07: Die App sichert Kevins echte Daten in der Cloud;
   Claude darf die Schlüssel dafür kennen. Vor dem ersten weiteren Nutzer beginnt die
   Sicherheitsphase (Checkliste „Vor dem ersten weiteren Nutzer“ in b2b-entwurf.md;
   `CLAUDE.md`). Die Desktop-Client-ID des Test-Clients bleibt bis dahin.
   **Vorher fertig** (Sicherheits- und Datenschutzprüfung 2026-11-12, Liste dort):
   Konto löschen, Bestätigung mit der Displaysperre (beide in der App V1.3c2, noch
   nicht installiert), Format v2 mit HKDF-Labels (in `protokoll` seit 2026-11-23, App
   bekommt es mit dem nächsten backend-commit),
   A-15 neu in der App (App V1.3c3, 2026-11-15, noch nicht installiert), Dokumente. **Konto löschen:** Server und Client fertig
   (2026-11-12). Die Infrastruktur steht in `infra/aufraeumen.tf`
   (Cloud Run Job `aufraeumen`, täglich 4 Uhr per Cloud Scheduler, Rolle
   „Objekte auflisten und löschen“ ohne Lesen, Alarm), angewendet und deployt am
   2026-11-13. **In der Cloud getestet am 2026-11-13** mit dem Test-Client und dem
   alten Testkonto (Kevins GCP-Google-Konto, keine echten Daten): Antrag von einem
   nicht freigeschalteten Gerät (Termin +7 Tage, sichtbar in Erneuern und
   Geräteliste), Abbrechen nur vom freigeschalteten Gerät, nach Fälligkeit
   abgelehnt (Termin dafür in Firestore vorgezogen); der Job hat das Konto
   gelöscht (Datensicherung mit Fassungen, Diagnosen, Konto, Schlüssel-Backup,
   17 Geräte, Sitzungen, Kennung; Gegenprobe der Abfragen an einem anderen Konto).
   Sofort löschen von einem freigeschalteten Gerät eines neuen Kontos: alles weg,
   der Test-Client hat das Gerät vergessen. Die lokalen Dateien der alten
   Testgeräte liegen in `%APPDATA%\avlreminder\testclient\alt-konto-8b07`.
   In der App umgesetzt in V1.3c2 (App-Branch `feature/konto-loeschen`, 2026-11-14), noch nicht installiert ([datenschutz/README.md](datenschutz/README.md) K1). Ergebnis der Prüfung: Nach der Rotation ist die
   Sicherung für eine Person sicher; für die DSGVO fehlen vor allem Konto löschen
   und Fristen ([anforderungen.md](anforderungen.md) Kap. 4.4, 4.5, 11).
   **DSGVO-Dokumente** (Entwürfe, 2026-11-14) und die Liste, was noch fehlt:
   [datenschutz/README.md](datenschutz/README.md). K2 ist **umgesetzt und
   deployt (2026-11-14, PR #26):** `zuletzt_aktiv`, Löschung nach 24 Monaten ohne
   Anmeldung, Grabsteine für Geräte nach 90 Tagen Sperre im Lauf `aufraeumen`.
   Erster Lauf von Hand nach dem Deploy grün („Aufräumen beendet“, alle Zähler 0,
   wie erwartet: noch kein Gerät 90 Tage gesperrt, kein Konto 24 Monate alt).
2. **App-Einbindung** (Schritte 1–5, Wiederherstellen R-1…R-6): App-Repo
   `docs/stand.md`; dort auch die Gerätetests.
3. **Review-Punkte:** `.review/state.md`, Abschnitt „Offen“.
4. **B3:** Vorgaben B3-1 bis B3-8 (anforderungen.md Kap. 4.6), offene Fragen in Kap. 10.
5. **Vor der Veröffentlichung des App-Repos** (App-Anf. 7.34, 2026-11-14): Zugang nur
   nach Freigabe über Kennungen (ID7) und Kostengrenzen gegen Angriffe (ID8) sind
   **umgesetzt und deployt (2026-11-14, PR #25)**. Freigegeben sind App- und
   Test-Konto; der Test-Client wurde vorher wie erwartet abgelehnt und meldet sich danach an. Die App
   zeigt die Kennung bei „nicht freigegeben“ an. Die Server-URL bleibt
   beim Zurücksetzen dieselbe. **Umzug 2026-11-23:** Backend unverändert in das neue Repo
   `FairPlayer4/auvilo-backend`, App mit bereinigter Historie nach `FairPlayer4/auvilo`
   (App-Anf. 7.34 OV5); CI wieder auf `develop` und bei PRs (OV6). Die alten Repos
   bleiben privat als Archiv.
6. **Review Lauf 11 umgesetzt und deployt (2026-11-19, PR #27):**
   Konto löschen meldet gelöscht, sobald der Zugang weg ist, und der Client prüft
   nach einem Fehler per Erneuern; die Kennung kommt nicht mehr ins Log; der Lauf
   löscht Sicherungen und Schlüssel-Backups ohne Konto (älter als 1 h). Der
   Anfragealarm meldet sich alle 30 Minuten erneut beim Kill-Switch
   (`renotify_interval`, angewendet 2026-11-19). **Pfad Anfragealarm →
   Kill-Switch in der Cloud getestet am 2026-11-19:** eine vorübergehende
   Richtlinie wie `anfragen`, aber mit Schwelle 5 (eigene OpenTofu-Wurzel mit
   lokalem Zustand, danach zerstört); 10 Anfragen an den Server um 13:41 UTC,
   um 13:45:26 „Alarm-Meldung verarbeitet“ (WARNING, Aktion „Dienste sperren“,
   1 Dienst, Richtlinie genannt), ab 13:47 antwortete der Server mit 403. Mit
   `-var=gesperrte_dienste_oeffnen=true` kam nur `allUsers` zurück, ab 13:48
   wieder offen, danach `tofu plan` ohne Änderungen. Vom Alarm bis zur Sperre
   etwa 4 Minuten, bis 403 noch 1–2 Minuten. Die App gibt `ServerKontoLoeschen`
   die Anmeldung mit (App-Repo, 2026-11-19).

## Befehle
```bash
cd infra && tofu plan
```

```bash
cd infra/killswitch && go test -cover ./...
```

Server lokal gegen Firestore-Emulator und fake-gcs-server (beide vorher starten,
Bucket `lokal` anlegen, siehe [entwicklungsumgebung.md](entwicklungsumgebung.md));
`PFEFFER` ist ein Zufallswert nur für diesen Lauf. Für `diagnose-senden` zusätzlich
`DIAGNOSE_KONTEN=<eigene Konto-ID>` setzen, sonst ist niemand freigeschaltet.
`FREIGEGEBENE_KENNUNGEN='*'` lässt lokal jedes Google-Konto zu (leer = niemand, ID7):

```bash
cd server && FIRESTORE_EMULATOR_HOST=localhost:8086 STORAGE_EMULATOR_HOST=localhost:4443 SICHERUNG_BUCKET=lokal DIAGNOSE_BUCKET=lokal PROJEKT=lokal GOOGLE_CLIENT_IDS=82416816442-vka4ve2no02681umqkn8b0rk37ri37qd.apps.googleusercontent.com FREIGEGEBENE_KENNUNGEN='*' PFEFFER=$(openssl rand -base64 32) go run ./cmd/server
```

Test-Client (Kotlin, B2) bauen, dann als Gerät anmelden und Schlüssel einrichten
bzw. aus dem Backup holen (weitere Befehle mit `--hilfe`):

```bash
cd client && ./gradlew :testclient:installDist
```

```bash
./client/testclient/build/install/testclient/bin/testclient --geraet pc anmelden
```

Gegen den Server in der Cloud (Adresse aus `tofu output server_url`; Gerätenamen
nicht mit lokalen Tests mischen, die Konten unterscheiden sich):

```bash
./client/testclient/build/install/testclient/bin/testclient --server "$(cd infra && tofu output -raw server_url)" --geraet cloud-pc anmelden
```

Kill-Switch von Hand auslösen (sperrt alle Cloud-Run-Dienste!):

```bash
gcloud pubsub topics publish budget-meldungen --message='{"costAmount":21,"budgetAmount":20}'
```

Nach einer Sperre wieder öffnen (erst, wenn die Ursache geklärt ist; ohne die
Variable bricht `tofu plan` mit „gesperrt“ ab):

```bash
tofu -chdir=infra apply -var=gesperrte_dienste_oeffnen=true
```

Die Variable öffnet **alle** gesperrten Dienste. Soll während der Sperre etwas
anderes geändert werden und der Dienst zu bleiben, nur die betroffenen
Ressourcen anwenden, z. B.:

```bash
tofu -chdir=infra apply -target=google_cloud_run_v2_service.server
```

Logs lesen (liegen im EU-Bucket, nicht im Standard-Bucket):

```bash
gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=europe-west1 --view=_AllLogs --limit=10
```
