# Review-Gedächtnis
Letzter Lauf: 2026-11-05 @ a9abda7 (Fixes auf review/2026-11-05, nach develop gemergt)   Laufzähler: 8

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
| Kill-Switch | `infra/killswitch/` | anforderungen.md KO3, stand.md | `*_test.go` | 2026-11-05 (Lauf 8, IAM-Sperrweg) | gut nach Fixes; IAM-Sperrweg nicht in der Cloud getestet (E1) |
| Infrastruktur | `infra/*.tf`, `infra/*.sh` | anforderungen.md 4.3, 5, 6; gcp-einrichtung.md; stand.md | `infra/tests/` (tofu test), `pruefe-regionen.sh` | 2026-11-05 (Lauf 8, Modul `oeffentlich`, Löschschutz) | gut nach Fixes; Deploy ohne CI-Gate, Textprüfung der Öffnung (E3, E4) |
| Server: Anmeldung/Sitzung | `server/internal/{geraeteschluessel,anmeldung,sitzung,dienst/dienst.go}`, `proto/` | b1-entwurf.md, proto-Kommentare, anforderungen.md Kap. 11 | `*_test.go` | 2026-11-05 (Lauf 8) | gut; Sperren bei vielen Sitzungen (Frage 4), Schlüsselausfall = abgelehnt (E14) |
| Server: Speicher/Start | `server/internal/speicher`, `server/cmd/server` | b1-entwurf.md (Daten, Betrieb) | `vertrag_test.go` (auch Emulator), `main_test.go` | 2026-11-05 (Lauf 8) | gut; `Speicher` 10 Methoden (E10), Geräte je Konto unbegrenzt (Frage 5) |
| Server: Datensicherung | `server/internal/datensicherung`, `dienst/datensicherung.go`, `datensicherung.proto` | b2b-entwurf.md, proto-Kommentare | `vertrag_test.go` (Arbeitsspeicher, fake-gcs), `datensicherung_test.go` | 2026-11-05 (Lauf 8, Upload-Limit) | gut nach Fixes (unkomprimiert, Tageslimit-Tests); Holen unbegrenzt (Frage 3) |
| CI, Deploy und Container | `.github/`, `server/Dockerfile`, `.golangci.yml`, `proto/buf*.yaml` | anforderungen.md Q4, go-review.md, stand.md | CI-Job `container` | 2026-11-03 (Lauf 6, CI-Fix) | gut; Emulator ohne feste Version (Nit) |
| Doku | `docs/`, `CLAUDE.md`, `README.md` | – | – | 2026-11-03 (Lauf 6, vollständig) | gut nach Fixes; CLAUDE.md-Verweise (E6), Nits |
| Kotlin `protokoll` | `client/protokoll`, `proto/avlreminder/inhalt`, `proto/testvektoren` | b2-entwurf.md, b2b-entwurf.md, backup.proto | `*Test.kt`; Go prüft nur `geraetenachrichten.json` | 2026-11-03 (Lauf 6) | gut nach Fix; Regel „offenes Backup“ im Test-Client (E1) |
| Kotlin `verbindung` | `client/verbindung` | b2b-entwurf.md, proto-Kommentare | `AdapterTest` (Connect-Nachbau) | 2026-11-05 (Lauf 8) | gut nach Fix (403 ohne Connect-Fehler); Sitzung noch im Test-Client (E5), Fehlerarten (E8, E9) |
| Backup und Test-Client | `server/internal/dienst/schluessel.go`, Backup in `speicher`, `client/testclient`, `schluessel.proto` | b2-entwurf.md, proto-Kommentare | `schluessel_test.go`, `vertrag_test.go`, `EinrichtungTest`, `GoogleTest`, `AblageTest` | 2026-11-05 (Lauf 8) | gut nach Fix (kaputte Dateien ohne Inhalt); Löschen braucht Tresor (E2 Lauf 6) |
| Server: Diagnose | `server/internal/{diagnose,kontingent}`, `dienst/diagnose.go`, `diagnose.proto` | diagnose-entwurf.md, formate.md, proto | `vertrag_test.go` (Arbeitsspeicher, Emulatoren), `diagnose_test.go` | 2026-11-05 (Lauf 8, mit Datensicherung) | gut; Diagnose von nicht freigeschalteten Geräten (Frage 1) |
| Diagnose: Skript und Kotlin | `werkzeuge/diagnose-holen.sh`, `client/*/Diagnose*`, `ServerDiagnoseAblage`, `diagnose-senden` | diagnose-entwurf.md E-D-6 | `DiagnoseTest`, `AdapterTest`, `TestDiagnoseTest`; Skript nur von Hand | 2026-11-04 (Lauf 7, neu) | gut nach Fixes; Grenzwert `MAX_NUTZLAST` ungetestet (E3), Skript noch nie gegen echte Sendungen |
| Kotlin-Build | `client/*.gradle.kts`, `client/gradle/`, CI-Job `kotlin` | b2-entwurf.md (CI) | CI | 2026-11-05 (Lauf 8) | gut; Prüfsummen aller Abhängigkeiten |
| Freischaltung (E-B2b-13) | `server/internal/{freischaltung,dienst/freischaltung.go}`, Freischaltung in `speicher`, `geraete.proto`, Kotlin `Einrichtung` | b2b-entwurf.md, geraete.proto | `freischaltung_test.go`, `vertrag_test.go`, `EinrichtungTest` | 2026-11-05 (Lauf 8, neu) | gut nach Fixes; Prüfung opt-in je Handler (E2), erstes Backup schaltet frei (Frage 2) |

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
- Erledigt in Schritt 5: Build-Konto eingeschränkt, GOMEMLIMIT, Image-Scan entschieden,
  Absenz-Alarm, `pruefe-regionen.sh` für Listen, tofu-Testlücken (Lauf 6 gefixt).

