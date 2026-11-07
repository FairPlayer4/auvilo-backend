# Stand und nächste Schritte

Stand: 2026-11-06. Bei jedem größeren Schritt aktualisieren.

## In GCP (Projekt `avlreminder-exp-1ef2`, Region `europe-west1`)
Etappe **B0** ist angelegt (OpenTofu, `infra/`):
- APIs, Firestore `(default)` (Native Mode, europe-west1), Artifact Registry
  `container` mit Aufräumregeln
- Budget 20 €/Monat nur für dieses Projekt, Kosten **vor** Guthaben; E-Mail bei
  25/50/90/100 % und bei Prognose 100 %
- **Kill-Switch** (Cloud Function `killswitch`, Go): ab 100 % wird allen
  Cloud-Run-Diensten das öffentliche Aufrufrecht (`allUsers`) entzogen, ab 150 % wird das Projekt vom
  Rechnungskonto getrennt. Er wird per `tofu apply` aus dem **lokalen** Stand gebaut,
  nicht über `main`: nur von einem geprüften, committeten Stand anwenden.
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
  Seit 2026-11-07 verwaltet OpenTofu nur das Secret, nicht die Version mit dem
  Wert (Pfeffer-Probe: ein Import läse ihn in den Zustand; Ablauf in
  [gcp-einrichtung.md](gcp-einrichtung.md)). Angewendet: 1 vergessen, sonst nichts.
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
  Ebenso kostet Speicher (Bucket der Datensicherungen) nach der Sperre weiter, bis
  zum Trennen; die Lebenszyklusregeln greifen erst verzögert. Das Hochladen
  begrenzt der Server auf 48 Sicherungen je Konto und Tag
  ([b2b-entwurf.md](b2b-entwurf.md), E-B2b-12); Holen auf 50 und Löschen auf 20
  je Konto und Tag (seit 2026-11-06), dazu höchstens 4 gleichzeitige Aufrufe und
  der Egress-Alarm (1 GiB in 23 Stunden). Die Grenze von 4 gilt je Instanz; mit
  bis zu 2 Instanzen laufen also höchstens 8 Sicherungen gleichzeitig (entschieden
  2026-11-07).
- **Bekannte Grenze (Absenz-Alarm):** schlägt erst an, nachdem der Kill-Switch
  einmal aufgerufen wurde. Nach einem Neuaufbau einmal eine Meldung unter der
  Schwelle schicken (`gcloud pubsub topics publish budget-meldungen
  --message='{"costAmount":1,"budgetAmount":20}'`) und den Aufruf im Log prüfen.
- **Bekannte Grenze (Deploy-Konto):** Es darf den Dienst `server` ändern
  (`roles/run.developer`) und könnte damit technisch mehr als das Image setzen:
  den Ingress, Mindestinstanzen, CPU
  dauerhaft, bis zu 100 Instanzen, oder den Dienst löschen. Mindestinstanzen
  kosten auch nach dem Sperren weiter, bis ab 150 % getrennt wird. Der Deploy
  selbst ändert nur das Image; `tofu apply` setzt alles außer dem Image zurück.
  Einen vom Kill-Switch gesperrten Dienst kann es nicht öffnen (dafür fehlt
  `setIamPolicy`). Voraussetzung wäre fremder Code auf `main`.
