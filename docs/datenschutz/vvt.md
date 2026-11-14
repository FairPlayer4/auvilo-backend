# Verzeichnis der Verarbeitungstätigkeiten (Art. 30 DSGVO)

Entwurf, Stand 2026-11-14. Umfasst das Backend (GCP-Projekt `avlreminder-exp-1ef2`,
Region `europe-west1`). Was die App nur auf dem Gerät verarbeitet, ist keine
Verarbeitung des Betreibers und steht hier nicht; es gehört in die
Datenschutzerklärung (Abschnitt „Auf dem Gerät“).

## Allgemeine Angaben
- **Verantwortlicher:** [Name, Anschrift, E-Mail] (Privatperson, Betreiber der App
  avlreminder). Kein Datenschutzbeauftragter: Art. 37 trifft nicht zu, § 38 Abs. 1
  BDSG verlangt ihn erst ab 20 Personen, **oder** wenn eine Verarbeitung einer
  Pflicht-DSFA unterliegt (Satz 2). Deshalb hält [dsfa.md](dsfa.md) fest, ob die DSFA
  Pflicht oder freiwillig ist.
- **Auftragsverarbeiter:** Google Cloud (Vertragspartner: [laut Cloud Console, z. B.
  Google Cloud EMEA Limited, Irland – prüfen]), Grundlage: Cloud Data Processing
  Addendum (CDPA) [akzeptiert am …]. Unterauftragsverarbeiter von Google nach dessen
  Liste; Übermittlungen in Drittländer (v. a. Google LLC, USA) über die
  Standardvertragsklauseln im CDPA und das EU-US Data Privacy Framework.
- **Keine weiteren Empfänger.** GitHub und Anthropic bekommen keine Daten von Nutzern
  (Ausnahme Diagnosen, V4).
- **Technische und organisatorische Maßnahmen:** [toms.md](toms.md).

## Datenbestände
Damit die Tätigkeiten unten kurz bleiben, hier einmal, was wo liegt.

| Ort | Inhalt | Personenbezug | Frist |
|---|---|---|---|
| Firestore `google_kennungen` | Kennung (HMAC-SHA256 aus Pfeffer und Google-Konto-ID), Konto-ID | Pseudonym | bis Konto löschen |
| Firestore `konten` | Konto-ID (128 Bit Zufall), Tag der Erstellung, Termin einer beantragten Löschung | Pseudonym | bis Konto löschen; P5: 24 Monate ohne Anmeldung (offen, README K2) |
| Firestore `geraete` | Geräte-ID, Konto-ID, öffentlicher Geräteschlüssel, gesperrt/freigeschaltet, Zeitpunkte | Pseudonym | bis Konto löschen; P5: gesperrt 90 Tage (offen) |
| Firestore `sitzungen` | Hash des Tokens, Konto-ID, Geräte-ID, Ablauf | Pseudonym | 1 Stunde, dann TTL |
| Firestore `backups` | Schlüssel-Backup (Chiffrat), Version, öffentlicher Freischaltschlüssel | Pseudonym, Inhalt verschlüsselt | bis Konto oder Backup löschen |
| Firestore `kontingente` | Zähler je Art, Tag und Konto | Pseudonym | TTL nach etwa 2 Tagen, dann wenige Tage Verzögerung |
| Bucket `…-datensicherung` | Datensicherung der App (Chiffrat bis 8 MiB), Größe, Zeitpunkt; ältere Fassungen | Pseudonym, Inhalt verschlüsselt | bis Löschen; ältere Fassungen 7 Tage, höchstens 200 |
| Bucket `…-diagnose` | Diagnose-Log (unverschlüsselt, ohne Inhalte) | Pseudonym | 14 Tage |
| Log-Bucket `logs-eu` | Anfrage-Logs von Cloud Run (IP-Adresse, User-Agent, Prozedur, Zeit, Status), Server-Logs (Konto-ID, Geräte-ID, Größe, Ergebnis) | IP-Adresse; Pseudonyme | 30 Tage |
| Log-Bucket `_Required` (global) | Admin-Audit-Logs (Änderungen an Ressourcen, vor allem durch Kevin und Dienstkonten) | nur Betreiber | 400 Tage (Google, nicht änderbar) |
| Umgebung des Dienstes | Freigabeliste (Kennungen), Konten mit Diagnose-Erlaubnis (Konto-IDs) | Pseudonym | bis Austragen (README K3) |
| Arbeitsspeicher (flüchtig) | Google-ID-Token beim Anmelden (enthält Google-Konto-ID und je nach Google auch E-Mail-Adresse, Name, Bild) | direkt | nur für die Prüfung, nicht gespeichert, nicht geloggt |

## V1 Anmeldung, Konto und Geräte
- **Zweck:** Zugang zum Cloud-Backup nur für zugelassene Personen und ihre Geräte;
  Geräte einzeln sperren; Konto löschen.
