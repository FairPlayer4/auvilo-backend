# Entwicklungsumgebung

Stand: 2026-11-01, Windows 11. Installiert per winget (global), außer wo anders
angegeben.

| Werkzeug | Version | Zweck | Installiert über |
|---|---|---|---|
| Go | 1.27.3 | Server | `winget GoLang.Go` |
| golangci-lint | 2.15.0 | Linter-Sammlung für Go | `winget GolangCI.golangci-lint` |
| govulncheck | aktuell | bekannte Sicherheitslücken in Abhängigkeiten | `go install golang.org/x/vuln/cmd/govulncheck@latest` |
| protoc-gen-go | 1.36.12 | Go-Code aus Protobuf erzeugen | `go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12` |
| protoc-gen-connect-go | 1.21.1 | Connect-Code aus Protobuf erzeugen | `go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.21.1` |
| buf | 1.75.0 | Protobuf: Lint, Breaking-Check, Codegenerierung | `winget bufbuild.buf` |
| OpenTofu (`tofu`) | 1.13.1 | Infrastruktur als Code | `winget OpenTofu.Tofu` |
| Google Cloud CLI (`gcloud`) | 591.0.0 | GCP-Verwaltung | `winget Google.CloudSDK` (Benutzerverzeichnis) |
| gcloud-Komponenten | – | `beta`, `cloud-firestore-emulator` | `gcloud components install` |
| JDK | 21 (Microsoft) | Kotlin-Client, Firestore-Emulator | war schon da (App) |
| Docker Desktop | – | Container-Image lokal bauen/testen | war schon da |
| Gradle | 9.8.1 | Kotlin-Client bauen | nichts installieren: Wrapper `client/gradlew` |
| actionlint | 1.7.12 | GitHub-Actions-Workflows prüfen (mit shellcheck) | nichts installieren: Docker-Image `rhysd/actionlint:1.7.12` |

## Hinweise
- Nach der Installation ein **neues Terminal** öffnen, damit der `PATH` stimmt.
- `gcloud components install …` verweigert im nicht-interaktiven Modus (Claude Code)
  das Update mit dem mitgelieferten Python. Abhilfe vorher in PowerShell:
  `$env:CLOUDSDK_PYTHON = (gcloud.cmd components copy-bundled-python).Trim()`
- **Alles prüfen wie die CI:** `werkzeuge/pruefen.sh` (Git Bash, Docker Desktop muss
  laufen), vor jedem Push; `werkzeuge/pruefen.sh voll` vor einem PR nach `main`. Die CI
  läuft nur auf `main` (seit 2026-11-13). Was die Stufen prüfen, steht im Kopf des
  Skripts. Go (gofmt, vet, Tests, golangci-lint, govulncheck) und OpenTofu laufen in
  Linux-Containern mit Firestore-Emulator (`google/cloud-sdk:591.0.0-emulators`) und
  fake-gcs-server in einem eigenen Docker-Netz; Caches in den Volumes
  `avlreminder-gomod` und `avlreminder-cache`. Dauer mit warmen Caches: `schnell` etwa
  35 s, `voll` einige Minuten (Race-Detector, Image-Build).
- **Smart App Control** (Windows 11) blockiert zeitweise unsignierte Programme:
  Gos eigenes `asm.exe` (dann scheitert jedes `go build`/`go test`), `tofu.exe`, selbst
  gebaute Programme in `~/go/bin` (Code-Integrity-Protokoll, Ereignis 3077). Ausnahmen
  für einzelne Programme kennt Smart App Control nicht. Seit 2026-11-13 ist es auf
  diesem Rechner ausgeschaltet. Go und OpenTofu laufen in `pruefen.sh` und buf in
  `werkzeuge/buf.sh` trotzdem weiter in Containern: gleiche Umgebung wie die CI (Linux),
  und die Prüfung hängt nicht an der Einstellung.
- `go test -race` braucht cgo und damit einen C-Compiler, den es unter Windows hier
  nicht gibt (und den Smart App Control ebenso blockieren könnte). `pruefen.sh voll`
  testet mit `-race` im Linux-Container.
- In PowerShell Argumente mit `=` und Punkt quoten, z. B. `tofu plan "-out=.build\plan"`.
- Bei `gcloud logging read` immer `--bucket=logs-eu --location=europe-west1
  --view=_AllLogs` angeben – der Standard-Bucket ist absichtlich leer.
- OpenTofu-Provider aktualisiert Dependabot nicht (nur Terraform-Registry). Stattdessen
  meldet der Workflow `provider-check.yml` monatlich neue Versionen (fehlgeschlagener
  Lauf = Mail von GitHub). Aktualisiert wird von Hand mit `tofu init -upgrade` in
  `infra/`; ein neues Major erst nach Anheben der Grenze in `infra/versionen.tf`.
- Die Versionen von buf und den beiden Plugins stehen auch in `.github/workflows/ci.yml`
  (Job `proto`). Andere Versionen erzeugen anderen Code; dann meldet die CI „erzeugter
  Code nicht aktuell“. Beim Aktualisieren beide Stellen ändern.