- **Kill-Switch ohne actAs** (seit 2026-11-05): Er sperrt über die IAM-Richtlinie
  (`allUsers` entfernen) und darf nur Dienste auflisten und deren Richtlinie
  lesen und setzen. Er kann keinen Dienst ändern und kein Image starten.
  **Grenze:** Kompromittierter Code im Kill-Switch könnte Dienste sperren oder
  Rechte auf Dienste vergeben, auch sich selbst (z. B. `run.admin`, dann könnte
  er Dienste löschen). Ein fremdes Image starten kann er auch so nicht (kein
  actAs); die Dienste prüfen die Anmeldung ohnehin selbst.
  **In der Cloud getestet** am 2026-11-06 (Meldung 21/20, Testdienst `kstest`
  mit Zusatzbindung): `allUsers` an `kstest` und `server` nach ca. 10 s entfernt,
  die anderen Bindungen (`deploy` an beiden) blieben, `killswitch` selbst nicht
  gesperrt, Log-WARNING „2 gefunden, 2 gesperrt“, nicht getrennt, Mail kam an.
  `kstest` antwortete sofort mit 403, `server` erst nach ca. 2 Minuten
  (IAM-Verzögerung). `tofu plan` brach danach wie gewollt ab; mit
  `-var=gesperrte_dienste_oeffnen=true` kam nur die Bindung `allUsers` zurück,
  danach `tofu plan` ohne Änderungen und Testclient-Aufrufe wieder möglich.
- **Löschschutz** (seit 2026-11-05, vor echten Daten): Firestore, der Dienst
  `server` und der Bucket der Datensicherungen lassen sich nicht mit `tofu destroy`
  löschen. In GCP selbst schützt nur Firestore (`delete_protection_state`): Den
  Dienst (ohne Daten) und den Bucket (mit den Sicherungen, ohne Soft Delete, also
  endgültig) kann ein Owner in der Konsole oder mit `gcloud` weiter löschen. Das
  Projekt zu löschen geht weiterhin (Q7).
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
   Review-Lauf 4 am 2026-11-01: [.review/runs/2026-11-01-4.md](../.review/runs/2026-11-01-4.md),
   Fixes in develop. Am 2026-11-02 umgesetzt: Einrichtung liegt in `protokoll` (A1),
   Wiederherstellungsschlüssel wird vor dem Hochladen gezeigt und bestätigt.
   Deploy am 2026-11-02 (PR #6). **In der Cloud getestet** am 2026-11-02 (echte
   Google-Anmeldung): `cloud-pc` richtet neue Schlüssel ein (erst nach „ja“);
   `cloud-tablet` bricht nach drei falschen Eingaben ab; `cloud-handy` übernimmt mit
   dem Wiederherstellungsschlüssel das Backup (auch mit Kleinbuchstaben und fehlendem
   Bindestrich), gleicher Fingerabdruck wie `cloud-pc`; `cloud-pc` erneut meldet
   „schon eingerichtet“ ohne Rückfrage. Damit ist B2 abgeschlossen.
   Am 2026-11-02 `testanmeldung` (Go) entfernt (A6); der Kotlin-Test-Client kann
   alles, was es konnte.
3. Kleinere Empfehlungen aus dem Review: E7 optional; weitere aus Lauf 2 in
   `.review/state.md`.
