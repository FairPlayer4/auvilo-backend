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
- `tofu plan`: bis auf Punkt 1 unter „Offen“ ohne Abweichungen
- Kill-Switch in der Cloud: Meldung 3 €/20 € → nichts; 22 €/20 € → Testdienst
  gesperrt (Log als WARNING in `logs-eu`), eigene Funktion nicht gesperrt.
  Testdienst danach gelöscht.
- **Nicht** in der Cloud getestet: Trennen vom Rechnungskonto (ab 150 %). Nur
  Unit-Tests; ein echter Test würde das Projekt abschalten.
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
1. **Ein Rest aus `tofu apply`**: Die Editor-Rolle von `…@appspot.gserviceaccount.com`
   ist noch nicht entfernt (409 „concurrent policy changes“ beim ersten Lauf).
   `tofu plan` zeigt genau diese eine Änderung; mit `tofu apply` nachholen.
2. Push von develop (11+ Commits), danach läuft die CI zum ersten Mal.
3. Schwellen 100 %/150 % bestätigen oder ändern (`schwelle_sperren`,
   `schwelle_trennen` in `infra/variablen.tf`).
4. Kleinere Empfehlungen aus dem Review: E4, E5, E7–E10 (siehe `.review/state.md`).
5. Etappe **B1**: Go-Server auf Cloud Run, Google-Login (Zustimmungsbildschirm und
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
