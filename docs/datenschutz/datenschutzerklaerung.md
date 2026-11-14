# Datenschutzerklärung avlreminder (Entwurf)

> **Entwurf, Stand 2026-11-14 – nicht veröffentlichen.** Platzhalter `[…]` füllt Kevin
> erst in der veröffentlichten Fassung (nicht im Repo). Bis B6 dient der Text als
> **Testerinformation**: dann die mit „(nur Play Store)“ markierten Teile weglassen und
> den Abschnitt „Für Tester“ dazunehmen. Den Abschnitt „Auf dem Gerät“ mit dem App-Repo
> abgleichen (Berechtigungen, App-Anf. PS4, PS6).

## 1. Wer ist verantwortlich?
[Name, Anschrift, E-Mail-Adresse]. avlreminder ist ein privates Projekt ohne
Gewinnabsicht. Einen Datenschutzbeauftragten gibt es nicht; er ist nicht
vorgeschrieben.

## 2. Das Wichtigste in Kürze
- Die App funktioniert **ohne Konto und ohne Internet**. Deine Erinnerungen bleiben auf
  deinem Handy, solange du das Cloud-Backup nicht einschaltest.
- Schaltest du es ein, verschlüsselt die App deine Daten **auf dem Handy**, bevor sie
  hochgeladen werden. Den Schlüssel hast nur du. **Wir können deine Erinnerungen nicht
  lesen** – auch nicht, wenn wir wollten oder müssten.
- Wir speichern weder deinen Namen noch deine E-Mail-Adresse.
- Alle Daten liegen in der EU (Google Cloud, Belgien).
- Du kannst dein Konto jederzeit in der App löschen.

## 3. Auf dem Gerät
Die App speichert Erinnerungen, Erledigungen und Einstellungen nur auf deinem Handy.
Sie nutzt Berechtigungen nur für ihre Funktionen, und die Daten verlassen das Handy
dadurch nicht:
- **Kamera und Mikrofon:** Fotos und Tonaufnahmen als Nachweis einer Erledigung, wenn
  du das für eine Erinnerung einstellst. Sie werden **nicht** in die Cloud gesichert.
- **Standort:** Prüfung, ob du an einem festgelegten Ort bist. Gespeichert werden nur
  deine festgelegten Orte und das Ergebnis der Prüfung (Entfernung, Genauigkeit,
  Zeit), keine Bewegungsdaten.
- **Benachrichtigungen, exakte Alarme, Start nach dem Neustart:** damit Erinnerungen
  pünktlich kommen.
[Abgleich mit dem App-Repo]

## 4. Cloud-Backup (freiwillig)
**Anmeldung mit Google.** Zum Anmelden nutzt die App dein Google-Konto. Google
bestätigt uns dabei, wer du bist; das Bestätigungs-Token enthält deine Google-Konto-ID
und, je nach Google, deine E-Mail-Adresse und deinen Namen. Wir prüfen es nur und
speichern davon nichts außer einer **Kennung**, die aus deiner Google-Konto-ID mit
einem geheimen Wert berechnet wird. Ohne diesen Wert lässt sie sich nicht auf dein
Google-Konto zurückführen. Für die Anmeldung selbst ist Google verantwortlich
(Datenschutzerklärung von Google).

**Was wir speichern:**
- eine zufällige Konto-ID, den Tag, an dem du das Konto angelegt hast, und den Tag,
  an dem sich zuletzt eins deiner Geräte angemeldet hat (nur der Tag, ohne Uhrzeit);
- je Gerät eine zufällige Geräte-ID und einen öffentlichen Schlüssel, mit dem sich
  das Gerät ausweist; ob es gesperrt ist;
- Sitzungen (eine Stunde gültig);
- dein **Schlüssel-Backup**, verschlüsselt mit deinem Wiederherstellungsschlüssel;
- deine **Datensicherung**, verschlüsselt mit einem Schlüssel, den nur deine Geräte
  haben, dazu frühere Fassungen der letzten 7 Tage. Fotos und Tonaufnahmen sind nicht
  dabei. Sichtbar sind für uns nur **Größe und Zeitpunkt** jeder Sicherung; daraus
  lässt sich ablesen, wann und wie viel du die App ungefähr nutzt;
- Zähler, wie oft dein Konto heute gesichert hat (Schutz vor Missbrauch und Kosten).

Deine Erinnerungen können Gesundheitsdaten enthalten (z. B. Medikamente). Weil wir sie
nicht entschlüsseln können, erfahren wir davon nichts. [Einwilligung nach Art. 9
Abs. 2 lit. a beim Einschalten – vor B6 klären, vvt.md V3]

**Zweck und Rechtsgrundlage:** Bereitstellung des Backups, das du eingeschaltet hast
(Art. 6 Abs. 1 lit. b DSGVO).

**Wie lange:** bis du die Sicherung oder dein Konto löschst. Hat sich **24 Monate**
keins deiner Geräte angemeldet, löschen wir das Konto mit allen Daten sieben Tage
später; meldest du dich in diesen sieben Tagen an, zeigt die App den Termin, und du
kannst ihn von einem eingerichteten Gerät aus abbrechen. Von einem gesperrten Gerät
löschen wir nach **90 Tagen** den Schlüssel und alle Zeitpunkte; nur seine Geräte-ID
bleibt bis zum Löschen des Kontos, damit es gesperrt bleibt.

