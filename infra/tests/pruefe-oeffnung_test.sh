#!/usr/bin/env bash
# Tests für ../pruefe-oeffnung.sh: Jeder Fall baut ein kleines infra-Verzeichnis
# und erwartet, dass die Prüfung durchgeht (ok) oder ablehnt (abgelehnt).
# Zuerst die echte Konfiguration: Sie muss durchgehen.
set -euo pipefail
hier="$(cd "$(dirname "$0")" && pwd)"
pruefung="$hier/../pruefe-oeffnung.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fehlgeschlagen=0
pruefe() { # name erwartung(ok|abgelehnt) verzeichnis
  local ergebnis=ok
  "$pruefung" "$3" > "$tmp/ausgabe" 2>&1 || ergebnis=abgelehnt
  if [ "$ergebnis" = "$2" ]; then
    echo "ok   $1"
  else
    echo "FEHL $1: erwartet $2, war $ergebnis"
    sed 's/^/     /' "$tmp/ausgabe"
    fehlgeschlagen=1
  fi
}

# Ein Fall: Modul (immer gleich, mit "allUsers") plus die Datei main.tf.
fall() { # name erwartung inhalt-von-main.tf
  local d="$tmp/$(echo "$1" | tr -c 'a-zA-Z0-9' '_')"
  mkdir -p "$d/module/oeffentlich"
  printf 'resource "x" "y" {\n  member = "allUsers"\n}\n' > "$d/module/oeffentlich/main.tf"
  printf '%s\n' "$3" > "$d/main.tf"
  pruefe "$1" "$2" "$d"
}

gut='module "server_oeffentlich" {
  source     = "./module/oeffentlich"
  oeffnen    = var.gesperrte_dienste_oeffnen
  depends_on = [google_cloud_run_v2_service.server]
}'

pruefe "die echte Konfiguration geht durch" ok "$hier/.."
fall "Modulaufruf wie vorgesehen" ok "$gut"
fall "allUsers nur im Kommentar" ok "$gut
# Nie \"allUsers\" direkt binden."
fall "allUsers außerhalb des Moduls abgelehnt" abgelehnt "$gut
resource \"google_cloud_run_v2_service_iam_member\" \"offen\" {
  member = \"allUsers\"
}"
fall "allAuthenticatedUsers abgelehnt" abgelehnt "$gut
locals { m = [\"allAuthenticatedUsers\"] }"
fall "invoker_iam_disabled abgelehnt" abgelehnt "$gut
resource \"google_cloud_run_v2_service\" \"s\" {
  invoker_iam_disabled = true
}"
fall "oeffnen fest auf true abgelehnt" abgelehnt 'module "server_oeffentlich" {
  source     = "./module/oeffentlich"
  oeffnen    = true
  depends_on = [google_cloud_run_v2_service.server]
}'
fall "Modulaufruf ohne depends_on abgelehnt" abgelehnt 'module "server_oeffentlich" {
  source  = "./module/oeffentlich"
  oeffnen = var.gesperrte_dienste_oeffnen
}'
fall "anderes Modul ohne oeffnen geht durch" ok "$gut
module \"anderes\" {
  source = \"./module/anderes\"
}"

exit "$fehlgeschlagen"
