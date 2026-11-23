# avlreminder-backend – Hinweise für Claude

Experimentelles Backend für avlreminder (Android-App in `../avlreminder`) auf Google
Cloud. Lernprojekt, vorerst **nicht mit der App verbunden**. Schwerpunkte:
Synchronisation zwischen Anwendern mit E2E-Verschlüsselung, EU-Datenhaltung, DSGVO,
Cloud-KI nur nach Freigabe, möglichst günstig (max. 20 €/Monat).
Kommunikation auf **Deutsch, mit ß**.

## Wo was steht
- [docs/stand.md](docs/stand.md) – aktueller Stand, offene Punkte (zuerst lesen)
- [docs/anforderungen.md](docs/anforderungen.md) – Anforderungen, offene Fragen (Kap. 10), Entscheidungen (Kap. 11)
- [docs/gcp-einrichtung.md](docs/gcp-einrichtung.md) – was Kevin in GCP vorab tun muss
- [docs/entwicklungsumgebung.md](docs/entwicklungsumgebung.md) – installierte Werkzeuge
- [docs/go-review.md](docs/go-review.md) – Review-Anleitung; Code und Tests danach ausrichten
- [docs/b2-entwurf.md](docs/b2-entwurf.md) – Schlüsselmodell, Backup, Kotlin-Client (`client/`)
- [docs/b2b-entwurf.md](docs/b2b-entwurf.md) – Daten-Backup, Vorbereitung der App-Einbindung
- [docs/diagnose-entwurf.md](docs/diagnose-entwurf.md) – Diagnose-Log aus der App
- [docs/datenschutz/](docs/datenschutz/README.md) – DSGVO-Dokumente (VVT, TOMs, DSFA,
  Datenschutzerklärung, Abläufe) und offene Punkte

## Code
- Go-Code erklärend kommentieren (Kevin kennt Go nur grob). Kevin liest vor allem
  Tests und Code mit `// KRITISCH:` – Tests sind die Spezifikation (tabellengetrieben,
  deutsche Testfallnamen, zu jeder Erlaubnis ein Ablehnungsfall).
- Kotlin (`client/`) gilt dasselbe: JUnit-6-Tests mit deutschen Namen, parametrisiert,
  `// KRITISCH:`; Compiler-Warnungen sind Fehler. Der Code aus `proto/` entsteht im
  Gradle-Build. `protokoll` bleibt ohne Netz und ohne Android, damit es in die App passt.
- Abläufe, die auch die App braucht, gehören nach `protokoll` (Server und lokale Ablage
  als Schnittstellen); die Connect-Anbindung (Adapter zu diesen Schnittstellen) liegt in
  `verbindung`, `testclient` ist nur CLI und Dateiablage.
- Tests prüfen Verhalten, nicht die geprüfte Konstante selbst (kein `VERSUCHE == 3`).
- Lokal prüft `werkzeuge/pruefen.sh` (Git Bash, Docker) wie die CI; Go und OpenTofu
  laufen dort in Linux-Containern wie in der CI (Smart App Control blockierte sie früher
  zeitweise, [entwicklungsumgebung.md](docs/entwicklungsumgebung.md)). actionlint
  steckt in `pruefen.sh voll`.

## Zusammenarbeit
- Kevin entwickelt die Anforderungen iterativ mit Claude: Feedback und Rückfragen geben,
  Empfehlung statt Optionsliste.
- Neue Anforderungen und Entscheidungen in `docs/anforderungen.md` festhalten
  (Entscheidungstabelle mit Datum).
- Keine echten Inhalte aus Kevins App, keine Schlüssel oder Secrets ins Repo.
- GCP-Ressourcen, die Geld kosten, erst nach Kevins Go anlegen.

## Geheimnisse und Sicherheitsphase
- Was Claude liest (Terminal, Dateien, Befehlsausgaben), geht in den Gesprächsverlauf
  (Anthropic und lokal `~/.claude/projects`). Claude liest, druckt und ruft daher
  **nie** ab, in keiner Phase: Passwörter, 2FA-Codes, Dienstkonto-Schlüssel,
  Google-ID-Tokens und Auth-Codes, OAuth-Client-Secret, echte App-Inhalte im
  Klartext, Daten von Testern oder anderen Nutzern. Die Zugänge schützen mehr als die
  App (Kosten, Google-Konto), und Zurücksetzen macht ihren Verlust nicht rückgängig.
  Unbedenklich: Fingerabdrücke, Geräte-IDs, Konto-Pseudonyme, Chiffrate, Client-IDs.
