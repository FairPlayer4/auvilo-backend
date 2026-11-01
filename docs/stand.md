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
- Logs gehen in den Bucket `logs-eu` (europe-west1, 30 Tage); der globale
  `_Default`-Bucket bekommt nichts mehr. `_Required` (Admin-Audit) bleibt global.
- Workload Identity Federation für GitHub Actions (nur Repo
  `FairPlayer4/avlreminder-backend`, nur Branch `main`), Dienstkonto `deploy` darf
  vorerst nur Images hochladen
- Zustand von OpenTofu: Bucket `avlreminder-exp-1ef2-tfstate` (europe-west1,
  versioniert), angelegt per `gcloud`

## Geprüft
- `tofu plan` ohne Abweichungen
- Kill-Switch in der Cloud: Meldung 3 €/20 € → nichts; 22 €/20 € → Testdienst
  gesperrt (Log als WARNING in `logs-eu`), eigene Funktion nicht gesperrt.
  Testdienst danach gelöscht.
- **Nicht** in der Cloud getestet: Trennen vom Rechnungskonto (ab 150 %). Nur
  Unit-Tests; ein echter Test würde das Projekt abschalten.
- Go-Tests: Entscheidung, Ausführung und Verarbeitung sind getestet. **Nicht**
  getestet ist `gcp.go`, und dort steckt eigene Logik (eigenen Dienst auslassen,
  schon interne überspringen, bei Fehlern weitermachen). Geprüft nur durch den
  Test in der Cloud; Empfehlung aus dem Review: Logik herausziehen und testen
  (siehe `.review/`).
- „Vollständig löschbar“ (Q7) heißt: **Projekt löschen**. `tofu destroy` allein lässt
  Reste, die Google selbst anlegt (z. B. Bucket `gcf-v2-sources-…`, Standard-Dienstkonten),
  und der Log-Bucket `logs-eu` bleibt nach dem Löschen 7 Tage reserviert.

## Offen
1. **Kevin prüft** den Stand, danach Commit und Push (CI läuft erst dann).
2. Schwellen 100 %/150 % sind so umgesetzt wie empfohlen; Kevin bestätigt sie oder
   ändert sie (`schwelle_sperren`, `schwelle_trennen` in `infra/variablen.tf`).
3. Etappe **B1**: Go-Server auf Cloud Run, Google-Login (Zustimmungsbildschirm und
   OAuth-Client richtet Kevin nach [gcp-einrichtung.md](gcp-einrichtung.md) ein).

## Befehle
```bash
cd infra && tofu plan
```

```bash
cd infra/killswitch && go test -cover ./...
```

Kill-Switch von Hand auslösen (sperrt alle Cloud-Run-Dienste!):

```bash
gcloud pubsub topics publish budget-meldungen --message='{"costAmount":21,"budgetAmount":20}'
```

Logs lesen (liegen im EU-Bucket, nicht im Standard-Bucket):

```bash
gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=europe-west1 --view=_AllLogs --limit=10
```
