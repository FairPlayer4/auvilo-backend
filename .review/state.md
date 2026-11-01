# Review-Gedächtnis
Letzter Lauf: 2026-11-01 @ 44b2399 (Fixes auf review/2026-11-01)   Laufzähler: 1

## Beabsichtigte Architektur
Vom Entwickler bestätigt am 2026-11-01.

- **Monorepo:** `proto/` (Schnittstelle, Protobuf/Connect mit buf), `server/` (Go),
  `client/` (Kotlin: Protokoll-Modul und Test-Client), `infra/` (OpenTofu und
  Hilfsfunktionen wie `killswitch`), `docs/`. Stand Lauf 1: nur `infra/` und `docs/`.
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

## Bereiche
| Bereich | Pfade | Anforderungen/Doku | Tests | Zuletzt reviewt | Zustand |
|---|---|---|---|---|---|
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, stand.md | `*_test.go` | 2026-11-01 (Lauf 1) | gut (E1 erledigt) |
| Infrastruktur | `infra/*.tf` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md | `infra/tests/` (tofu test) | 2026-11-01 (Lauf 1) | gut (E2, E3, E6 erledigt) |
| CI und Werkzeuge | `.github/workflows/`, `.golangci.yml` | anforderungen.md Q4, go-review.md | – | 2026-11-01 (Lauf 1) | ok; Modul-Matrix fehlt (E4) |
| Doku | `docs/`, `CLAUDE.md`, `README.md` | – | – | nie | – |

## Bekannte Schulden und beobachtete Drift
- 2026-11-01: Kein Alarm bei Fehlern des Kill-Switch (E9).
- 2026-11-01: Offene kleinere Empfehlungen aus Lauf 1: E4 (Modul-Matrix in der CI,
  spätestens mit `server/`), E5 (eigenes Registry-Repo für Funktionen), E7 (Herkunft
  der Budget-Meldung prüfen), E8 (Zeitlimit beim Trennen), E10 (Actions-Pflege).
- Erledigt nach Lauf 1 (in develop): E1 Sperr-Logik testbar (`sperren.go`), E2 Rechte
  verkleinert, E3 D1-Test in der CI, E6 Build-Konto ohne Builder-Rolle.

## Offene Fragen an den Entwickler
Keine.

## Getroffene Entscheidungen
- 2026-11-01: Architekturbild bestätigt.
- 2026-11-01: `.review/` wird im Repository versioniert.
- 2026-11-01: Cloud-Änderungen aus Lauf 1 anwenden: ja.
- 2026-11-01: Editor-Rolle der Standardkonten entfernen und `serviceAccountUser`
  einschränken: ja.
- 2026-11-01: Keine Pflicht-Statuschecks (GitHub Free, privat) – Konvention reicht, kein
  produktiver Einsatz. Nicht erneut als Fund melden.
- 2026-11-01: Kosten vor Verfügbarkeit: `tofu apply` darf gesperrte Dienste nicht wieder
  öffnen (`ignore_changes = [ingress]`). Hohe Kosten gelten als Zeichen für Fehler/Angriff.
- 2026-11-01: `EXCLUDE_ALL_CREDITS` bleibt (auch Gratis-Kontingent zählt) – erlaubt einen
  späteren Live-Test des Kill-Switch. Nicht erneut als Fund melden.
