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
- Netzwerkzugriffe (winget, `go install`, `gcloud`, `git push`) laufen in Claude Code
  außerhalb der Sandbox.