- 2026-11-03 (Lauf 6), offen (Details und Empfehlungen E1–E6 in runs/2026-11-03-2.md):
  - Vor echten Daten: Pfeffer-Schutz, Upload-Limit, Sperren ohne actAs (seit Lauf 3) –
    stand.md hatte sie auf B6 geschoben, Tester kommen aber schon in B2b.
  - **Trend (Lauf 5 und 6): Ein Test kann nicht rot werden**, weil Testdaten gleich sind
    (tofu-Mocks) oder eine Grenze nicht getroffen wird (8 MiB, Startzähler). Mit
    Mutations-Stichproben gefunden; bei neuen Tests einen Gegenwert wählen.
  - Doku: „Bekannte Grenzen“ wachsen in stand.md; erledigte Sätze aufräumen.

- 2026-11-04 (Lauf 7), offen (Details und Empfehlungen E1–E4 in runs/2026-11-04.md):
  - **Trend (Lauf 5, 6, 7): Test trifft die Grenze nicht** – diesmal Kotlin
    `MAX_NUTZLAST` (E3). Und: Ein Schutz (Rolle, `prevent_destroy`, weitere
    IAM-Bindung) wird nur positiv geprüft, nicht gegen „zusätzlich zu weit“.
  - Pfeffer-Wiederherstellung ungetestet (Frage 3).

- 2026-11-05 (Lauf 8), offen (Details, Fragen 1–7 und Empfehlungen E1–E18 in
  runs/2026-11-05.md):
  - **Trend (Lauf 7 und 8): Ein Schutz wird nur positiv geprüft.** Lauf 8: Erlaubnisfälle
    der Freischaltung prüften nur `err == nil`, nicht die Wirkung (Mutant „Löschen ohne
    Wirkung“ blieb grün; gefixt 31cf529, 9f747db). Bei Erlaubnis-Tests immer die Wirkung
    prüfen, bei Schutz-Tests auch „zusätzlich zu weit“.
  - **Trend (Lauf 5 und 8): Sitzung/Anmelden noch im Test-Client, Callbacks in
    `protokoll` synchron mit festen Texten** (E5, E6) – vor der App-Einbindung.
  - **Drift wächst (Lauf 4 → 8): `speicher.Speicher` von 6 auf 10 Methoden**, `dienst`
    wird Sammelbecken; Freischaltprüfung ist opt-in je Handler (E2, E10). Vor B3 handeln.
  - **Trend (Lauf 6 und 8): stand.md wird zum Tagebuch** (E17).
  - Vor echten Daten: Kill-Switch-IAM-Sperrweg und echte 403-Antwort in der Cloud testen
    (E1); Deploy ohne CI-Gate (E3).