- **Betroffene:** Nutzer der App mit Cloud-Backup (heute nur Kevin; später Tester).
- **Daten:** Kennung, Konto-ID, Geräte-IDs und öffentliche Geräteschlüssel,
  Sitzungen, Termine; flüchtig das Google-ID-Token. Die Freigabeliste mit Kennungen.
- **Rechtsgrundlage:** Art. 6 Abs. 1 lit. b (Nutzungsvertrag: Bereitstellung des
  Backups). Freigabeliste: lit. b (wer teilnimmt) und lit. f (Kosten und Missbrauch
  begrenzen).
- **Herkunft:** App; Google bestätigt die Anmeldung (Google ist für das Google-Konto
  selbst Verantwortlicher).
- **Löschung:** Konto löschen (B1) löscht alles sofort bzw. nach 7 Tagen Wartezeit
  ([betroffenenrechte.md](betroffenenrechte.md)); Sitzungen nach 1 Stunde; Fristen P5.

## V2 Schlüssel-Backup
- **Zweck:** Wiederherstellen der Schlüssel auf einem neuen Gerät mit dem
  Wiederherstellungsschlüssel.
- **Daten:** Chiffrat von Personen- und Tresorschlüssel. Der Betreiber kann es nicht
  entschlüsseln (Wiederherstellungsschlüssel nur bei der Person).
- **Rechtsgrundlage:** Art. 6 Abs. 1 lit. b.
- **Löschung:** mit dem Konto oder einzeln in der App.

## V3 Datensicherung (Cloud-Backup)
- **Zweck:** Sicherung der App-Daten (Erinnerungen, Erledigungen, Orte; ohne Fotos und
  Audio) für den Fall, dass das Gerät verloren geht.
- **Daten:** Chiffrat, verschlüsselt auf dem Gerät mit dem Tresorschlüssel. Der
  Betreiber sieht nur Größe, Zeitpunkt und Häufigkeit. Die Inhalte **können
  Gesundheitsdaten sein** (Art. 9, z. B. Medikamente); siehe [dsfa.md](dsfa.md).
- **Rechtsgrundlage:** Art. 6 Abs. 1 lit. b. Für Art. 9: Der Betreiber kann die
  Inhalte nie lesen; ob es trotzdem einer ausdrücklichen Einwilligung (Art. 9 Abs. 2
  lit. a) bedarf, ist eine Frage für die Prüfung vor B6. Empfehlung bis dahin: Die App
  holt beim Einschalten des Backups die ausdrückliche Einwilligung ein, mit Hinweis
  auf mögliche Gesundheitsdaten.
- **Löschung:** *Cloud-Daten löschen* in der App (sofort, alle Fassungen), Konto
  löschen; ältere Fassungen nach 7 Tagen.

## V4 Diagnose-Log
- **Zweck:** Fehler der App finden ([diagnose-entwurf.md](../diagnose-entwurf.md)).
- **Betroffene:** nur Konten mit Diagnose-Erlaubnis (heute nur Kevin, P9).
- **Daten:** feste Ereignisse mit Zahlen und Aufzählungen, App- und Android-Version,
  Gerätemodell, Stacktraces ohne Meldungstext. Keine Inhalte (per Konstruktion).
- **Rechtsgrundlage:** Art. 6 Abs. 1 lit. a, Einwilligung je Sendung (Knopf).
- **Empfänger:** Kevin; Anthropic (Claude liest die Logs; Übermittlung in die USA).
  Für andere Personen als Kevin erst mit Auftragsverarbeitungsvertrag (P9, Kap. 10).
- **Löschung:** 14 Tage; mit Konto löschen; lokale Kopie `.diagnose/` beim nächsten
  Lauf von `diagnose-holen.sh`.

## V5 Betrieb, Logs und Missbrauchsschutz
- **Zweck:** Fehlersuche, Abwehr von Missbrauch und Angriffen, Kosten begrenzen
  (Tageszähler, Alarme, Kill-Switch).
- **Daten:** Anfrage-Logs mit IP-Adresse und User-Agent, Server-Logs mit Konto- und
  Geräte-ID, Tageszähler. Keine Inhalte.
- **Rechtsgrundlage:** Art. 6 Abs. 1 lit. f (berechtigtes Interesse: sicherer und
  bezahlbarer Betrieb). Abwägung: kurze Frist, nur pseudonyme IDs, kein Profil, keine
  Weitergabe.
- **Löschung:** Logs 30 Tage (einzeln nicht löschbar, Kap. 11, 2026-11-01),
  Tageszähler nach wenigen Tagen.

## Später
- **B3 Teilen:** Mitglieder einer Freigabe, Umschläge, Operationen; Empfänger sind
  andere Nutzer.
- **B4 Push (FCM):** Geräte-Token bei Google, inhaltslose Nachrichten; Drittland.
- **B5 KI:** Klartext an Vertex AI (EU-Endpunkt), Einwilligung je Anfrage, keine
  Speicherung.