**Wer hat Zugriff?** Nur der Betreiber, und nur auf die oben genannten Daten, nicht auf
Inhalte. Wir geben nichts weiter und verkaufen nichts.

## 5. Zugang zum Dienst
Der Server nimmt nur Konten an, die wir freigeschaltet haben. Dazu steht deine Kennung
auf einer Liste. Ist sie noch nicht darauf, zeigt dir die App sie an, damit du sie uns
schicken kannst. Rechtsgrundlage: Art. 6 Abs. 1 lit. b und f DSGVO (Kosten und
Missbrauch begrenzen). Wir nehmen sie von der Liste, wenn du nicht mehr teilnimmst
oder es verlangst.

## 6. Server-Protokolle
Bei jedem Aufruf speichert Google Cloud für uns ein Protokoll mit **IP-Adresse**,
Gerätekennung des Browsers bzw. der App (User-Agent), Zeit und Ergebnis; unser Server
ergänzt Konto- und Geräte-ID, Größe und Ergebnis, nie Inhalte. Zweck: Fehler finden,
Angriffe und Missbrauch abwehren, Kosten begrenzen. Rechtsgrundlage: berechtigtes
Interesse (Art. 6 Abs. 1 lit. f DSGVO). Die Protokolle werden nach **30 Tagen**
automatisch gelöscht; einzeln löschen lassen sie sich nicht, deshalb bleiben sie auch
nach dem Löschen deines Kontos bis zum Ablauf der 30 Tage.

## 7. Diagnose (nur mit deiner Zustimmung)
[Nur für Personen mit Diagnose-Erlaubnis, P9.] Wenn du in der App „Diagnose senden“
drückst, schickt sie ein Protokoll technischer Ereignisse: App-, Android-Version,
Gerätemodell, Fehlerstellen. **Keine Inhalte**, keine Texte. Es wird unverschlüsselt in
der EU gespeichert, nach **14 Tagen** gelöscht und vom Betreiber mit Hilfe eines
KI-Assistenten (Claude von Anthropic, USA) ausgewertet. Rechtsgrundlage: deine
Einwilligung je Sendung (Art. 6 Abs. 1 lit. a DSGVO); du kannst jederzeit aufhören,
schon gesendete löschen wir auf Wunsch.

## 8. Dienstleister und Drittländer
- **Google Cloud** (Google Cloud EMEA Limited, Irland) betreibt Server und Speicher für uns in der
  Region Belgien, als Auftragsverarbeiter nach Art. 28 DSGVO. Google kann
  Unterauftragnehmer außerhalb der EU einsetzen; dafür gelten die
  EU-Standardvertragsklauseln und das EU-US Data Privacy Framework.
- **Google-Anmeldung:** siehe Abschnitt 4.
- **Google Play** (nur Play Store): Installation und Updates; dafür ist Google
  verantwortlich.

## 9. Deine Rechte
Du hast das Recht auf Auskunft (Art. 15), Berichtigung (Art. 16), Löschung (Art. 17),
Einschränkung (Art. 18), Datenübertragbarkeit (Art. 20) und Widerspruch gegen
Verarbeitungen aus berechtigtem Interesse (Art. 21). Eine Einwilligung kannst du
jederzeit für die Zukunft widerrufen. Schreib dazu an [E-Mail] und gib die
**Konto-ID** an, die dir die App anzeigt; ohne sie können wir dich nicht zuordnen, weil
wir weder Namen noch E-Mail-Adressen speichern (Art. 11 DSGVO).

Du kannst dich bei einer Datenschutz-Aufsichtsbehörde beschweren, z. B. bei der für
deinen Wohnort.

## 10. Konto löschen
In der App unter [Pfad]: *Konto löschen*. Von einem eingerichteten Gerät löschen wir
sofort alles: Konto, Geräte, Sitzungen, Schlüssel-Backup, Datensicherung mit allen
früheren Fassungen und Diagnosen. Hast du kein eingerichtetes Gerät mehr (nur dein
Google-Konto), löschen wir **nach 7 Tagen**: So kann niemand, der dein Google-Konto
übernommen hat, sofort alles löschen; deine Geräte zeigen den Termin an und können
abbrechen. Danach bleiben nur die Server-Protokolle bis zum Ablauf ihrer 30 Tage und
Zähler für wenige Tage. Die Daten auf deinem Handy löscht das nicht.

(nur Play Store) Ohne App: [URL der Löschseite].

## 11. Alter
Die App ist für Personen ab **16 Jahren** (Art. 8 DSGVO).

## 12. Für Tester (nur bis B6)
- Der Dienst ist ein Lernprojekt im Test. Er kann jederzeit ausfallen oder gesperrt
  werden, z. B. wenn die Kosten eine Grenze erreichen. Deine Daten bleiben dann auf
  dem Handy. Verlass dich nicht allein auf das Cloud-Backup.
- Die Testphase kann enden; dann löschen wir alle Konten und sagen dir vorher Bescheid.

## 13. Änderungen
Stand: [Datum]. Ändert sich etwas Wesentliches, zeigt die App einen Hinweis.
