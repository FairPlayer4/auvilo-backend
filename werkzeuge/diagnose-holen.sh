#!/usr/bin/env bash
# Holt die Diagnosen aus der App (docs/diagnose-entwurf.md, E-D-6) nach
# .diagnose/ im Repo (steht in .gitignore) und entpackt sie:
#
#   .diagnose/roh/<konto_id>/<zeit>-<zufall>.jsonl.gz   wie im Bucket
#   .diagnose/<konto_id>/<zeit>-<zufall>.jsonl          entpackt, zum Lesen
#
# Die lokale Kopie folgt dem Bucket: Was Google nach 14 Tagen löscht, löscht
# dieses Skript beim nächsten Lauf auch hier (roh und entpackt).
#
# Aufruf (angemeldet mit `gcloud auth login`):
#   werkzeuge/diagnose-holen.sh [projekt]
set -euo pipefail

projekt="${1:-avlreminder-exp-1ef2}"
bucket="gs://${projekt}-diagnose"
ziel="$(cd "$(dirname "$0")/.." && pwd)/.diagnose"
roh="$ziel/roh"
mkdir -p "$roh"

# rsync lädt nur, was fehlt; --delete-unmatched-destination-objects löscht
# lokal, was im Bucket nicht mehr liegt.
gcloud storage rsync "$bucket/diagnose" "$roh" --recursive \
  --delete-unmatched-destination-objects --project "$projekt"

neu=0
while IFS= read -r -d '' gz; do
  rel="${gz#"$roh"/}"
  jsonl="$ziel/${rel%.gz}"
  if [ ! -f "$jsonl" ]; then
    mkdir -p "$(dirname "$jsonl")"
    # Erst in eine Zwischendatei: Ein abgebrochenes Entpacken bleibt sonst als
    # halbe Datei liegen und würde nie neu entpackt.
    gzip -dc "$gz" > "$jsonl.teil"
    mv "$jsonl.teil" "$jsonl"
    neu=$((neu + 1))
  fi
done < <(find "$roh" -type f -name '*.jsonl.gz' -print0)

# Entpackte Dateien ohne Gegenstück im Bucket löschen (14-Tage-Frist).
alt=0
while IFS= read -r -d '' jsonl; do
  rel="${jsonl#"$ziel"/}"
  if [ ! -f "$roh/$rel.gz" ]; then
    rm -f "$jsonl"
    alt=$((alt + 1))
  fi
done < <(find "$ziel" -path "$roh" -prune -o -type f -name '*.jsonl' -print0)
find "$ziel" -mindepth 1 -type d -empty -delete

echo "Diagnosen in $ziel: $neu neu entpackt, $alt abgelaufene gelöscht."
