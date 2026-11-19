# Rechte der Betroffenen (Art. 12–22 DSGVO): Abläufe

Entwurf, Stand 2026-11-14. Gilt für Anfragen an den Betreiber (Kevin). Was die App
selbst kann, ist der Normalweg; die Abläufe hier sind für alles andere.

## Allgemein (Art. 12)
- **Frist:** ohne Verzögerung, spätestens **ein Monat** nach Eingang; bei Bedarf
  um zwei Monate verlängerbar, mit Begründung innerhalb des ersten Monats.
- **Kostenlos.**
- **Identität:** Der Server kennt weder Namen noch E-Mail-Adresse. Eine Anfrage lässt
  sich nur über die **Konto-ID** zuordnen, die die App unter *Einstellungen → Cloud-Backup (experimentell)*
  zum Kopieren anzeigt (ab V1.3c2, sobald das Gerät eingerichtet ist),
  oder über die Kennung, falls die Person auf der Freigabeliste steht. Zweifel an der
  Identität: zusätzliche Bestätigung über die App verlangen (Art. 12 Abs. 6). Wer
  weder App noch Google-Konto hat, lässt sich nicht zuordnen (Art. 11 Abs. 2); das
  wird der Person so mitgeteilt, und die Frist aus P5 löscht.
- **Nachweis:** jede Anfrage mit Datum, Art, Antwort und Erledigung in einer privaten
  Liste außerhalb des Repos; nach drei Jahren löschen (Verjährung).

## Auskunft (Art. 15) und Datenübertragbarkeit (Art. 20)
Bis es eine Funktion dafür gibt (B2 in Kap. 3.5), von Hand:
1. Konto-ID der Person feststellen (siehe oben).
2. In Firestore lesen: `konten/<id>`, `geraete` und `sitzungen` mit `konto_id == <id>`,
   `backups/<id>` (nur Version und Zeit, nicht das Chiffrat), Kennung aus
   `google_kennungen` mit `konto_id == <id>`.
3. Buckets: Fassungen der Datensicherung (Version, Zeit, Größe) mit
   `gcloud storage ls --all-versions -l`; Diagnosen unter `diagnose/<id>/`.
4. Logs der letzten 30 Tage mit der Konto-ID und den Geräte-IDs (`gcloud logging read`
   im Bucket `logs-eu`).
5. Antwort mit den Angaben aus Art. 15 Abs. 1 (Zwecke, Kategorien, Empfänger, Fristen,
   Rechte, Herkunft) – sie stehen in der Datenschutzerklärung; dazu die Daten als
   JSON. Chiffrate auf Wunsch mit; entschlüsseln kann sie nur die Person.

Die Inhalte (Erinnerungen) hat die Person ohnehin in der App; deren Export ist eine
Funktion der App.

## Berichtigung (Art. 16)
Der Server speichert keine Angaben, die falsch sein können (nur Zufalls-IDs,
Zeitpunkte, Chiffrate). Inhalte berichtigt die Person in der App.

## Löschung (Art. 17)
- **Normalweg:** *Konto löschen* in der App (V1.3c2, noch nicht installiert). Von einem
  freigeschalteten Gerät sofort, sonst nach 7 Tagen Wartezeit (B1 in Kap. 3.5).
  *Sicherung löschen* löscht nur die Datensicherung.
- **Von Hand** (z. B. App nicht mehr installiert, Person aber per Google-Konto
  zuordenbar): Termin in `konten/<id>` setzen (`loeschen_ab` = jetzt) und den Job
  `aufraeumen` starten (`gcloud run jobs execute aufraeumen --region=europe-west1`).
  Prüfen, dass das Konto weg ist (Log „Konto gelöscht (Lauf)“).
- **Danach zusätzlich:**
  - Steht die Kennung auf der Freigabeliste und will die Person nicht mehr
    teilnehmen: aus `freigegebene_kennungen` austragen, `tofu apply`, alte
    Revisionen des Dienstes löschen (sie enthalten die alte Liste).
  - Konto-ID aus `diagnose_konten` austragen.
  - Hatte die Person eine Diagnose-Erlaubnis: `werkzeuge/diagnose-holen.sh` laufen
    lassen, damit `.diagnose/` folgt; Claude-Gesprächsverläufe, in denen ihre
    Diagnosen gelesen wurden, löschen (erst relevant mit P9).
- **Was bleibt:** Logs bis 30 Tage, Tageszähler wenige Tage (README K4, K5). Das steht
  in der Datenschutzerklärung.

## Einschränkung (Art. 18)
Auf Wunsch den Zugang sperren, ohne zu löschen: alle Geräte des Kontos sperren
(Test-Client `sperren` bzw. Firestore `gesperrt = true`). Daten bleiben, bis die
Einschränkung endet.

## Widerspruch (Art. 21)
Betrifft V5 (Logs, Missbrauchsschutz, lit. f). Die Logs dienen der Abwehr von
Angriffen und laufen nach 30 Tagen ab; einzeln löschen geht nicht. Einem Widerspruch
wird in der Regel mit Verweis auf diese zwingenden Gründe nicht gefolgt; wer keine
Logs will, nutzt das Backup nicht. Jeden Widerspruch trotzdem einzeln prüfen.

## Widerruf der Einwilligung (Art. 7 Abs. 3)
Diagnosen (V4): Die Einwilligung gilt je Sendung; wer nicht mehr senden will,
drückt den Knopf nicht mehr. Schon gesendete Diagnosen löscht Kevin auf Wunsch
(`gcloud storage rm -r gs://…-diagnose/diagnose/<id>/`), sonst laufen sie nach
14 Tagen ab.

## Beschwerde (Art. 77)
Die Datenschutzerklärung nennt das Recht, sich bei einer Aufsichtsbehörde zu beschweren.
