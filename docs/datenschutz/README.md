# Datenschutz (DSGVO)

Stand: 2026-11-14. Die Dokumente sind **Entwürfe**. Vor B6 prüft sie jemand mit
Datenschutzerfahrung ([anforderungen.md](../anforderungen.md) Kap. 11, 2026-11-01).
Grundlage sind Kap. 4.4 (P1–P9) und 3.5 (B1–B3) der Anforderungen und die
Sicherheits- und Datenschutzprüfung vom 2026-11-12.

Persönliche Angaben des Verantwortlichen (Name, Anschrift, E-Mail) stehen nicht im
Repo, auch nicht in der Datenschutzerklärung. Dort stehen Platzhalter `[…]`, die erst
in der veröffentlichten Fassung ersetzt werden.

| Dokument | Pflicht aus | Stand |
|---|---|---|
| [vvt.md](vvt.md) – Verzeichnis der Verarbeitungstätigkeiten, mit Löschfristen und Empfängern | Art. 30 | Entwurf; CDPA akzeptiert 2026-11-14 |
| [toms.md](toms.md) – technische und organisatorische Maßnahmen | Art. 32 | Entwurf |
| [dsfa.md](dsfa.md) – Datenschutz-Folgenabschätzung | Art. 35 (P7) | Entwurf, Ergebnis vorläufig |
| [datenschutzerklaerung.md](datenschutzerklaerung.md) – Datenschutzerklärung und Testerinformation | Art. 13, Play Store | Entwurf mit Platzhaltern |
| [betroffenenrechte.md](betroffenenrechte.md) – Abläufe für Auskunft, Löschung, Widerspruch | Art. 12–22 | Entwurf |
| [datenpannen.md](datenpannen.md) – Ablauf bei Datenpannen, Pannenliste | Art. 33/34 | Entwurf |

## Prüfung „Konto löschen“ (2026-11-14)
Geprüft: `server/internal/dienst/konto.go`, `server/internal/speicher/firestore.go`
(`KontoSperren`, `KontoEntfernen`), die `Loeschen` der beiden Ablagen,
`infra/datensicherung.tf`, `infra/diagnose.tf`, `infra/aufraeumen.tf`.

**Erfüllt:**
- Der Löschweg erfasst alles, was der Server zu einem Konto speichert: Kennung,
  Konto, Geräte, Sitzungen, Schlüssel-Backup, Datensicherung mit allen Fassungen,
  Diagnosen. Beide Buckets haben kein Soft Delete, Firestore hat weder
  Point-in-Time-Recovery noch Sicherungen: Gelöscht ist wirklich gelöscht (Art. 17).
  Aus Googles eigenen Systemen verschwindet es spätestens nach 180 Tagen (CDPA 6.1).
- Bricht das Löschen ab, macht der tägliche Lauf weiter; kein halb gelöschtes Konto
  ohne Termin. Kommt danach noch eine Sicherung oder ein Schlüssel-Backup an, löscht
  der Lauf es nach einer Stunde (seit 2026-11-19).
- Die 7 Tage Wartezeit bei einem Antrag ohne freigeschaltetes Gerät passen zu
  Art. 12 Abs. 3 (Frist ein Monat) und Abs. 6 (begründete Zweifel an der Identität:
  ein übernommenes Google-Konto). Die Datenschutzerklärung muss sie nennen.
- Die Kennung steht in keinem Log.

**Lücken** (K = Konto löschen):

