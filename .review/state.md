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
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, stand.md | `*_test.go` (54) | 2026-11-01 (Lauf 1) | gut; `gcp.go` ungetestet (E1) |
| Infrastruktur | `infra/*.tf` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md | keine (E3) | 2026-11-01 (Lauf 1) | IAM zu breit (E2) |
| CI und Werkzeuge | `.github/workflows/`, `.golangci.yml` | anforderungen.md Q4, go-review.md | – | 2026-11-01 (Lauf 1) | ok; Modul-Matrix fehlt (E4) |
| Doku | `docs/`, `CLAUDE.md`, `README.md` | – | – | nie | – |

## Bekannte Schulden und beobachtete Drift
- 2026-11-01: `gcp.go` enthält Logik ohne Tests (E1). Erstbeobachtung.
- 2026-11-01: Least Privilege nicht eingehalten: Kill-Switch hat `run.developer` und
  projektweit `serviceAccountUser`, Build-Konto hat die Builder-Rolle, Standardkonten
  haben Editor (E2, E6). Erstbeobachtung.
- 2026-11-01: D1-Absicherung nur über eine Variablen-Validierung, kein Plan-Test (E3).
- 2026-11-01: Kein Alarm bei Fehlern des Kill-Switch (E9).
- 2026-11-01: Fixes aus Lauf 1 an Funktion, Thema und Bucket sind noch nicht per
  `tofu apply` angewendet.

## Offene Fragen an den Entwickler
1. Cloud-Änderungen aus Lauf 1 anwenden (nach Merge)?
2. Editor-Rolle der Standardkonten entfernen, `serviceAccountUser` einschränken?
3. Pflicht-Statuschecks: GitHub Free privat → nur Konvention. Akzeptiert?
4. `tofu apply` öffnet vom Kill-Switch gesperrte Dienste wieder (ab B1) – gewollt?
5. `EXCLUDE_ALL_CREDITS` zählt auch Gratis-Kontingent mit – so lassen?

## Getroffene Entscheidungen
- 2026-11-01: Architekturbild bestätigt.
- 2026-11-01: `.review/` wird im Repository versioniert.
