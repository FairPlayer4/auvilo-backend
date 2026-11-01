# Review-Gedächtnis
Letzter Lauf: 2026-11-01 @ 871e695 (Fixes auf review/2026-11-01-2)   Laufzähler: 2

## Beabsichtigte Architektur
Vom Entwickler bestätigt am 2026-11-01.

- **Monorepo:** `proto/` (Schnittstelle, Protobuf/Connect mit buf), `server/` (Go),
  `client/` (Kotlin: Protokoll-Modul und Test-Client), `infra/` (OpenTofu und
  Hilfsfunktionen wie `killswitch`), `docs/`. Stand Lauf 2: `proto/`, `server/`, `infra/`,
  `docs/`; `client/` kommt mit B2.
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
| Server: Anmeldung/Sitzung | `server/internal/{geraeteschluessel,anmeldung,sitzung,dienst}`, `proto/` | b1-entwurf.md, anforderungen.md Kap. 11 | `*_test.go` | 2026-11-01 (Lauf 2) | gut; Testlücken (Streaming, Logs) |
| Server: Speicher/Start | `server/internal/speicher`, `server/cmd/server` | b1-entwurf.md (Daten, Betrieb) | `vertrag_test.go` (auch Emulator), `main_test.go` | 2026-11-01 (Lauf 2) | ok; Gleichzeitigkeit ungetestet |
| Werkzeug testanmeldung | `server/cmd/testanmeldung` | b1-entwurf.md | `main_test.go` | 2026-11-01 (Lauf 2) | ok; Google-Flow ungetestet |
| CI und Werkzeuge | `.github/`, `.golangci.yml`, `proto/buf*.yaml` | anforderungen.md Q4, go-review.md | – | 2026-11-01 (Lauf 2) | Matrix und buf neu; nie auf GitHub gelaufen |
| Doku | `docs/`, `CLAUDE.md`, `README.md` | – | – | 2026-11-01 (Lauf 2, nur B1-Teile) | ok |

## Bekannte Schulden und beobachtete Drift
- 2026-11-01: Offene kleinere Empfehlungen aus Lauf 1: E4 (Modul-Matrix in der CI,
  spätestens mit `server/`), E5 (eigenes Registry-Repo für Funktionen), E7 (Herkunft
  der Budget-Meldung prüfen, optional), E8 (Zeitlimit beim Trennen).
- 2026-11-01 (Lauf 2): Offene Empfehlungen: Vertragstest für Gleichzeitigkeit (major),
  Drossel beim Laden der Google-Schlüssel oder als Grenze dokumentieren, feste
  Pfeffer-Version in der Infrastruktur, Desktop-Client-ID im Betrieb entfernen (mit
  App), Testlücken (`googleIDToken`, Streaming, Logs, Sperr-Wettlauf in `dienst`),
  testanmeldung an Server binden. E4 erledigt; E5, E8 mit dem Deploy von B1.
- 2026-11-01 (Lauf 2): Beobachtung „Test hängt an der Konstante, die er prüfen soll“
  (iat-Fall) – beim nächsten Lauf auf ähnliche Muster achten.
- Erledigt nach Lauf 1 (in develop): E1 Sperr-Logik testbar (`sperren.go`), E2 Rechte
  verkleinert, E3 D1-Test in der CI, E6 Build-Konto ohne Builder-Rolle, E9 Alarm per
  E-Mail (`infra/alarm.tf`), E10 Actions auf Node 24 per SHA, Dependabot.

## Offene Fragen an den Entwickler
- 2026-11-01 (Lauf 2): Wiederholte Anfragen im Zeitfenster (Anmelden ~5 min, Erneuern
  ±2 min) bewusst zulassen? Vorschlag: ja, festhalten und Test ergänzen.
- 2026-11-01 (Lauf 2): Geräte-IDs (pseudonym) in Logs zulassen? Vorschlag: ja.

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
