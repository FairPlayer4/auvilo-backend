#!/usr/bin/env bash
# Prüft das Repo lokal wie die CI (.github/workflows/ci.yml und der Image-Test aus
# deploy.yml). Die CI läuft nur noch auf main (Entscheidung 2026-11-13); alles
# davor muss dieses Skript abfangen.
#
# Aufruf im Repo, in Git Bash (Docker Desktop muss laufen):
#   werkzeuge/pruefen.sh          # = schnell, vor jedem Push
#   werkzeuge/pruefen.sh voll     # vor jedem PR nach main und für Dependabot-Branches
#
# schnell: Go (server, infra/killswitch): gofmt, vet, Tests mit Firestore-Emulator
#          und fake-gcs-server (Pflicht, nichts wird übersprungen), golangci-lint;
#          Kotlin: gradlew check; OpenTofu: fmt und die beiden Prüfskripte.
# voll:    Go-Tests stattdessen mit Race-Detector, dazu govulncheck, buf (Lint,
#          Format, erzeugter Code aktuell, keine inkompatiblen Änderungen gegenüber
#          origin/main), tofu validate und tofu test, Server-Image bauen und
#          starten, actionlint.
#
# Go und OpenTofu laufen in Linux-Containern, wie in der CI. Smart App Control
# blockierte unter Windows zeitweise die Go-Werkzeuge (asm.exe), tofu.exe und
# selbst gebaute Programme (seit 2026-11-13 aus), und -race braucht cgo.
# Kotlin läuft direkt unter Windows (Java ist signiert).
#
# Ein roter Schritt bricht nicht ab; am Ende steht eine Übersicht, Exit-Code 1,
# wenn etwas rot war. Die Emulatoren laufen in Containern in einem eigenen Netz,
# stören also eigene auf 8086/4443 nicht, und werden am Ende entfernt.
# Caches (Go-Module, Build, Provider) liegen in Docker-Volumes "avlreminder-*".
set -uo pipefail

modus="${1:-schnell}"
case "$modus" in schnell | voll) ;; *)
  echo "Aufruf: $0 [schnell|voll]" >&2
  exit 2
  ;;
esac

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo" || exit 2
docker version > /dev/null 2>&1 || {
  echo "Docker läuft nicht (Docker Desktop starten)." >&2
  exit 2
}

# Docker unter Windows braucht den Pfad in Windows-Schreibweise (pwd -W).
quelle="$repo"
if pwd -W > /dev/null 2>&1; then quelle="$(pwd -W)"; fi
export MSYS_NO_PATHCONV=1 # Git Bash soll /repo usw. nicht umschreiben

# Versionen wie in ci.yml und docs/entwicklungsumgebung.md.
go_v="$(sed -n 's/^go \([0-9.]*\)$/\1/p' server/go.mod)"
go_image="golang:$go_v"
lint_image=golangci/golangci-lint:v2.15.0
tofu_image=ghcr.io/opentofu/opentofu:1.13.1
fs_image=google/cloud-sdk:591.0.0-emulators
gcs_image=fsouza/fake-gcs-server:1.56.1@sha256:797ce226d62f947c009dc40246b30cfb456b8473d8241407f9d6f2c04e4d69ef
netz=avlreminder-pruefen

aufraeumen() {
  docker rm -f avl-pruefen-firestore avl-pruefen-gcs > /dev/null 2>&1 || true
  docker network rm "$netz" > /dev/null 2>&1 || true
}
trap aufraeumen EXIT

ergebnisse=()
rot=0
# schritt NAME BEFEHL… – führt den Befehl in einer Subshell mit set -e aus.
schritt() {
  local name="$1" start=$SECONDS
  shift
  echo
  echo "==> $name"
  # Nicht als if-Bedingung: Dort wäre set -e in der Subshell wirkungslos.
  (set -e; "$@")
  if [ $? -eq 0 ]; then
    ergebnisse+=("ok      $name ($((SECONDS - start)) s)")
  else
    ergebnisse+=("FEHLER  $name ($((SECONDS - start)) s)")
    rot=1
  fi
}

# Container mit dem Repo (nur lesend) und den gemeinsamen Caches.
# EINSTIEG=sh ersetzt den Einstiegspunkt des Images (beim OpenTofu-Image "tofu").
im_container() { # Image, Arbeitsverzeichnis im Repo, Befehl…
  local image="$1" dir="$2" einstieg=()
  shift 2
  [ -z "${EINSTIEG:-}" ] || einstieg=(--entrypoint "$EINSTIEG")
  docker run "${einstieg[@]}" --rm --network "$netz" -v "$quelle:/repo:ro" -w "/repo/$dir" \
    -v avlreminder-gomod:/go/pkg/mod -v avlreminder-cache:/root/.cache \
    -e FIRESTORE_EMULATOR_HOST=avl-pruefen-firestore:8086 \
    -e STORAGE_EMULATOR_HOST=avl-pruefen-gcs:4443 -e EMULATOR_PFLICHT=1 \
    -e TF_DATA_DIR=/tmp/tofu -e TF_PLUGIN_CACHE_DIR=/root/.cache/tofu-plugins \
    "$image" "$@"
}

