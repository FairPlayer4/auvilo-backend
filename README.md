# auvilo-backend

Backend für **AuViLo-Reminder** (Arbeitsname avlreminder), eine Android-App für
Erinnerungen, die erst als erledigt gelten, wenn sie nachweislich erledigt sind. Die App
liegt im Schwester-Repo [FairPlayer4/auvilo](https://github.com/FairPlayer4/auvilo).

Das Backend ist ein **privates Lernprojekt**: Google Cloud, Go auf dem Server,
Ende-zu-Ende-Verschlüsselung, Datenschutz nach DSGVO. Heute liefert es das optionale
**Cloud-Backup** der App. Später soll es Erinnerungen zwischen Geräten und Personen
synchronisieren und, nur nach ausdrücklicher Freigabe, eine Cloud-KI anbinden.

> **Stand:** Eigenbetrieb. Das Cloud-Backup ist in der App eingebunden und sichert nur
> die Daten des Entwicklers. Weitere Nutzer gibt es erst nach der „Sicherheitsphase“
> (alle Schlüssel neu, siehe [b2b-entwurf.md](docs/b2b-entwurf.md)). Der Dienst ist
> nur auf Einladung nutzbar.

## Grundsätze

- **Der Server sieht keine Inhalte.** Alles, was eine Person sichert, wird auf ihrem Gerät
  mit [Tink](https://developers.google.com/tink) verschlüsselt. Der Server speichert nur
  Chiffrate und die nötigsten Metadaten (Konto-Pseudonym, Größe, Zeitpunkt).
- **Login entscheidet über Zugriff, Schlüssel über Lesbarkeit.** Angemeldet wird mit
  Google. Lesen kann nur, wer den passenden Schlüssel hat.
- **Datenminimierung:** Gespeichert wird nie die Google-Konto-ID, sondern ein HMAC davon
  mit geheimem Pfeffer. Keine E-Mail-Adresse, kein Name, keine Suche nach Personen.
- **Nur EU:** alles in `europe-west1` (Belgien), per Test abgesichert.
- **Kosten sind begrenzt, nicht nur überwacht:** Budget 20 € im Monat, im Leerlauf 0 €.
  Ein Kill-Switch sperrt alle Dienste ab 100 % des Budgets und trennt das Projekt ab
  150 % vom Rechnungskonto. **Kosten gehen vor Verfügbarkeit**: Das Cloud-Backup ist
  bewusst kein verlässliches Backup.
- **Die App bleibt lokal-first.** Alarme und Fachlogik laufen auf dem Gerät; ohne Backend
  funktioniert die App vollständig.
- **Vergessen ist Standard:** Ablaufzeiten auf dem Server, *Konto löschen* löscht alles.

Ausführlich: [Anforderungen](docs/anforderungen.md), Kap. 1, 2 und 4.

## Aufbau

```
proto/      Schnittstelle (Protobuf + Connect, mit buf verwaltet) und gemeinsame
            Testvektoren für Go und Kotlin (proto/testvektoren)
server/     Go-Server für Cloud Run: Anmeldung, Sitzungen, Geräte, Schlüssel-Backup,
            Datensicherung, Diagnose, Konto löschen
client/     Kotlin (Gradle):
  protokoll   Schlüssel, Formate und Abläufe ohne Netz und ohne Android – läuft
              unverändert in der App
  verbindung  Connect-Anbindung an den Server (Adapter zu den Schnittstellen in protokoll)
  testclient  Kommandozeile, die ein Gerät gegenüber dem Server spielt
infra/      OpenTofu: GCP-Projekt, Cloud Run, Firestore, Buckets, Budget,
            Kill-Switch (Go-Funktion), Alarme, Workload Identity für GitHub
werkzeuge/  pruefen.sh (lokale Prüfung wie die CI), buf.sh, diagnose-holen.sh
docs/       Anforderungen, Entwürfe je Etappe, Formate, Datenschutz
```

| Baustein | Wahl |
|---|---|
| Server | Go auf Cloud Run, 0–2 Instanzen, eigenes Dienstkonto |
| Schnittstelle | Protobuf mit [Connect](https://connectrpc.com), Schemas mit `buf` |
| Daten | Firestore (Native Mode), Gültigkeitsdauer für Sitzungen und Zähler |
| Datensicherungen | Cloud Storage, versioniert (ältere Fassungen 7 Tage) |
| Krypto | Tink im Client: XChaCha20-Poly1305, HPKE, Ed25519, HKDF |
| Infrastruktur | OpenTofu, Zustand in einem EU-Bucket |
| CI/CD | GitHub Actions, Deploy nur von `main` per Workload Identity Federation (keine Schlüsseldateien) |

Begründungen: [Anforderungen](docs/anforderungen.md), Kap. 5 und 11.

## Verschlüsselung in Kürze

- Jede Person hat einen **Personenschlüssel** (HPKE und Ed25519) und einen
  **Tresorschlüssel** für ihre privaten Daten; jedes Gerät hat einen eigenen
  **Geräteschlüssel**.
- Ein **Wiederherstellungsschlüssel** (256 Bit, im Passwortmanager oder auf Papier) öffnet
  das Schlüssel-Backup auf dem Server. Alle Schlüssel daraus sind per HKDF mit eigenem
  Label abgeleitet (Format v2).
- Ein neues Gerät darf erst etwas ändern, wenn es **freigeschaltet** ist: durch ein
  vertrautes Gerät oder mit dem Wiederherstellungsschlüssel.
- Die Datensicherung ist mit dem Tresorschlüssel verschlüsselt. Ein Zweck-Präfix in den
  zugehörigen Daten bindet jedes Chiffrat an seinen Zweck und sein Konto.

Maßgeblich für alle Formate: [formate.md](docs/formate.md), mit festen Testvektoren, die
nie neu erzeugt werden.

## Etappen

| Etappe | Inhalt | Stand |
|---|---|---|
| B0 | GCP-Projekt, Budget, Kill-Switch, EU-Logs, CI | fertig |
| B1 | Go-Server, Google-Anmeldung, Sitzungs-Token, Geräte sperren | fertig, in der Cloud getestet |
| B2 | Schlüsselmodell, Schlüssel-Backup, Kotlin-Client | fertig, in der Cloud getestet |
| B2b | Daten-Backup für eine Person, Diagnose-Log, *Konto löschen*, Einbindung in die App | fertig, Eigenbetrieb |
| B3 | Kontakte per QR, Freigaben, signiertes Log, Konfliktauflösung | geplant |
| B4–B6 | Push, Cloud-KI (EU), Play-Store-Vorbereitung | später |

Was gerade läuft und was vor dem ersten weiteren Nutzer noch fehlt:
[stand.md](docs/stand.md).

## Dokumentation

| Dokument | Inhalt |
|---|---|
| [stand.md](docs/stand.md) | aktueller Stand in GCP, offene Punkte – **zuerst lesen** |
| [anforderungen.md](docs/anforderungen.md) | Anforderungen, offene Fragen (Kap. 10), Entscheidungen mit Datum (Kap. 11) |
| [b1-entwurf.md](docs/b1-entwurf.md) | Server, Google-Login, Sitzungen, Geräte |
| [b2-entwurf.md](docs/b2-entwurf.md) | Schlüsselmodell, Schlüssel-Backup, Kotlin-Client |
| [b2b-entwurf.md](docs/b2b-entwurf.md) | Daten-Backup, App-Einbindung, Checkliste der Sicherheitsphase |
| [diagnose-entwurf.md](docs/diagnose-entwurf.md) | Diagnose-Log aus der App, ohne Inhalte |
| [formate.md](docs/formate.md) | Client-Formate und Testvektoren (maßgeblich) |
| [datenschutz/](docs/datenschutz/README.md) | DSGVO: Verzeichnis der Verarbeitungstätigkeiten, TOMs, DSFA, Datenschutzerklärung, Abläufe (Entwürfe) |
| [gcp-einrichtung.md](docs/gcp-einrichtung.md) | was im GCP-Projekt von Hand eingerichtet wird |
| [entwicklungsumgebung.md](docs/entwicklungsumgebung.md) | Werkzeuge und Versionen |
| [go-review.md](docs/go-review.md) | wie man den Go-Code liest und reviewt |

Aus Sicht der App: [backend.md](https://github.com/FairPlayer4/auvilo/blob/develop/docs/backend.md)
und [cloud-entscheidungen.md](https://github.com/FairPlayer4/auvilo/blob/develop/docs/cloud-entscheidungen.md)
im App-Repo.

## Entwickeln

Voraussetzungen: Git Bash, Docker Desktop und ein JDK 21 (Details in
[entwicklungsumgebung.md](docs/entwicklungsumgebung.md)). Go und OpenTofu laufen in
Linux-Containern wie in der CI, Kotlin über den Gradle-Wrapper.

```bash
werkzeuge/pruefen.sh        # schnell: Go-Tests mit Firestore-Emulator, Lint, Kotlin, OpenTofu
werkzeuge/pruefen.sh voll   # alles wie die CI: dazu Race-Detector, govulncheck, buf breaking,
                            # tofu test, Server-Image bauen und starten, actionlint
cd client && ./gradlew check   # nur der Kotlin-Teil
```

- **Maßgeblich ist die CI** ([ci.yml](.github/workflows/ci.yml)) auf `develop`, `main`
  und bei Pull Requests. `pruefen.sh` prüft lokal dasselbe, um einen roten Lauf
  nachzustellen oder schneller Rückmeldung zu bekommen.

- **Tests sind die Spezifikation:** tabellengetrieben, mit deutschen Testfallnamen, zu
  jeder Erlaubnis ein Ablehnungsfall. Sicherheitskritischer Code ist mit `// KRITISCH:`
  markiert. Code und Kommentare sind auf Deutsch.
- **Branches:** gearbeitet wird auf `develop`; nach `main` nur per Pull Request. Ein Merge
  nach `main` deployt den Server, wenn die CI grün ist.
- **Infrastruktur** ändert sich nur über OpenTofu (`infra/`). Lokale Werte wie das
  Rechnungskonto stehen in `infra/werte.auto.tfvars` und nicht im Repo (Vorlage:
  `werte.auto.tfvars.beispiel`).
- Pull Requests aus Forks laufen in der CI erst nach Freigabe.

## Lizenz

[Apache License 2.0](LICENSE), Copyright 2026 Kevin Bartik.
