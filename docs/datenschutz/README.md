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
| [vvt.md](vvt.md) – Verzeichnis der Verarbeitungstätigkeiten, mit Löschfristen und Empfängern | Art. 30 | Entwurf |
| [toms.md](toms.md) – technische und organisatorische Maßnahmen | Art. 32 | Entwurf |
| [dsfa.md](dsfa.md) – Datenschutz-Folgenabschätzung | Art. 35 (P7) | Entwurf, Ergebnis offen bis K2 |
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
- Bricht das Löschen ab, macht der tägliche Lauf weiter; kein halb gelöschtes Konto
  ohne Termin.
- Die 7 Tage Wartezeit bei einem Antrag ohne freigeschaltetes Gerät passen zu
  Art. 12 Abs. 3 (Frist ein Monat) und Abs. 6 (begründete Zweifel an der Identität:
  ein übernommenes Google-Konto). Die Datenschutzerklärung muss sie nennen.
- Die Kennung steht in keinem Log.

**Lücken** (K = Konto löschen):

| Nr. | Lücke | Gewicht | Empfehlung |
|---|---|---|---|
| K1 | Die **App** kann das Konto noch nicht löschen; nur der Test-Client kann es. | vor dem ersten Tester Pflicht | wie geplant (App-Repo `docs/stand.md`) |
| K2 | **Die Frist aus P5 lässt sich nicht umsetzen:** Das Konto speichert nur `erstellt` und `loeschen_ab`, keine letzte Aktivität. Ohne sie gibt es weder „24 Monate ohne Anmeldung“ noch einen Löschweg für Konten, deren Google-Konto weg ist. Die Begründung mit Art. 11 (Kap. 11, 2026-11-12) stützt sich aber genau auf diese Frist. Je später das Feld kommt, desto später greift die Frist. | hoch | Feld `zuletzt_aktiv` (nur der Tag, wie `erstellt`) jetzt einführen, gesetzt beim Anmelden und Erneuern, höchstens ein Schreibvorgang je Konto und Tag. Die beiden Fristen (24 Monate, gesperrte Geräte 90 Tage über `gesperrt_am`) in `aufraeumen` **vor dem ersten Tester** statt zu B6. |
| K3 | Die **Kennung bleibt auf der Freigabeliste** (`freigegebene_kennungen` in `werte.auto.tfvars`, Umgebungsvariable des Dienstes, auch in alten Revisionen). Sie ist ein Pseudonym der Person; meldet sie sich neu an, ist sie sofort wieder zugelassen. | mittel | Die Liste ist eine eigene Verarbeitung (Zugangsverwaltung, [vvt.md](vvt.md) V1). Austragen, wenn jemand nicht mehr teilnimmt oder es verlangt; alte Revisionen dann löschen. In der Datenschutzerklärung nennen. |
| K4 | **Logs** (Konto-ID, Geräte-ID, IP-Adresse) bleiben bis 30 Tage nach dem Löschen. | bewusst | So entschieden (Kap. 11, 2026-11-01); steht in der Datenschutzerklärung. |
| K5 | **Tageszähler** (`kontingente`, Konto-ID in der Dokument-ID) verschwinden erst mit der TTL. Firestore löscht abgelaufene Dokumente verzögert, laut Google in der Regel innerhalb von 24 Stunden; „höchstens zwei Tage“ in B1 ist also zu knapp. | gering | In B1 und Datenschutzerklärung „wenige Tage“ schreiben. |
| K6 | **Kopien der Diagnosen außerhalb von GCP:** `.diagnose/` bei Kevin folgt dem Bucket erst beim nächsten Lauf von `diagnose-holen.sh`; was Claude gelesen hat, liegt in Gesprächsverläufen. | solange nur Kevin sendet, keins | Vor Diagnosen von Testern (P9): Löschablauf um beides erweitern ([betroffenenrechte.md](betroffenenrechte.md)). |
| K7 | **Webseite zum Löschen** fehlt (Play-Store-Pflicht). | zu B6 | bis dahin Anleitung in der Datenschutzerklärung |
| K8 | Mit B3: eigene Operationen in Freigaben anderer und Mitgliedschaften gehören zum Löschweg (B1). | mit B3 | im B3-Entwurf |

## Was noch zu tun ist
Reihenfolge nach Fälligkeit. „Kevin“ heißt: nur er kann es (Console, rechtliche
Angaben, Entscheidung).

**Vor dem ersten Tester bzw. vor B3 (Sicherheitsphase):**
1. **Kevin:** In der Cloud Console prüfen, dass das **Cloud Data Processing Addendum**
   (CDPA) akzeptiert ist, und den Vertragspartner von Google für [vvt.md](vvt.md)
   notieren (P3).
2. **Konto löschen in der App** (K1).
3. **`zuletzt_aktiv` und die Fristen aus P5** (K2). Empfehlung: vorziehen.
4. **Freigabeliste pflegen** (K3): Ablauf in [betroffenenrechte.md](betroffenenrechte.md).
5. **Auskunft** (Art. 15, B2): Die App braucht eine Anzeige der eigenen Konto-ID, damit
   Kevin eine Anfrage zuordnen kann; den Rest erledigt er nach
   [betroffenenrechte.md](betroffenenrechte.md) von Hand. Ein eigener Endpunkt erst,
   wenn Anfragen häufiger kommen.
6. **Testerinformation** aus [datenschutzerklaerung.md](datenschutzerklaerung.md):
   Platzhalter füllen, Mindestalter 16 (P8), Abschnitt Diagnose nur, wenn der Tester
   eine Diagnose-Erlaubnis bekommt (P9). Dem Tester vor der Freigabe geben.
7. **DSFA abschließen** ([dsfa.md](dsfa.md)): Ergebnis bestätigen, sobald K2 umgesetzt ist.
8. **TOMs nachziehen:** Displaysperre (SE5), Format v2 (SE6), GitHub mit Passkey; die
   Liste in [toms.md](toms.md) markiert sie als offen.
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
