# Review-Gedächtnis
Letzter Lauf: 2026-11-03 @ 3180671 (Fixes auf review/2026-11-03)   Laufzähler: 5

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
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, stand.md | `*_test.go` | 2026-11-03 (Lauf 5) | gut; WARN-Dauertakt (Frage) |
| Infrastruktur | `infra/*.tf`, `infra/*.sh` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md; stand.md | `infra/tests/` (tofu test), `pruefe-regionen.sh` | 2026-11-03 (Lauf 5, vollständig) | ok; Build-Konto breit, Testlücken tofu, Schritt-5-Punkte |
| Server: Anmeldung/Sitzung | `server/internal/{geraeteschluessel,anmeldung,sitzung,dienst/dienst.go}`, `proto/` | b1-entwurf.md, proto-Kommentare, anforderungen.md Kap. 11 | `*_test.go` | 2026-11-03 (Lauf 5, vollständig) | gut; Neuanmeldung nach Sperren (Frage), Streaming ungetestet |
| Server: Speicher/Start | `server/internal/speicher`, `server/cmd/server` | b1-entwurf.md (Daten, Betrieb) | `vertrag_test.go` (auch Emulator), `main_test.go` | 2026-11-03 (Lauf 5) | gut |
| Server: Datensicherung | `server/internal/datensicherung`, `dienst/datensicherung.go`, `datensicherung.proto` | b2b-entwurf.md, proto-Kommentare | `vertrag_test.go` (Arbeitsspeicher, fake-gcs), `datensicherung_test.go` | 2026-11-03 (Lauf 5, neu) | gut; begrenze vor Sitzungsprüfung (Frage) |
| CI, Deploy und Container | `.github/`, `server/Dockerfile`, `.golangci.yml`, `proto/buf*.yaml` | anforderungen.md Q4, go-review.md, stand.md | CI-Job `container` | 2026-11-01 (Lauf 4) | gut; Gradle-Cache jetzt `basic` |
| Doku | `docs/`, `CLAUDE.md`, `README.md` | – | – | 2026-11-01 (Lauf 3, vollständig; Lauf 4 B2-Teile) | gut nach Fixes; anforderungen.md [E]-Markierungen veraltet |
| Kotlin `protokoll` | `client/protokoll`, `proto/avlreminder/inhalt`, `proto/testvektoren` | b2-entwurf.md, b2b-entwurf.md, backup.proto | `*Test.kt`; Go prüft nur `geraetenachrichten.json` | 2026-11-03 (Lauf 5) | gut; gesehene Zähler, Wiederherstellen = Übernehmen (Fragen) |
| Kotlin `verbindung` | `client/verbindung` | b2b-entwurf.md, proto-Kommentare | `AdapterTest` (Connect-Nachbau) | 2026-11-03 (Lauf 5, neu) | gut; Sitzung noch im Test-Client |
| Backup und Test-Client | `server/internal/dienst/schluessel.go`, Backup in `speicher`, `client/testclient`, `schluessel.proto` | b2-entwurf.md, proto-Kommentare | `schluessel_test.go`, `vertrag_test.go`, `EinrichtungTest`, `GoogleTest` | 2026-11-03 (Lauf 5) | gut nach Fixes; `Ablage` ohne Tests, ABA (Frage) |
| Kotlin-Build | `client/*.gradle.kts`, `client/gradle/`, CI-Job `kotlin` | b2-entwurf.md (CI) | CI | 2026-11-03 (Lauf 5) | gut; Prüfsummen aller Abhängigkeiten |

## Bekannte Schulden und beobachtete Drift
- 2026-11-01 (Lauf 1): E7 (Herkunft der Budget-Meldung prüfen) offen, optional.
  E1–E6, E8–E10 erledigt.
- 2026-11-01 (Lauf 2), noch offen:
  - Drossel beim Laden der Google-Schlüssel, oder als Grenze dokumentieren.
  - Desktop-Client-ID im Betrieb entfernen (mit der App).
  - Testlücken: `googleIDToken`, Streaming, Logs, Sperr-Wettlauf in `dienst`.
  - testanmeldung an den Server binden.
  - Erledigt: Gleichzeitigkeits-Vertragstest; feste Pfeffer-Version (B1).
- **Trend (Lauf 2, 3, 4 und 5): Ein Test hängt an der Konstante, die er prüfen soll.**
  Lauf 5: `sitzung.Toleranz`/`Dauer` (gefixt, Mutant 10 s blieb vorher grün). Seit
  2026-11-02 Regel in go-review.md und CLAUDE.md; neuer Code hält sie ein, alter nicht.
  Empfehlung E1 (Lauf 5): einmal alle Tests gezielt durchsuchen.
