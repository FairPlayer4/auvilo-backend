#!/usr/bin/env bash
# Führt buf mit den Plugins in Docker aus, statt die Programme lokal zu starten.
# Grund: Smart App Control in Windows 11 blockiert die selbst gebauten,
# unsignierten Plugins in ~/go/bin zeitweise (seit 2026-11-08 beobachtet).
#
# Die Versionen kommen aus .github/workflows/ci.yml (Job "proto") und
# server/go.mod; das Image heißt nach ihnen und wird nur gebaut, wenn es mit
# diesen Versionen noch nicht existiert (erstes Mal etwa 10 Minuten).
#
# Aufruf im Repo, in Git Bash (Docker Desktop muss laufen):
#   werkzeuge/buf.sh                       # = buf generate
#   werkzeuge/buf.sh lint
#   werkzeuge/buf.sh format -d --exit-code
#   werkzeuge/buf.sh breaking --against '../.git#ref=origin/main,subdir=proto'
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
ci="$repo/.github/workflows/ci.yml"

version() { # Wert einer Zeile "NAME: wert" aus ci.yml
  local v
  v="$(sed -n "s/^ *$1: *\(v[0-9.]*\) *$/\1/p" "$ci" | head -n 1)"
  [ -n "$v" ] || { echo "$1 nicht in $ci gefunden" >&2; exit 1; }
  echo "$v"
}
buf_v="$(version BUF_VERSION)"
go_plugin_v="$(version PROTOC_GEN_GO_VERSION)"
connect_v="$(version PROTOC_GEN_CONNECT_GO_VERSION)"
go_v="$(sed -n 's/^go \([0-9.]*\)$/\1/p' "$repo/server/go.mod")"
[ -n "$go_v" ] || { echo "Go-Version nicht in server/go.mod gefunden" >&2; exit 1; }

image="avlreminder-buf:go${go_v}-buf${buf_v}-pg${go_plugin_v}-cg${connect_v}"
if ! docker image inspect "$image" > /dev/null 2>&1; then
  echo "Baue $image (einmalig) …" >&2
  docker build -q -t "$image" \
    --build-arg GO_VERSION="$go_v" \
    --build-arg BUF_VERSION="$buf_v" \
    --build-arg PROTOC_GEN_GO_VERSION="$go_plugin_v" \
    --build-arg PROTOC_GEN_CONNECT_GO_VERSION="$connect_v" \
    "$repo/werkzeuge/buf" > /dev/null
fi

# Docker unter Windows braucht den Pfad in Windows-Schreibweise (pwd -W);
# MSYS_NO_PATHCONV verhindert, dass Git Bash /repo umschreibt.
quelle="$repo"
if (cd "$repo" && pwd -W) > /dev/null 2>&1; then quelle="$(cd "$repo" && pwd -W)"; fi
[ $# -gt 0 ] || set -- generate
MSYS_NO_PATHCONV=1 exec docker run --rm -v "$quelle:/repo" "$image" "$@"
