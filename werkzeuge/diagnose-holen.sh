#!/usr/bin/env bash
# Holt die Diagnosen aus der App (docs/diagnose-entwurf.md, E-D-6) nach
# .diagnose/ im Repo (steht in .gitignore) und entpackt sie:
#
#   .diagnose/roh/diagnose/<konto_id>/<zeit>-<zufall>.jsonl.gz   wie im Bucket
#   .diagnose/<konto_id>/<zeit>-<zufall>.jsonl                   entpackt, zum Lesen
#
# Die lokale Kopie folgt dem Bucket: Was Google nach 14 Tagen löscht, löscht
# dieses Skript beim nächsten Lauf auch hier (roh und entpackt). Läuft es
# länger nicht, bleiben die lokalen Kopien so lange liegen.
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
# lokal, was im Bucket nicht mehr liegt. Synchronisiert wird der ganze Bucket,
# nicht nur diagnose/: Gibt es dort (noch oder nicht mehr) keine Sendung,
# bricht rsync sonst ab, statt lokal zu löschen.
gcloud storage rsync "$bucket" "$roh" --recursive \
  --delete-unmatched-destination-objects --project "$projekt"

# Erst aufräumen: Entpackte Dateien ohne Gegenstück im Bucket löschen
# (14-Tage-Frist), dazu Reste abgebrochener Läufe (.teil).
alt=0
while IFS= read -r -d '' datei; do
  rel="${datei#"$ziel"/}"
  if [ "${datei%.teil}" != "$datei" ] || [ ! -f "$roh/diagnose/$rel.gz" ]; then
    rm -f "$datei"
    alt=$((alt + 1))
  fi
done < <(find "$ziel" -path "$roh" -prune -o -type f \( -name '*.jsonl' -o -name '*.jsonl.teil' \) -print0)

# Dann entpacken. Eine kaputte Sendung (der Server prüft nur den gzip-Kopf)
# wird gemeldet und übersprungen, damit sie nicht jeden Lauf abbricht.
neu=0
kaputt=0
while IFS= read -r -d '' gz; do
  rel="${gz#"$roh"/diagnose/}"
  jsonl="$ziel/${rel%.gz}"
  if [ ! -f "$jsonl" ]; then
    mkdir -p "$(dirname "$jsonl")"
    # Erst in eine Zwischendatei: Ein abgebrochenes Entpacken bleibt sonst als
    # halbe Datei liegen und würde nie neu entpackt.
    if gzip -dc "$gz" > "$jsonl.teil"; then
      mv "$jsonl.teil" "$jsonl"
      neu=$((neu + 1))
    else
      rm -f "$jsonl.teil"
      echo "Kaputte Sendung übersprungen: $gz" >&2
      kaputt=$((kaputt + 1))
    fi
  fi
done < <(find "$roh/diagnose" -type f -name '*.jsonl.gz' -print0 2>/dev/null || true)
find "$ziel" -mindepth 1 -type d -empty -delete

echo "Diagnosen in $ziel: $neu neu entpackt, $kaputt kaputt, $alt abgelaufene gelöscht."