- 2026-11-01 (Lauf 3), offen:
  - Skript-Check für die Cloud-Run-Regeln (Laufzeitkonten, `ignore_changes`) sowie
    tofu-Asserts für Leerlauf 0 € – spätestens mit dem 2. Dienst.
  - Sperren ohne actAs (invoker-Bindung entfernen) – vor B6.
  - WARN „Trennen beginnt“.
  - anforderungen.md: [E] bei entschiedenen Punkten, Ed25519-Begründung in Kap. 5
    (**Trend**, wieder in Lauf 4 gesehen, jetzt Teil von A2).
  - Nits: siehe runs/2026-11-01-3.md.

- 2026-11-01 (Lauf 4), offen (Details in runs/2026-11-01-4.md):
  - A1 Client-Abläufe nach `protokoll` (Einrichtung), A2 Mitgliedschaft Server vs. Log,
    A3 Client-Formate und Geräte-ID-Ableitung maßgeblich beschreiben + feste Vektoren,
    A4 Umschlag-Kontext und Keyset-Typen, A5 Speicher/Dienste aufteilen vor B3,
    A6 `testanmeldung` entfernen nach dem Cloud-Test.
  - Erste Drift-Beobachtung: Interface `speicher.Speicher` beim Anbieter statt beim
    Verwender; wächst mit B3. Beim nächsten Architektur-Blick (Lauf 8) vergleichen.
  - Nach dem Merge von B2: Ausnahme `FILE_SAME_JAVA_*` in buf.yaml entfernen.

- 2026-11-03 (Lauf 5), offen (Details und Empfehlungen E1–E7 in runs/2026-11-03.md):
  - Testlücken Server (Streaming im Interceptor, Sperr-Rennen in `SitzungErneuern`,
    Speicherfehler im Gerätedienst) – seit Lauf 2 teils bekannt, **Trend**.
  - Schritt 5: Build-Konto einschränken, GOMEMLIMIT/Parallelität, Image-Scan (SE3),
    Absenz-Alarm Kill-Switch, `pruefe-regionen.sh` für Listen, tofu-Testlücken.
  - Mutant „genau 8 MiB“ im Sicherungsablauf unentdeckt.
  - Vor App-Einbindung: Sitzung nach `verbindung`, Callbacks suspend, zweiter
    Schlüsselbund nach Backup-Löschen.
- Erledigt in Lauf 5: Werkzeug `testanmeldung` ist entfernt (A6).

## Offene Fragen an den Entwickler
Aus Lauf 5 (runs/2026-11-03.md, je mit Vorschlag): 1 Neuanmeldung eines gestohlenen
Geräts nach dem Sperren (major); 2 Sicherungsplätze ohne Anmeldung belegbar; 3 ABA beim
Schlüssel-Backup nach Löschen; 4 Wiederherstellen = Übernehmen; 5 gesehene Zähler merken;
6 Löschen der Datensicherung in `protokoll`; 7 drei Fassungen vs. Alter; 8 Kill-Switch-WARN
im Dauertakt; 9 Löschschutz vor echten Daten; 10 Bedeutung von SE1.

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
- 2026-11-01 (Lauf 2): Wiederholte Anfragen im Zeitfenster sind bewusst erlaubt
  (anforderungen.md Kap. 11, Test in dienst_test). Nicht erneut als Fund melden.
- 2026-11-01 (Lauf 2): Geräte-IDs dürfen in Logs stehen (anforderungen.md Kap. 11).
  Nicht erneut als Fund melden; Token, `sub` und Inhalte bleiben verboten.
- 2026-11-01 (nach Lauf 3): Pfeffer-Schutz angewendet (`tofu apply`).
- 2026-11-01 (nach Lauf 3): Keine Offline-Kopie des Pfeffers bis zu echten Nutzern
  (anforderungen.md Kap. 11). Nicht erneut als Fund melden.
- 2026-11-01 (nach Lauf 3): IP-Adressen in den Anfrage-Logs 30 Tage sind gewollt
  (anforderungen.md Kap. 11). Nicht erneut als Fund melden.
- 2026-11-01 (nach Lauf 3): Alte Kill-Switch-Images im Repo `container` gelöscht.
- 2026-11-01: `EXCLUDE_ALL_CREDITS` bleibt (auch Gratis-Kontingent zählt) – erlaubt einen
  späteren Live-Test des Kill-Switch. Nicht erneut als Fund melden.
- 2026-11-02 (nach Lauf 4): Wiederherstellungsschlüssel vor dem Hochladen zeigen und
  bestätigen lassen; umgesetzt.
- 2026-11-02 (nach Lauf 4): Regel „Tests prüfen Verhalten, nicht die Konstante“ in
  go-review.md und CLAUDE.md.
- 2026-11-02 (nach Lauf 4): Gradle-Prüfsummen erst vor der App – inzwischen umgesetzt
  (Gradle prüft Prüfsummen aller Abhängigkeiten).
- 2026-11-02 (nach Lauf 4): A1 umgesetzt (Einrichtung in `protokoll`); A2 und A3 werden
  im B3-Entwurf geklärt.