- Nach Änderungen an `.github/workflows/` actionlint ausführen (steckt auch in
  `pruefen.sh voll`, nicht in der CI), im Wurzelverzeichnis des Repos in Git Bash:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/repo" -w /repo rhysd/actionlint:1.7.12 -no-color -oneline`
  (PowerShell: `docker run --rm -v "${PWD}:/repo" -w /repo rhysd/actionlint:1.7.12 -no-color -oneline`).
  Ohne Ausgabe und mit Exit-Code 0 ist alles in Ordnung.
- Code neu erzeugen nach Änderungen an `proto/`: `werkzeuge/buf.sh` (Git Bash, Docker
  Desktop muss laufen). Das Skript führt `buf generate` in einem Image mit buf und den
  Plugins aus; weitere Befehle gehen durch, z. B. `werkzeuge/buf.sh lint`. Die
  Versionen liest es aus `ci.yml` und `server/go.mod`, ein neues Image entsteht nur
  bei geänderten Versionen (das erste Mal etwa 10 Minuten). Grund: Smart App Control
  in Windows 11 blockiert die selbst gebauten Plugins in `~/go/bin` zeitweise
  (Code-Integrity-Protokoll, Ereignis 3077); auch `gopls.exe` ist betroffen.
  Lokal installiert geht `cd proto && buf generate` weiterhin, solange Windows es zulässt.
- Firestore-Emulator für die Server-Tests (in einem eigenen Terminal):
  `gcloud emulators firestore start --host-port=localhost:8086`, dann
  `FIRESTORE_EMULATOR_HOST=localhost:8086 go test ./...` in `server/`. Ohne die
  Variable werden die Firestore-Fälle übersprungen; in der CI sind sie Pflicht.
- Nachbau von Cloud Storage für die Datensicherungen (Docker Desktop muss laufen):
  `docker run -d --rm --name fakegcs -p 4443:4443 fsouza/fake-gcs-server:1.56.1 -scheme http -port 4443 -backend memory`,
  dann zusätzlich `STORAGE_EMULATOR_HOST=localhost:4443`. `-backend memory` ist
  nötig, weil nur dort die Versionierung nachgebildet ist. Für den lokalen Server
  den Bucket einmal anlegen:
  `curl -X POST "http://localhost:4443/storage/v1/b?project=lokal" -H "Content-Type: application/json" -d '{"name":"lokal","versioning":{"enabled":true}}'`.
  Stoppen mit `docker stop fakegcs` (Inhalt ist dann weg).
- Netzwerkzugriffe (winget, `go install`, `gcloud`, `git push`) laufen in Claude Code
  außerhalb der Sandbox.
- Kotlin-Client (`client/`): `./gradlew check` baut und testet alle Module (`protokoll`, `verbindung`, `testclient`). Der
  Code aus den .proto-Dateien entsteht dabei im Build und wird nicht eingecheckt.
  Gradle-Cache auf diesem Rechner: `GRADLE_USER_HOME=E:\caches\gradle`.
- Gradle prüft jede Abhängigkeit gegen `client/gradle/verification-metadata.xml`
  (SHA-256). Nach einem Versionswechsel, auch in einem Dependabot-PR, die Datei neu
  erzeugen, sonst bricht der Build ab:
  `./gradlew --write-verification-metadata sha256 check :testclient:installDist`,
  und zwar mit leerem `GRADLE_USER_HOME` (sonst fehlen Dateien, die schon im Cache
  liegen, und erst die CI merkt es). Danach den Diff ansehen: Es dürfen nur die geänderten Versionen dazukommen.
  `protoc` ist ein Programm je Plattform; Gradle trägt nur die Windows-Variante ein.
  Die Linux-Variante für die CI von Hand ergänzen (SHA-256 der Datei von Maven
  Central, SHA-1 mit der `.sha1`-Datei dort abgleichen).
- Den regelmäßigen Lauf (fällige Konto-Löschungen) lokal einmal ausführen, mit
  denselben Emulatoren und Variablen wie den Server (ohne Pfeffer und Client-IDs):
  `PROJEKT=lokal SICHERUNG_BUCKET=lokal DIAGNOSE_BUCKET=lokal go run ./cmd/server aufraeumen`
  in `server/`. In der Cloud läuft er als Cloud Run Job.
- Test-Client gegen den lokalen Server:
  `./gradlew :testclient:run --args="--geraet pc anmelden"` (Befehle mit `--hilfe`).
  Er nutzt die `client_secret_*.json` in `%APPDATA%\avlreminder` und legt
  Geräteschlüssel und Schlüssel in `%APPDATA%\avlreminder\testclient` ab.
  Für die Datensicherung braucht der lokale Server zusätzlich
  `STORAGE_EMULATOR_HOST=localhost:4443` und `SICHERUNG_BUCKET=lokal` (Bucket wie
  oben anlegen), für Diagnosen `DIAGNOSE_BUCKET=lokal` (derselbe Bucket reicht) und
  in `DIAGNOSE_KONTEN` die Konto-IDs mit Diagnose-Erlaubnis (leer = niemand).
