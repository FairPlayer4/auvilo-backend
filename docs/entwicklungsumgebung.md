# Entwicklungsumgebung

Stand: 2026-11-01, Windows 11. Installiert per winget (global), außer wo anders
angegeben.

| Werkzeug | Version | Zweck | Installiert über |
|---|---|---|---|
| Go | 1.27.3 | Server | `winget GoLang.Go` |
| golangci-lint | 2.15.0 | Linter-Sammlung für Go | `winget GolangCI.golangci-lint` |
| govulncheck | aktuell | bekannte Sicherheitslücken in Abhängigkeiten | `go install golang.org/x/vuln/cmd/govulncheck@latest` |
| protoc-gen-go, protoc-gen-connect-go | aktuell | Go-Code aus Protobuf erzeugen | `go install …` (liegen in `%USERPROFILE%\go\bin`) |
| buf | 1.75.0 | Protobuf: Lint, Breaking-Check, Codegenerierung | `winget bufbuild.buf` |
| OpenTofu (`tofu`) | 1.13.1 | Infrastruktur als Code | `winget OpenTofu.Tofu` |
| Google Cloud CLI (`gcloud`) | 591.0.0 | GCP-Verwaltung | `winget Google.CloudSDK` (Benutzerverzeichnis) |
| gcloud-Komponenten | – | `beta`, `cloud-firestore-emulator` | `gcloud components install` |
| JDK | 21 (Microsoft) | Kotlin-Client, Firestore-Emulator | war schon da (App) |
| Docker Desktop | – | Container-Image lokal bauen/testen | war schon da |

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
- Netzwerkzugriffe (winget, `go install`, `gcloud`, `git push`) laufen in Claude Code
  außerhalb der Sandbox.
