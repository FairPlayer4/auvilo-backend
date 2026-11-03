#!/usr/bin/env bash
# Anforderung D1: Findet fest eingetragene Standorte in den *.tf-Dateien.
# Erlaubt sind nur var.region, [var.region] (Listen wie
# allowed_persistence_regions) oder ein Verweis auf den Standort einer anderen
# Ressource (z. B. google_artifact_registry_repository.container.location).
# Ergänzt den tofu-Test (tests/eu_regionen.tftest.hcl), der nur die heute
# bekannten Ressourcen prüft; diese Prüfung erfasst auch neue Ressourcen.
set -euo pipefail
cd "$(dirname "$0")"

attribute='location|location_id|locations|region|regions|trigger_region|allowed_persistence_regions'
erlaubt='var\.region|\[\s*var\.region\s*\]|[a-z0-9_]+\.[a-z0-9_]+\.location'
treffer=$(grep -nE "^\s*($attribute)\s*=" ./*.tf \
  | grep -vE "=\s*($erlaubt)\s*(#.*)?$" || true)

if [ -n "$treffer" ]; then
  echo "Fest eingetragene Standorte gefunden – bitte var.region verwenden:"
  echo "$treffer"
  exit 1
fi
echo "Keine fest eingetragenen Standorte."
