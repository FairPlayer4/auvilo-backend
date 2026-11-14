# Datenschutz-Folgenabschätzung (Art. 35 DSGVO)

Entwurf, Stand 2026-11-14 (P7). Gegenstand: das Cloud-Backup (V1–V5 in
[vvt.md](vvt.md)) vor dem ersten weiteren Nutzer. Für B3 (Teilen), B4 (Push) und B5
(KI) wird sie fortgeschrieben.

## 1. Ist sie Pflicht?
Kriterien der Aufsichtsbehörden (EDSA-Leitlinien WP 248, Muss-Liste der DSK):
- **Sensible Daten** (Art. 9): ja, möglich. Erinnerungen können Medikamente,
  Arzttermine oder Therapien betreffen.
- **Umfang:** sehr klein (Kevin, später wenige Tester, langfristig eine kleine
  Zielgruppe im Play Store).
- **Neue Technologie, Profiling, Überwachung, Zusammenführen von Datensätzen:** nein.
- **Lesbarkeit für den Betreiber:** Die Inhalte sind Ende-zu-Ende-verschlüsselt; der
  Betreiber verarbeitet sie nur als Chiffrat.

**Einschätzung:** Nur ein Kriterium, in kleinem Umfang und ohne Lesezugriff.
Voraussichtlich **keine Pflicht**; die DSFA wird **freiwillig** gemacht (P7). Wichtig
wegen § 38 Abs. 1 Satz 2 BDSG: Bei einer Pflicht-DSFA wäre ein
Datenschutzbeauftragter zu benennen. Die Einschätzung lässt Kevin bei der Prüfung
vor B6 bestätigen; mit B3 (Inhalte gehen an andere Personen) und B5 (Klartext an die
KI) neu bewerten.

## 2. Beschreibung
Die App sichert ihre Datenbank (ohne Fotos und Audio) verschlüsselt in Cloud Storage
(EU). Schlüssel entstehen und bleiben auf den Geräten; ein Schlüssel-Backup liegt
verschlüsselt mit dem Wiederherstellungsschlüssel auf dem Server. Anmeldung mit
Google, gespeichert nur pseudonyme Kennungen. Einzelheiten: [vvt.md](vvt.md),
[b2b-entwurf.md](../b2b-entwurf.md).

## 3. Notwendigkeit und Verhältnismäßigkeit
- **Zweck:** Daten bei Verlust des Geräts nicht zu verlieren. Ohne Server ginge das nur
  über die lokale Sicherung, die der Mensch selbst verwahren muss.
- **Freiwillig:** Das Backup ist ein Schalter; die App funktioniert ohne.
- **Minimal:** kein Klartext, keine E-Mail-Adresse, kein Name; kurze Fristen;
  EU-Region.
- **Rechte der Betroffenen:** Löschen in der App (sofort), Auskunft von Hand
  ([betroffenenrechte.md](betroffenenrechte.md)).

## 4. Risiken
Bewertet nach Eintrittswahrscheinlichkeit (E) und Schwere (S), jeweils gering /
mittel / hoch, **nach** den Maßnahmen aus [toms.md](toms.md).

| Nr. | Risiko | Maßnahmen | E | S |
|---|---|---|---|---|
| R1 | Angriff auf Server oder Speicher legt Inhalte offen | E2E: Schlüssel nie auf dem Server; Server sieht nur Chiffrate | gering | gering (nur Metadaten) |
| R2 | Übernahme des Google-Kontos | Neues Gerät braucht Freischaltung oder Wiederherstellungsschlüssel; Löschen ohne Freischaltung nur mit 7 Tagen Wartezeit, sichtbar auf allen Geräten | mittel | gering; **hoch**, wenn der Wiederherstellungsschlüssel ungeschützt im Passwortdienst liegt (daher Papier als Standard) |
| R3 | Übernahme der Betreiberkonten (GCP, GitHub) | Krypto nur im Client: Ein manipulierter Server kann nichts entschlüsseln. Möglich: Daten löschen, Metadaten lesen, Dienst sperren | gering | mittel (Verfügbarkeit, Metadaten). GitHub mit Passkey seit 2026-11-14 |
| R4 | Fehler in der Kryptografie | Tink statt Eigenbau, feste Testvektoren, Reviews, Format v2 (SE6, offen) | gering | hoch |
| R5 | Metadaten verraten Nutzungsmuster (Größe, Zeitpunkt, IP-Adresse) | 30 Tage Logs, Zugriff nur Kevin, kein Profil | mittel | gering |
| R6 | Datenverlust (Kill-Switch, Löschung durch Fremden mit entsperrtem Handy, Fehler) | lokal-first, ältere Fassungen 7 Tage, lokales Backup; Displaysperre (SE5, offen) | mittel | mittel |
| R7 | Daten bleiben unbegrenzt, wenn die Person ihr Google-Konto verliert | Frist 24 Monate ohne Anmeldung (P5) | **hoch, solange die Frist fehlt** | gering (nur Chiffrate) |
| R8 | Diagnose enthält doch einen Inhalt und geht an Anthropic | Logger ohne freie Zeichenketten; nur Kevins Konto (P9); 14 Tage | gering | mittel |
| R9 | Zugriff von Behörden auf Google (auch aus Drittländern) | EU-Region; nur Chiffrate und pseudonyme Metadaten | gering | gering |
| R10 | Verwechslung oder falsche Löschung eines Kontos | Löschen nur mit fälligem Termin (`ErrNichtFaellig`), Tests | gering | mittel |

## 5. Ergebnis
**Vorläufig:** Das Restrisiko ist gering, weil der Betreiber die Inhalte nicht lesen
kann. Zwei Punkte müssen vor dem ersten weiteren Nutzer erledigt sein, damit das
gilt:
- **R7:** Frist aus P5 mit `zuletzt_aktiv` ([README.md](README.md), K2).
- **R2/R6:** Displaysperre (SE5) und A-15 neu (Papier als Standard) in der App.

Danach Ergebnis hier mit Datum eintragen. Keine Konsultation der Aufsichtsbehörde
nötig (Art. 36), solange kein hohes Restrisiko bleibt.

## 6. Fortschreibung
| Datum | Anlass | Ergebnis |
|---|---|---|
| 2026-11-14 | Entwurf | vorläufig, siehe 5 |
