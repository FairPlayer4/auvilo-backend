#!/usr/bin/env bash
# Empfehlung E4 (Review-Lauf 8): Ein Cloud-Run-Dienst wird nur über das Modul
# module/oeffentlich öffentlich, damit `tofu apply` einen vom Kill-Switch
# gesperrten Dienst nicht wieder öffnet. Diese Textprüfung findet Wege daran
# vorbei, auch in neuen Dateien:
#   1. "allUsers" oder "allAuthenticatedUsers" außerhalb des Moduls,
#   2. invoker_iam_disabled (schaltet die IAM-Prüfung ganz ab; der Kill-Switch
#      sperrt dann nichts mehr),
#   3. ein Aufruf des Moduls, der `oeffnen` fest setzt statt
#      var.gesperrte_dienste_oeffnen zu übergeben, oder der kein depends_on hat.
# Kommentare (ab #) zählen nicht. Aufruf: ./pruefe-oeffnung.sh [Verzeichnis]
set -euo pipefail
verzeichnis="${1:-$(dirname "$0")}"
cd "$verzeichnis"

fehler=0
melde() {
  echo "$1"
  echo "$2" | sed 's/^/  /'
  fehler=1
}

# Alle *.tf-Dateien ohne Kommentare, Zeilen als "datei:nr:inhalt".
ohne_kommentare() {
  for f in ./*.tf ./module/*/*.tf; do
    [ -e "$f" ] || continue
    awk -v f="$f" '{ sub(/#.*/, ""); sub(/\/\/.*/, ""); print f ":" NR ":" $0 }' "$f"
  done
}
zeilen=$(ohne_kommentare)

treffer=$(echo "$zeilen" | grep -E '"all(Authenticated)?Users"' | grep -v '^\./module/oeffentlich/' || true)
[ -z "$treffer" ] || melde "Öffentliche Bindung außerhalb von module/oeffentlich:" "$treffer"

treffer=$(echo "$zeilen" | grep -E 'invoker_iam_disabled' || true)
[ -z "$treffer" ] || melde "invoker_iam_disabled schaltet die IAM-Prüfung ab (Kill-Switch wirkungslos):" "$treffer"

# Modulaufrufe: Block von `module "..." {` bis zur schließenden Klammer am
# Zeilenanfang (tofu fmt rückt so ein).
treffer=$(for f in ./*.tf; do
  [ -e "$f" ] || continue
  awk -v f="$f" '
    { sub(/#.*/, "") }
    /^module "/ { im = 1; start = NR; block = ""; next }
    im && /^}/ {
      im = 0
      if (block ~ /source *= *"\.\/module\/oeffentlich"/) {
        if (block !~ /\n *oeffnen *= *var\.gesperrte_dienste_oeffnen *\n/)
          print f ":" start ": oeffnen nicht als var.gesperrte_dienste_oeffnen übergeben"
        if (block !~ /\n *depends_on *=/)
          print f ":" start ": depends_on auf den Dienst fehlt"
      }
      next
    }
    im { block = block "\n" $0 "\n" }
  ' "$f"
done)
[ -z "$treffer" ] || melde "Aufruf von module/oeffentlich nicht wie vorgesehen:" "$treffer"

if [ "$fehler" -ne 0 ]; then
  exit 1
fi
echo "Öffnung nur über module/oeffentlich."