4. Etappe **B2b** (vor B3): Daten-Backup für eine Person, danach Einbindung in die
   App. Entwurf: [b2b-entwurf.md](b2b-entwurf.md), bestätigt am 2026-11-02.
   **Erledigt (2026-11-02):** Schritte 1–3 der Reihenfolge – `docs/formate.md` mit
   festen Testvektoren, Gradle-Prüfsummen, `Datensicherung` und `Sicherungsablauf`
   in `protokoll`, `DatensicherungDienst` im Server mit Ablage in Cloud Storage
   (getestet gegen `fake-gcs-server`, auch in der CI). Der Server braucht jetzt
   `SICHERUNG_BUCKET`; vor dem nächsten Deploy muss der Bucket per OpenTofu stehen.
   **Für den Infrastruktur-Schritt:** Soft Delete des Buckets auf 0 Tage (sonst
   bleiben gelöschte Sicherungen 7 Tage liegen, DSGVO); Zeitlimit von Cloud Run
   und Server (30 s) für 8 MiB über langsames Mobilfunknetz prüfen.
   **Am 2026-11-03 (Schritt 4):** Modul `client/verbindung`
   (Connect-Clients und Adapter, aus `testclient` herausgelöst) mit
   `ServerSicherungsAblage`; Adapter-Tests gegen einen Connect-Nachbau (Fehlercodes
   je Methode). Server: `BackupLoeschen` für das Schlüssel-Backup. Test-Client:
   `sichern`, `wiederherstellen`, `sicherung-loeschen`, `schluessel-loeschen`.
   **Lokal von Ende zu Ende getestet** am 2026-11-03 (echte Google-Anmeldung,
   Firestore-Emulator, fake-gcs): `b2b-pc` richtet Schlüssel ein und sichert zweimal
   (zwei Fassungen im Bucket); `b2b-handy` übernimmt das Backup (gleicher
   Fingerabdruck), stellt die Sicherung byte-gleich wieder her und sichert selbst.
   `b2b-pc` mit veraltetem Stand fragt nach („stammt von Gerät …“): „nein“ lässt die
   Sicherung des Handys stehen, „ja“ überschreibt mit höherem Zähler.
   `sicherung-loeschen` entfernt alle Fassungen, danach legt ein Gerät mit altem
   Stand sauber neu an; `schluessel-loeschen` entfernt das Backup (zweimal ohne
   Fehler). Im Server-Log nur Gerät, Version und Größe.
   **Review Lauf 5 am 2026-11-03** (`.review/runs/2026-11-03.md`), Fragen wie
   vorgeschlagen entschieden (anforderungen.md Kap. 11): Sitzung vor den
   Sicherungsplätzen geprüft, gesehene Zähler gemerkt, `Sicherungsablauf.loeschen`
   behält den Zähler, Kill-Switch-Mail nur bei echtem Eingriff (aktiv seit
   `tofu apply` in Schritt 5), Grenzen und „Vor echten Daten“ ergänzt.
   **Schritt 5 (Infrastruktur), Konfiguration am 2026-11-03:** Bucket
   `…-datensicherung` (`infra/datensicherung.tf`: Versionierung, ältere Fassungen
   7 Tage/höchstens 200 (am 2026-11-04 von 100 erhöht), ohne Soft Delete, nur Server-Konto mit `objectUser`),
   `SICHERUNG_BUCKET` und `GOMEMLIMIT=192MiB` am Server (Parallelität bleibt 80,
   Begründung in `server.tf`), Build-Konto liest nur Quellen und `gcf-v2-*` und
   schreibt nur ins Repo `funktionen`, Absenz-Alarm „Kill-Switch wird nicht mehr
   aufgerufen“ (23,5 h), `pruefe-regionen.sh` prüft Listen, neue tofu-Tests.
   Image-Scan (SE3): kein eigener Scanner (anforderungen.md Kap. 11).
   Am selben Tag angewendet (`tofu apply`, 7 neu, 2 geändert, 3 entfernt; Kill-Switch
   mit dem engeren Build-Konto neu gebaut) und per PR #8 deployt.
   **In der Cloud getestet** am 2026-11-03 (Geräte `cloud-pc`, `cloud-handy`,
   Sitzung per `erneuern`, Dateien ohne echte Inhalte): `cloud-pc` sichert zweimal
   (125 B und 2 MB, zwei Fassungen im Bucket); `cloud-handy` stellt byte-gleich
   wieder her und sichert selbst; `cloud-pc` fragt dann nach („nein“ lässt die
   Sicherung stehen, „ja“ überschreibt mit Zähler 4); 9 MB lehnt der Client vor dem
   Hochladen ab; `sicherung-loeschen` entfernt alle Fassungen, danach „keine
   Datensicherung“ und Neuanlage. Ohne Sitzung antwortet der Dienst
   `unauthenticated`. Server-Log nur Gerät, Bytes, Version.
   **Schritt 6 (Review Lauf 6) am 2026-11-03** (`.review/runs/2026-11-03-2.md`).
   Am 2026-11-04 entschieden und umgesetzt: App sichert höchstens stündlich plus
   manuell, ältere Fassungen höchstens 200; Kill-Switch warnt (Mail), wenn ab 100 %
   kein Dienst gefunden wird. Beides wirkt erst nach `tofu apply` (Bucket-Regel und
   Neubau des Kill-Switch aus dem lokalen Stand).
   Pfeffer: Offline-Kopie zu Beginn der Sicherheitsphase (Ablauf in
   [gcp-einrichtung.md](gcp-einrichtung.md)), tofu-Test für den Schutz.
   Diagnose-Log aus der App: Entwurf [diagnose-entwurf.md](diagnose-entwurf.md)
   (bestätigt am 2026-11-04). Server, `infra/diagnose.tf` (Bucket `…-diagnose`,
   14 Tage, Server darf nur anlegen; Firestore-TTL der Tageszähler) und
   `werkzeuge/diagnose-holen.sh` am 2026-11-04 umgesetzt; Infrastruktur am selben
   Tag angewendet (3 neu, 1 geändert). Test-Client: `diagnose-senden` (feste
   Test-Diagnose; nennt bei „nicht freigeschaltet“ die Konto-ID), Packen und Senden
   in `protokoll` (`Diagnose`), Adapter in `verbindung`. **Am 2026-11-05 in der Cloud
   getestet:** deployt (PR #9), Kevins Konto freigeschaltet, `diagnose-senden` legt
   die Sendung ab, `werkzeuge/diagnose-holen.sh` holt und entpackt sie, das
   Server-Log nennt nur Gerät und Größe.
   **Upload-Limit** am 2026-11-05: 48 Sicherungen je Konto und Tag (E-B2b-12),
   Server und Kotlin (`SicherungTageslimit`); Deploy über PR nach `main`.
   **Vor echten Daten** am 2026-11-05: Kill-Switch sperrt ohne actAs, Löschschutz
   in OpenTofu (am 2026-11-05 angewendet).
   **Neue Geräte erst nach Freischaltung** (E-B2b-13) am 2026-11-05: Server
   (Merkmal am Gerät, Prüfung in allen ändernden Diensten, `GeraetFreischalten`,
   `MitWiederherstellungFreischalten`), Ableitung in `protokoll` mit gemeinsamen
   Testvektoren, Adapter, Test-Client (`freischalten <id>`, Anzeige in `geraete`).
   Deploy über PR nach `main`. Alte Testgeräte sind danach nicht freigeschaltet:
   mit neuem Gerätenamen anmelden.
   **Fragen 1–5 aus Review-Lauf 8** am 2026-11-06 entschieden und lokal umgesetzt
   (anforderungen.md Kap. 11): Diagnosen nur von freigeschalteten Geräten; erstes
   Backup eines nicht freigeschalteten Geräts nur ohne vertrautes Gerät im Konto;
   Tageslimits für Holen (50) und Löschen (20) der Sicherung und für Sitzungen
   (200 je Gerät); Interceptor prüft „gesperrt“, Sperren löscht Sitzungen in
   Stapeln; höchstens 10 offene Geräte je Konto. Server, Schnittstelle (nur
   Kommentare), Adapter und Test-Client. Deploy über PR #12 am 2026-11-06.
   **In der Cloud getestet** am 2026-11-07 (Geräte `r-pc`, `r-handy`): `r-pc`
   wird freigeschaltet, weil das Konto kein freigeschaltetes Gerät hatte, löscht
   das alte Schlüssel-Backup, sperrt die alten `cloud-*`-Geräte und sendet eine
   Diagnose. `r-handy` ist nicht freigeschaltet: Sein erstes Backup lehnt der
   Server ab, ebenso seine Diagnose; Geräte auflisten geht. Nachdem `r-pc` es
   gesperrt hat, lehnt der Server seine noch laufende Sitzung beim nächsten
   Aufruf ab (3 s später, `unauthenticated`). Log nur mit Geräte-IDs.
   Nicht in der Cloud getestet (nur Tests): Gerätelimit und Tageslimits.
   Die alte Testsicherung liegt noch im Bucket (`sicherung-loeschen` braucht
   lokale Schlüssel; geht mit „Vor echten Daten“, Punkt 1, weg).
   Egress-Alarm (`infra/alarm.tf`: Mail, wenn der Server in 23 Stunden mehr als
   1 GiB sendet, ca. 0,40 $/Monat) am 2026-11-06 angewendet (2 neu, 1 geändert,
   1 entfernt); dabei wurde auch der Kill-Switch mit den Fixes aus Lauf 8 neu gebaut.
   **Weiter mit:** die Schritte, die nur Kevin macht
   (b2b-entwurf.md, „Vor echten Daten“, Punkte 1–3 und 6); Schritt 7 (App-Einbindung, eigener Entwurf im App-Repo). Offen aus
   Lauf 5: Empfehlungen E5–E7; aus Lauf 6: E1–E6.
5. Für den B3-Entwurf aus Lauf 4: A2 (Mitgliedschaft: Server-Liste oder signiertes
   Log), A4 (Umschläge an Kontext binden, Keyset-Typen prüfen), A5 (Speicher und
   Dienste aufteilen; Anfang in B2b). A3 (Formate festschreiben) kommt in B2b.

## Befehle
```bash
cd infra && tofu plan
```

```bash
cd infra/killswitch && go test -cover ./...
```

Server lokal gegen Firestore-Emulator und fake-gcs-server (beide vorher starten,
Bucket `lokal` anlegen, siehe [entwicklungsumgebung.md](entwicklungsumgebung.md));
`PFEFFER` ist ein Zufallswert nur für diesen Lauf. Für `diagnose-senden` zusätzlich
`DIAGNOSE_KONTEN=<eigene Konto-ID>` setzen, sonst ist niemand freigeschaltet:

```bash
cd server && FIRESTORE_EMULATOR_HOST=localhost:8086 STORAGE_EMULATOR_HOST=localhost:4443 SICHERUNG_BUCKET=lokal DIAGNOSE_BUCKET=lokal PROJEKT=lokal GOOGLE_CLIENT_IDS=82416816442-vka4ve2no02681umqkn8b0rk37ri37qd.apps.googleusercontent.com PFEFFER=$(openssl rand -base64 32) go run ./cmd/server
```

Test-Client (Kotlin, B2) bauen, dann als Gerät anmelden und Schlüssel einrichten
bzw. aus dem Backup holen (weitere Befehle mit `--hilfe`):

```bash
cd client && ./gradlew :testclient:installDist
```

```bash
./client/testclient/build/install/testclient/bin/testclient --geraet pc anmelden
```

Gegen den Server in der Cloud (Adresse aus `tofu output server_url`; Gerätenamen
nicht mit lokalen Tests mischen, die Konten unterscheiden sich):

```bash
./client/testclient/build/install/testclient/bin/testclient --server "$(cd infra && tofu output -raw server_url)" --geraet cloud-pc anmelden
```

Kill-Switch von Hand auslösen (sperrt alle Cloud-Run-Dienste!):

```bash
gcloud pubsub topics publish budget-meldungen --message='{"costAmount":21,"budgetAmount":20}'
```

Nach einer Sperre wieder öffnen (erst, wenn die Ursache geklärt ist; ohne die
Variable bricht `tofu plan` mit „gesperrt“ ab):

```bash
tofu -chdir=infra apply -var=gesperrte_dienste_oeffnen=true
```

Die Variable öffnet **alle** gesperrten Dienste. Soll während der Sperre etwas
anderes geändert werden und der Dienst zu bleiben, nur die betroffenen
Ressourcen anwenden, z. B.:

```bash
tofu -chdir=infra apply -target=google_cloud_run_v2_service.server
```

Logs lesen (liegen im EU-Bucket, nicht im Standard-Bucket):

```bash
gcloud logging read 'resource.labels.service_name="killswitch"' --bucket=logs-eu --location=europe-west1 --view=_AllLogs --limit=10
```
