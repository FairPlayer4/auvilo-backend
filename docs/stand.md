# Stand und nächste Schritte

Stand: 2026-11-01. Bei jedem größeren Schritt aktualisieren.

## In GCP (Projekt `avlreminder-exp-1ef2`, Region `europe-west1`)
Etappe **B0** ist angelegt (OpenTofu, `infra/`):
- APIs, Firestore `(default)` (Native Mode, europe-west1), Artifact Registry
  `container` mit Aufräumregeln
- Budget 20 €/Monat nur für dieses Projekt, Kosten **vor** Guthaben; E-Mail bei
  25/50/90/100 % und bei Prognose 100 %
- **Kill-Switch** (Cloud Function `killswitch`, Go): ab 100 % werden alle
  Cloud-Run-Dienste auf „nur intern“ gestellt, ab 150 % wird das Projekt vom
  Rechnungskonto getrennt
- **Alarm per E-Mail**, wenn der Kill-Switch eingreift (WARNING) oder scheitert
  (ERROR): Log-Alarmregel in Cloud Monitoring (`infra/alarm.tf`), ca. 0,10 $/Monat.
  Empfänger in `infra/werte.auto.tfvars` (`alarm_email`). Getestet am 2026-11-01:
  Mail kam an. Eine neu angelegte Regel braucht einige Minuten, bis sie greift –
  der erste Test 70 s nach dem Anlegen löste keine Mail aus.
- Logs gehen in den Bucket `logs-eu` (europe-west1, 30 Tage); der globale
  `_Default`-Bucket bekommt nichts mehr. `_Required` (Admin-Audit) bleibt global.
- Workload Identity Federation für GitHub Actions (nur Repo
  `FairPlayer4/avlreminder-backend`, nur Branch `main`), Dienstkonto `deploy` darf
  Images ins Repo `container` hochladen und den Dienst `server` ändern (B1)
Etappe **B1** ist angelegt (2026-11-01, `infra/server.tf`):
- Cloud-Run-Dienst `server` (0–2 Instanzen, 256 MiB, CPU nur bei Anfragen),
  öffentlich aufrufbar, eigenes Dienstkonto `server` (Firestore, nur Secret `pfeffer`).
  Adresse: `tofu output server_url`.
- Secret `pfeffer` (nur europe-west1, feste Version 1, Wert nicht im Zustand).
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
- Zustand von OpenTofu: Bucket `avlreminder-exp-1ef2-tfstate` (europe-west1,
  versioniert), angelegt per `gcloud`

## Geprüft
- `tofu plan` ohne Abweichungen; kein Konto im Projekt hat mehr die Editor-Rolle
- Kill-Switch in der Cloud: Meldung 3 €/20 € → nichts; 22 €/20 € → Testdienst
  gesperrt (Log als WARNING in `logs-eu`), eigene Funktion nicht gesperrt.
  Testdienst danach gelöscht.
- **Nicht** in der Cloud getestet: Trennen vom Rechnungskonto (ab 150 %). Nur
  Unit-Tests; ein echter Test würde das Projekt abschalten.
- **Bekannte Grenze:** Ab 100 % sperrt der Kill-Switch nur Cloud-Run-**Dienste** in
  `var.region`. Cloud-Run-Jobs und Dienste in anderen Regionen bleiben offen; dafür
  greift erst das Trennen ab 150 %. Kommen solche dazu, den Kill-Switch erweitern.
- **Bekannte Grenze (Deploy-Konto):** Es darf den Dienst `server` ändern
  (`roles/run.developer`) und könnte damit technisch mehr als das Image setzen:
  den vom Kill-Switch gesperrten Ingress wieder öffnen, Mindestinstanzen, CPU
  dauerhaft, bis zu 100 Instanzen, oder den Dienst löschen. Mindestinstanzen
  kosten auch nach dem Sperren weiter, bis ab 150 % getrennt wird. Der Deploy
  selbst ändert nur das Image; `tofu apply` setzt alles außer Image und Ingress
  zurück. Voraussetzung wäre fremder Code auf `main`.
- **Bekannte Grenze (Kill-Switch):** Er darf Dienste ändern und dabei die
  eingetragenen Laufzeitkonten benutzen (actAs, nötig fürs Sperren). Eine
  kompromittierte Abhängigkeit im Kill-Switch könnte so ein fremdes Image als
  Konto `server` starten (Firestore, Pfeffer). Vor echten Daten (B6) prüfen, ob
  das Sperren ohne actAs geht (Review Lauf 3).
- Go-Tests: Entscheidung, Ausführung, Verarbeitung und Sperr-Logik (`sperren.go`)
  sind getestet. `gcp.go` enthält nur noch die Cloud-Aufrufe und wird durch den
  Test in der Cloud geprüft.
- `tofu test` (simulierte Provider) prüft, dass alle Standorte in der EU liegen.
- „Vollständig löschbar“ (Q7) heißt: **Projekt löschen**. `tofu destroy` allein lässt
  Reste, die Google selbst anlegt (z. B. Bucket `gcf-v2-sources-…`, Standard-Dienstkonten),
  und der Log-Bucket `logs-eu` bleibt nach dem Löschen 7 Tage reserviert.