| Nr. | Lücke | Gewicht | Empfehlung |
|---|---|---|---|
| K1 | ~~Die **App** kann das Konto noch nicht löschen; nur der Test-Client kann es.~~ **In der App umgesetzt in V1.3c2 (App-Branch `feature/konto-loeschen`, 2026-11-14), noch nicht installiert:** *Konto löschen* ersetzt *Cloud-Daten löschen*; ohne Freischaltung als Antrag mit *Löschung abbrechen*; einen Antrag von anderswo meldet die App mit Benachrichtigung, der stündliche Lauf fragt dafür spätestens nach 23 Stunden nach. | vor dem ersten Tester Pflicht | Gerätetest C15–C17 auf dem Emulator (App-Repo `docs/geraetetest.md`), dann installieren |
| K2 | ~~**Die Frist aus P5 lässt sich nicht umsetzen:** keine letzte Aktivität im Konto.~~ **Erledigt 2026-11-14:** `zuletzt_aktiv` (nur der Tag) beim Anmelden und Erneuern; der tägliche Lauf `aufraeumen` merkt Konten nach 24 Monaten ohne Aktivität zur Löschung in 7 Tagen vor und ersetzt Geräte, die seit 90 Tagen gesperrt sind, durch einen Grabstein ([anforderungen.md](../anforderungen.md) Kap. 11). | – | Deployt 2026-11-14 (PR #26). Konten von vor dem Deploy erfasst die Frist erst nach ihrer nächsten Anmeldung. |
| K3 | Die **Kennung bleibt auf der Freigabeliste** (`freigegebene_kennungen` in `werte.auto.tfvars`, Umgebungsvariable des Dienstes, auch in alten Revisionen). Sie ist ein Pseudonym der Person; meldet sie sich neu an, ist sie sofort wieder zugelassen. | mittel | Die Liste ist eine eigene Verarbeitung (Zugangsverwaltung, [vvt.md](vvt.md) V1). Austragen, wenn jemand nicht mehr teilnimmt oder es verlangt; alte Revisionen dann löschen. In der Datenschutzerklärung nennen. |
| K4 | **Logs** (Konto-ID, Geräte-ID, IP-Adresse) bleiben bis 30 Tage nach dem Löschen. | bewusst | So entschieden (Kap. 11, 2026-11-01); steht in der Datenschutzerklärung. |
| K5 | **Tageszähler** (`kontingente`, Konto- oder Geräte-ID in der Dokument-ID, beim Zähler für neue Geräte die Kennung) verschwinden erst mit der TTL. Firestore löscht abgelaufene Dokumente verzögert, laut Google in der Regel innerhalb von 24 Stunden; „höchstens zwei Tage“ in B1 ist also zu knapp. | gering | In B1 und Datenschutzerklärung „wenige Tage“ schreiben. |
| K6 | **Kopien der Diagnosen außerhalb von GCP:** `.diagnose/` bei Kevin folgt dem Bucket erst beim nächsten Lauf von `diagnose-holen.sh`; was Claude gelesen hat, liegt in Gesprächsverläufen. | solange nur Kevin sendet, keins | Vor Diagnosen von Testern (P9): Löschablauf um beides erweitern ([betroffenenrechte.md](betroffenenrechte.md)). |
| K7 | **Webseite zum Löschen** fehlt (Play-Store-Pflicht). | zu B6 | bis dahin Anleitung in der Datenschutzerklärung |
| K8 | Mit B3: eigene Operationen in Freigaben anderer und Mitgliedschaften gehören zum Löschweg (B1). | mit B3 | im B3-Entwurf |

## Was noch zu tun ist
Reihenfolge nach Fälligkeit. „Kevin“ heißt: nur er kann es (Console, rechtliche
Angaben, Entscheidung).

**Vor dem ersten Tester bzw. vor B3 (Sicherheitsphase):**
1. ~~**Cloud Data Processing Addendum** (CDPA) akzeptieren (P3)~~ – erledigt
   2026-11-14 ([vvt.md](vvt.md)). Offen nur: Vertragspartner auf der nächsten Rechnung
   bestätigen.
2. **Konto löschen in der App** (K1): umgesetzt in V1.3c2 (App-Branch `feature/konto-loeschen`, 2026-11-14), noch nicht installiert; offen sind die Gerätetests C15–C17.
3. ~~**`zuletzt_aktiv` und die Fristen aus P5** (K2)~~ – umgesetzt und deployt
   2026-11-14 (PR #26).
4. **Freigabeliste pflegen** (K3): Ablauf in [betroffenenrechte.md](betroffenenrechte.md).
5. ~~**Auskunft** (Art. 15, B2): Die App braucht eine Anzeige der eigenen Konto-ID~~ –
   umgesetzt in V1.3c2 (2026-11-14, noch nicht installiert): *Einstellungen → Cloud-Backup (experimentell)* zeigt
   die Konto-ID zum Kopieren, damit Kevin eine Anfrage zuordnen kann; den Rest erledigt er nach
   [betroffenenrechte.md](betroffenenrechte.md) von Hand. Ein eigener Endpunkt erst,
   wenn Anfragen häufiger kommen.
6. **Testerinformation** aus [datenschutzerklaerung.md](datenschutzerklaerung.md):
   Platzhalter füllen, Mindestalter 16 (P8), Abschnitt Diagnose nur, wenn der Tester
   eine Diagnose-Erlaubnis bekommt (P9). Dem Tester vor der Freigabe geben.
7. **DSFA abschließen** ([dsfa.md](dsfa.md)): R7 ist mit K2 erledigt; Ergebnis
   bestätigen, sobald die App mit SE5 und A-15 installiert ist (V1.3c2, V1.3c3,
   2026-11-15).
8. **TOMs nachziehen:** Erledigt: GitHub mit Passkey (2026-11-14), Displaysperre (SE5,
   App V1.3c2, 2026-11-15), Format v2 (SE6, 2026-11-23, in [toms.md](toms.md)).
9. **Kevin:** zuständige Aufsichtsbehörde in [datenpannen.md](datenpannen.md) eintragen.

**Mit B3 bis B5:** Datenschutzerklärung, VVT und DSFA um Teilen (Mitglieder sehen
Inhalte anderer, Art. 26 prüfen?), FCM (Drittland, B4) und KI (Vertex AI, B5) ergänzen;
K8; Frage 2 aus Kap. 10 (Kinder).

**Zu B6 (Play Store):**
- Datenschutzerklärung unter einer öffentlichen URL (Play Console), Impressum.
- Webseite zum Löschen (K7).
- Angaben zur Datensicherheit in der Play Console. Klären, ob Google E2E-verschlüsselte
  Sicherungen als „erhoben“ zählt; die Konto-ID zählt sicher (Nutzer-IDs).
- Berechtigungen der App (Kamera, Mikrofon, Standort im Hintergrund) begründen; das
  gehört ins App-Repo (App-Anf. PS4).
- Prüfung durch jemanden mit Datenschutzerfahrung.
