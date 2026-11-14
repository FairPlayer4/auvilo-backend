# Datenpannen (Art. 33/34 DSGVO): Ablauf

Entwurf, Stand 2026-11-14. Eine Datenpanne ist jede Verletzung der Sicherheit, die
zu Vernichtung, Verlust, Veränderung oder unbefugter Offenlegung von
personenbezogenen Daten führt – auch versehentlich (z. B. falsch gelöschte
Sicherungen) oder nur möglicherweise (z. B. ein Secret im Repo).

**Zuständige Aufsichtsbehörde:** [Landesbeauftragte(r) für den Datenschutz des
Bundeslandes, in dem Kevin wohnt; Meldeformular-URL] – **Kevin trägt ein.**

## 1. Erkennen
Hinweise: Alarm-Mails (Kill-Switch, Egress, Anfragen, `aufraeumen` gescheitert),
Budget-Mails, ungewohnte Einträge im Audit-Log, Meldungen von Nutzern, Funde in
Reviews, Dependabot oder `govulncheck`, ein Geheimnis an falscher Stelle (Repo,
Gesprächsverlauf nach Beginn der Sicherheitsphase).

Ab dem Moment, in dem Kevin davon **weiß**, laufen die **72 Stunden**. Zeitpunkt
notieren.

## 2. Eindämmen (sofort)
Je nach Lage:
- Dienst sperren: Kill-Switch von Hand (`stand.md`, „Kill-Switch von Hand auslösen“).
- Einzelnes Konto oder Gerät: Geräte sperren; Kennung von der Freigabeliste nehmen.
- Geheimnis offengelegt: Pfeffer, Schlüssel oder Zugänge erneuern (Ablauf
  „Vor dem ersten weiteren Nutzer“ in [b2b-entwurf.md](../b2b-entwurf.md),
  [gcp-einrichtung.md](../gcp-einrichtung.md)).
- Betreiberkonto übernommen: Passwort und Passkeys erneuern, Sitzungen bei Google und
  GitHub abmelden, Deploy-Rechte prüfen (WIF, Dienstkonto `deploy`).
- Beweise sichern: Logs exportieren, bevor sie nach 30 Tagen ablaufen.

## 3. Bewerten
Leitfrage: Welches Risiko besteht für die Personen?
- **Nur Chiffrate oder pseudonyme IDs betroffen, Schlüssel sicher:** in der Regel
  **kein Risiko** → nicht melden, aber dokumentieren (Art. 33 Abs. 1 und 5).
- **Metadaten oder IP-Adressen offengelegt:** meist geringes Risiko; im Zweifel melden.
- **Schlüssel offengelegt** (Wiederherstellungs-, Personen- oder Tresorschlüssel einer
  Person) oder der Pfeffer zusammen mit Google-Konto-IDs: Inhalte sind lesbar bzw.
  Personen zuordenbar → **melden**; wegen möglicher Gesundheitsdaten oft **hohes
  Risiko** → auch die Personen benachrichtigen (Art. 34).
- **Datenverlust** (Sicherungen gelöscht, ohne lokale Kopie): melden, wenn Personen
  dadurch Daten verlieren; Personen benachrichtigen.

Art. 34 Abs. 3 lit. a: Keine Benachrichtigung der Personen nötig, wenn die Daten für
Unbefugte unzugänglich sind, insbesondere durch Verschlüsselung – gilt nur, solange
die Schlüssel nicht betroffen sind.

## 4. Melden (Art. 33)
Innerhalb von 72 Stunden an die Aufsichtsbehörde, auch unvollständig (Rest nachreichen):
Art der Panne, Kategorien und ungefähre Zahl der Personen und Datensätze, Kontakt,
wahrscheinliche Folgen, ergriffene und geplante Maßnahmen. Später gemeldet: Grund der
Verzögerung angeben.

## 5. Benachrichtigen (Art. 34)
Bei hohem Risiko die Personen in klarer Sprache: was passiert ist, Folgen, Maßnahmen,
was sie selbst tun können (z. B. neuen Wiederherstellungsschlüssel erzeugen). Weg:
Kontakt, den der Tester angegeben hat; im Play Store über einen Hinweis in der App
(der Server kennt keine E-Mail-Adressen).

## 6. Dokumentieren (Art. 33 Abs. 5)
Jede Panne, auch ohne Meldung, in dieser Liste (ohne personenbezogene Daten; Einzelheiten
in der privaten Ablage):

| Datum erkannt | Was | Betroffene Daten | Risiko | Gemeldet / benachrichtigt | Maßnahmen |
|---|---|---|---|---|---|
| – | bisher keine | | | | |

Hinweis: Dass Schlüssel und Pfeffer im Eigenbetrieb in Gesprächsverläufen stehen, ist
keine Panne, sondern so entschieden (Kap. 11, 2026-11-07); betroffen sind nur Kevins
eigene Daten, und alles wird zu Beginn der Sicherheitsphase ersetzt.