## Review
- Lauf 1 am 2026-11-01: [.review/runs/2026-11-01.md](../.review/runs/2026-11-01.md).
  Fixes und die Empfehlungen E1, E2, E3, E6 sind in develop und in GCP angewendet.
- Kill-Switch nach dem Verkleinern der Rechte erneut in der Cloud getestet
  (Testdienst mit Standardkonto nach ca. 10 s gesperrt, danach gelöscht).

## Offen
1. Etappe **B1** (begonnen 2026-11-01): Go-Server auf Cloud Run, Google-Login.
   Entwurf: [b1-entwurf.md](b1-entwurf.md), bestätigt. **Lokal fertig:** Schnittstelle
   (`proto/`), Server (`server/`) mit Tests gegen Speicher im Arbeitsspeicher und
   Firestore-Emulator, Werkzeug `testanmeldung`, CI (Go-Module als Matrix = E4, buf).
   Am 2026-11-01 lokal von Ende zu Ende getestet: echte Google-Anmeldung im Browser,
   Erneuern, Auflisten, eigenes Gerät sperren (danach Sitzung ungültig, Erneuern
   abgelehnt).
   Infrastruktur am 2026-11-01 angelegt (siehe oben), erster Deploy aus GitHub
   Actions am selben Tag. **In der Cloud getestet** (Gerät `cloud`): echte
   Google-Anmeldung, Erneuern, Auflisten, eigenes Gerät sperren (danach Sitzung
   ungültig, Erneuern abgelehnt). Logs enthalten Geräte-ID und Grund, kein Token.
   Damit ist B1 abgeschlossen.
2. Etappe **B2** (begonnen 2026-11-01): Schlüsselmodell und Schlüssel-Backup.
   Entwurf: [b2-entwurf.md](b2-entwurf.md), bestätigt. **Lokal fertig:**
   Kotlin-Client `client/` (Gradle) mit Modul `protokoll` (Tink: Personen-, Tresor-
   und Wiederherstellungsschlüssel, Backup, Umschlag, Signatur) und Test-Client
   `testclient`; gemeinsame Testvektoren mit Go (`proto/testvektoren`); Server-Dienst
   `SchluesselDienst` (Firestore-Sammlung `backups`, Version per Transaktion);
   CI-Job „Kotlin – client“, Dependabot für Gradle.
   Am 2026-11-01 lokal von Ende zu Ende getestet (echte Google-Anmeldung): Gerät
   `pc` richtet neue Schlüssel ein; Gerät `tablet` bricht nach drei falschen
   Eingaben ab, ohne etwas zu speichern; Gerät `handy` übernimmt mit dem richtigen
   Wiederherstellungsschlüssel das Backup (gleicher Fingerabdruck wie `pc`).
   **Offen:** Deploy und Test in der Cloud, Review-Lauf 4. Danach kann
   `testanmeldung` (Go) weg.
3. Kleinere Empfehlungen aus dem Review: E7 optional; weitere aus Lauf 2 in
   `.review/state.md`.

## Befehle
```bash
cd infra && tofu plan
```

```bash
cd infra/killswitch && go test -cover ./...
```

Server lokal gegen den Firestore-Emulator (Emulator vorher starten, siehe
[entwicklungsumgebung.md](entwicklungsumgebung.md)); `PFEFFER` ist ein Zufallswert
nur für diesen Lauf:

```bash
cd server && FIRESTORE_EMULATOR_HOST=localhost:8086 PROJEKT=lokal GOOGLE_CLIENT_IDS=82416816442-vka4ve2no02681umqkn8b0rk37ri37qd.apps.googleusercontent.com PFEFFER=$(openssl rand -base64 32) go run ./cmd/server
```

Als Gerät anmelden (öffnet den Browser), zweites Gerät, Geräte auflisten:

```bash
cd server && go run ./cmd/testanmeldung anmelden
```

```bash
cd server && go run ./cmd/testanmeldung -geraet handy anmelden
```

```bash
cd server && go run ./cmd/testanmeldung geraete
```

Gegen den Server in der Cloud (Adresse aus `tofu output server_url`):

```bash
cd server && go run ./cmd/testanmeldung -server "$(cd ../infra && tofu output -raw server_url)" -geraet cloud anmelden
```

Test-Client (Kotlin, B2) bauen, dann als Gerät anmelden und Schlüssel einrichten
bzw. aus dem Backup holen (weitere Befehle mit `--hilfe`):

```bash
cd client && ./gradlew :testclient:installDist
```

```bash
./client/testclient/build/install/testclient/bin/testclient --geraet pc anmelden
```

Kill-Switch von Hand auslösen (sperrt alle Cloud-Run-Dienste!):

```bash
gcloud pubsub topics publish budget-meldungen --message='{"costAmount":21,"budgetAmount":20}'
```

Logs lesen (liegen im EU-Bucket, nicht im Standard-Bucket):

```bash
gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=europe-west1 --view=_AllLogs --limit=10
```