## Offene Fragen an den Entwickler
Aus Lauf 7, Frage 3: Pfeffer-Wiederherstellung durchspielen – entschieden (ja, vor
der Sicherheitsphase), Durchlauf steht noch aus.
Erledigt am 2026-11-07: Lauf 8, Fragen 6 (Ablehnung gemerkt, automatische Sicherung
ruht) und 7 (2 Instanzen bleiben); Lauf 7, Frage 1 bestätigt. Lauf 8, Fragen 1–5
am 2026-11-06.
Erledigt am 2026-11-05: Frage 2 (Adapter unterscheiden „zu groß“ und „Tageslimit“ an
der gesendeten Größe, Diagnose und Datensicherung), Empfehlung E3 (Grenzwerttest
`MAX_NUTZLAST`), Upload-Limit (48 je Tag, E-B2b-12).
Lauf 6 erledigt: 200 Fassungen (stündlich), WARN ab 100 %, Pfeffer-Offline-Kopie.
Erledigt am 2026-11-05: Sperren ohne actAs (Kill-Switch entfernt `allUsers`, Modul
`infra/module/oeffentlich` hält gesperrte Dienste bei `tofu apply` zu), Löschschutz.

## Getroffene Entscheidungen
- 2026-11-01: Architekturbild bestätigt.
- 2026-11-01: `.review/` wird im Repository versioniert.
- 2026-11-01: Cloud-Änderungen aus Lauf 1 anwenden: ja.
- 2026-11-01: Editor-Rolle der Standardkonten entfernen und `serviceAccountUser`
  einschränken: ja.
- 2026-11-01: Keine Pflicht-Statuschecks (GitHub Free, privat) – Konvention reicht, kein
  produktiver Einsatz. Nicht erneut als Fund melden.
- 2026-11-01: Kosten vor Verfügbarkeit: `tofu apply` darf gesperrte Dienste nicht wieder
  öffnen (seit 2026-11-05 über `infra/module/oeffentlich` statt `ignore_changes =
  [ingress]`). Hohe Kosten gelten als Zeichen für Fehler/Angriff.
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
- 2026-11-03 (nach Lauf 5, alle zehn Fragen, anforderungen.md Kap. 11): Neuanmeldung
  nach Sperren bekannte Grenze bis vor echten Daten; Sitzung vor dem Lesen geprüft;
  Schlüssel-Backup-Version ab 1 nach Löschen (kein Grabstein); Wiederherstellen =
  Übernehmen, gesehene Zähler; Löschen in `protokoll` behält den Zähler; 7 Tage/100
  Fassungen; Kill-Switch-WARN nur bei Eingriff; Löschschutz vor echten Daten; SE1 meint
  Sync-Operationen ab B3; SE3 ohne eigenen Scanner. Nicht erneut als Fund melden.
- 2026-11-03: Bucket ohne Soft Delete; `GOMEMLIMIT=192MiB`, Parallelität 80;
  Absenz-Alarm 23,5 h.
- 2026-11-06 (nach Lauf 8, Fragen 1–5 wie vorgeschlagen, umgesetzt): Diagnosen nur
  von freigeschalteten Geräten; erstes Backup eines nicht freigeschalteten Geräts nur,
  wenn das Konto kein freigeschaltetes, ungesperrtes Gerät hat; Holen 50, Löschen 20,
  Sitzungen 200 je Gerät und Tag; Interceptor prüft „gesperrt“, Sperren löscht in
  Stapeln; höchstens 10 offene Geräte je Konto. Egress-Alarm (1 GiB in 23 h) nach
  Kevins Go in `infra/alarm.tf`. Nicht erneut als Frage stellen.
