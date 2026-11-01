#!/usr/bin/env bash
# Anforderung D1: Findet fest eingetragene Standorte in den *.tf-Dateien.
# Erlaubt sind nur var.region oder ein Verweis auf den Standort einer anderen
# Ressource (z. B. google_artifact_registry_repository.container.location).
# Ergänzt den tofu-Test (tests/eu_regionen.tftest.hcl), der nur die heute
# bekannten Ressourcen prüft; diese Prüfung erfasst auch neue Ressourcen.
set -euo pipefail
cd "$(dirname "$0")"

treffer=$(grep -nE '^\s*(location|location_id|region|trigger_region)\s*=' ./*.tf \
  | grep -vE '=\s*(var\.region|[a-z0-9_]+\.[a-z0-9_]+\.location)\s*(#.*)?$' || true)

if [ -n "$treffer" ]; then
  echo "Fest eingetragene Standorte gefunden – bitte var.region verwenden:"
  echo "$treffer"
  exit 1
fi
echo "Keine fest eingetragenen Standorte."