- **Ausnahme Diagnose-Logs:** Sendungen freigeschalteter Konten
  ([diagnose-entwurf.md](docs/diagnose-entwurf.md), E-D-3/E-D-7) darf Claude lesen,
  weil sie per Konstruktion keine Inhalte enthalten – auch in der Sicherheitsphase.
  Fällt doch ein Inhalt auf: als Fehler im Logger melden und nicht weiterlesen.
- **Eigenbetrieb (jetzt, entschieden 2026-11-07):** Echte Daten gibt es nur von Kevin
  selbst. Bis zur Sicherheitsphase darf Claude Pfeffer, Wiederherstellungsschlüssel,
  Keysets, private Geräteschlüssel (`geraet-*.pem`) und Sitzungs-Token
  (`sitzung-*.json`) sehen, auch die für Kevins echte Daten – aber nur, wenn eine
  Aufgabe es braucht. Sie gelten als bekannt und werden zu Beginn der
  Sicherheitsphase alle ersetzt ([b2b-entwurf.md](docs/b2b-entwurf.md), „Vor dem
  ersten weiteren Nutzer“).
- **Sicherheitsphase:** beginnt vor dem ersten Schritt, bei dem jemand außer Kevin
  die App gegen die Cloud nutzt: Teilen mit einer anderen Person (B3, auch
  Familie), erster Tester, Veröffentlichung im Play Store. Claude kündigt sie
  **vorher ausdrücklich** an (Nachricht mit „Beginn der Sicherheitsphase“ und Eintrag
  in `docs/stand.md`), dann wird nach der Checkliste in b2b-entwurf.md alles
  zurückgesetzt und neu eingerichtet. Ab dann sieht Claude auch keine der oben unter
  Eigenbetrieb genannten Werte mehr; Kevin führt solche Schritte selbst in einem
  sicheren Kontext aus.

## GCP
- Projekt `avlreminder-exp-1ef2`, Region `europe-west1`, Infrastruktur in `infra/` (OpenTofu).
- Änderungen an der Infrastruktur nur über OpenTofu; vor `tofu apply` den Plan
  zusammenfassen. Was Geld kostet oder etwas abschaltet, erst nach Kevins Go.
- Lokale Werte (Rechnungskonto) in `infra/werte.auto.tfvars`, nicht im Repo.
- Kosten gehen vor Verfügbarkeit. Jeder Cloud-Run-Dienst bekommt ein eigenes
  Dienstkonto; öffentlich wird er nur über das Modul `infra/module/oeffentlich`
  (mit `depends_on` auf den Dienst). Der Kill-Switch sperrt, indem er `allUsers`
  entfernt, und das Modul verhindert, dass `tofu apply` gesperrte Dienste wieder
  öffnet (nur mit `-var=gesperrte_dienste_oeffnen=true`).
- Nur EU-Regionen: Standorte immer über `var.region`; `tofu test` und
  `infra/pruefe-regionen.sh` prüfen das.

## Git
- Remote `origin`: GitHub `FairPlayer4/auvilo-backend` (öffentlich, seit 2026-11-23;
  das alte private Repo `FairPlayer4/avlreminder-backend` ist das Archiv mit PRs bis #27).
  `develop` ist der Arbeitsbranch; `main` nur per Pull Request. Der lokale Ordner heißt
  weiter `avlreminder-backend`.
- **Feature-Branches nur nach Kevins Bestätigung anlegen** (sonst kommt er durcheinander,
  2026-11-15); ohne Bestätigung direkt auf `develop` arbeiten, in beiden Repos.
- Commit-Nachrichten auf Deutsch.
- Vor Commit/Push fragen, solange Kevin das nicht anders sagt (er schaut vorher drüber).
- CI läuft auf `main`, `develop` und bei PRs (wieder seit dem öffentlichen Repo,
  2026-11-23); PRs aus Forks erst nach Freigabe. Trotzdem vor jedem Push mit Code-Änderungen (Go, Kotlin,
  Proto, OpenTofu, Skripte, CI) `werkzeuge/pruefen.sh` und vor einem PR nach `main`
  (auch Dependabot-Branches) `werkzeuge/pruefen.sh voll`, beide grün. Reine Doku
  (Markdown) braucht kein Prüfskript (Kevin, 2026-11-14). Bei kleinen Code-Änderungen
  darf man abwägen, im Zweifel aber lieber einmal zu viel prüfen. Nach dem Merge die CI auf `main` ansehen; ist sie rot, folgt kein Deploy.
