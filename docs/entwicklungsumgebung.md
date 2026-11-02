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

## Hinweise
- Nach der Installation ein **neues Terminal** öffnen, damit der `PATH` stimmt.
- `gcloud components install …` verweigert im nicht-interaktiven Modus (Claude Code)
  das Update mit dem mitgelieferten Python. Abhilfe vorher in PowerShell:
  `$env:CLOUDSDK_PYTHON = (gcloud.cmd components copy-bundled-python).Trim()`
- `go test -race` braucht cgo und damit einen C-Compiler, den es unter Windows hier
  nicht gibt. Lokal ohne `-race` testen; die CI (Linux) testet mit `-race`.
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
- Code neu erzeugen nach Änderungen an `proto/`: `cd proto && buf generate`.
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
- Kotlin-Client (`client/`): `./gradlew check` baut und testet beide Module. Der
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
- Test-Client gegen den lokalen Server:
  `./gradlew :testclient:run --args="--geraet pc anmelden"` (Befehle mit `--hilfe`).
  Er nutzt die `client_secret_*.json` in `%APPDATA%\avlreminder` und legt
  Geräteschlüssel und Schlüssel in `%APPDATA%\avlreminder\testclient` ab.