emulatoren() {
  aufraeumen
  docker network create "$netz" > /dev/null
  docker run -d --name avl-pruefen-firestore --network "$netz" \
    "$fs_image" gcloud emulators firestore start --host-port=0.0.0.0:8086 > /dev/null
  docker run -d --name avl-pruefen-gcs --network "$netz" \
    "$gcs_image" -scheme http -port 4443 -backend memory > /dev/null
  # Bereit, wenn beide antworten (geprüft aus einem Container im selben Netz).
  im_container "$go_image" . sh -c '
    for i in $(seq 90); do
      curl -sf http://avl-pruefen-firestore:8086/ > /dev/null &&
        curl -sf http://avl-pruefen-gcs:4443/storage/v1/b > /dev/null && exit 0
      sleep 1
    done
    exit 1' || {
    docker logs avl-pruefen-firestore
    docker logs avl-pruefen-gcs
    return 1
  }
}

go_modul() { # Verzeichnis
  local race=""
  [ "$modus" = voll ] && race="-race"
  im_container "$go_image" "$1" sh -c "
    set -e
    u=\$(gofmt -l .); [ -z \"\$u\" ] || { echo \"Nicht formatiert: \$u\"; exit 1; }
    go vet ./...
    go test $race -cover ./...
  "
  im_container "$lint_image" "$1" golangci-lint run --config /repo/.golangci.yml
  if [ "$modus" = voll ]; then
    im_container "$go_image" "$1" go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
  fi
}

# fmt nur für eingecheckte Dateien wie in der CI (werte.auto.tfvars ist lokal).
tofu_schnell() {
  local dateien
  mapfile -t dateien < <(cd infra && git ls-files -- '*.tf' '*.tfvars' '*.tftest.hcl')
  EINSTIEG=sh im_container "$tofu_image" infra -c '
    set -e
    tofu fmt -check "$@"
    ./pruefe-regionen.sh
    ./pruefe-oeffnung.sh
    ./tests/pruefe-oeffnung_test.sh' sh "${dateien[@]}"
}

# Eigenes Datenverzeichnis (TF_DATA_DIR): Das echte .terraform mit GCS-Backend
# bleibt unberührt, die Lock-Datei auch.
tofu_voll() {
  EINSTIEG=sh im_container "$tofu_image" infra -c '
    set -e
    mkdir -p "$TF_PLUGIN_CACHE_DIR"
    tofu init -backend=false -input=false -lockfile=readonly > /dev/null
    tofu validate
    tofu test'
}

# Schnittstelle (buf in Docker, siehe werkzeuge/buf.sh).
buf_pruefen() {
  werkzeuge/buf.sh lint
  werkzeuge/buf.sh format -d --exit-code
  werkzeuge/buf.sh generate
  local diff
  diff="$(git status --porcelain -- server/gen)"
  [ -z "$diff" ] || {
    echo "server/gen ist nicht aktuell oder nicht eingecheckt:"
    echo "$diff"
    return 1
  }
  git fetch -q origin main ||
    echo "Warnung: git fetch fehlgeschlagen, vergleiche mit dem letzten Stand von origin/main"
  if [ -z "$(git ls-tree --full-tree origin/main proto)" ]; then
    echo "origin/main hat noch kein proto/ – breaking übersprungen"
    return 0
  fi
  werkzeuge/buf.sh breaking --against '../.git#ref=origin/main,subdir=proto'
}

# Server-Image wie im Deploy: ohne Konfiguration muss es mit dieser Meldung abbrechen.
image_pruefen() {
  docker build -q -t avlreminder-server:pruefen server > /dev/null
  local ausgabe
  ausgabe="$(docker run --rm avlreminder-server:pruefen 2>&1 || true)"
  echo "$ausgabe"
  grep -q "PROJEKT fehlt" <<< "$ausgabe"
}

actionlint_pruefen() {
  docker run --rm -v "$quelle:/repo:ro" -w /repo rhysd/actionlint:1.7.12 -no-color -oneline
}

[ -z "$(git status --porcelain)" ] ||
  echo "Hinweis: Es gibt uncommittete Änderungen; geprüft wird der Arbeitsbaum."

schritt "Emulatoren starten" emulatoren
schritt "Go – server" go_modul server
schritt "Go – infra/killswitch" go_modul infra/killswitch
schritt "Kotlin – client (gradlew check)" sh -c 'cd client && ./gradlew check --console=plain -q'
schritt "OpenTofu – fmt und Prüfskripte" tofu_schnell
if [ "$modus" = voll ]; then
  schritt "OpenTofu – validate und test" tofu_voll
  schritt "Schnittstelle (buf)" buf_pruefen
  schritt "Server-Image bauen und starten" image_pruefen
  schritt "actionlint" actionlint_pruefen
fi

echo
echo "=== Ergebnis ($modus)"
printf '%s\n' "${ergebnisse[@]}"
exit "$rot"
