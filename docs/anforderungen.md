# Anforderungen – avlreminder-backend

> Experimentelles Backend für avlreminder (Android-App, Repo `FairPlayer4/avlreminder`)
> auf Google Cloud.
> Stand: 2026-11-01 – zweiter Entwurf (Schlüsselmodell, Google-Login, Go), wird
> iterativ verfeinert.
> Markierungen: **[E]** = Empfehlung von Claude, noch nicht entschieden;
> **[F n]** = offene Frage in Kap. 10; **[P]** = vor Umsetzung prüfen.

## 1. Zweck und Rahmen

- **Lernprojekt**: GCP, serverseitige Entwicklung in Go, Ende-zu-Ende-Verschlüsselung,
  Synchronisation und Datenschutz praktisch kennenlernen.
- **Erstmal nicht mit der App verbunden.** Getestet wird mit einem eigenen
  Test-Client (CLI), der mehrere Personen und Geräte simuliert.
- **Langfristig Play Store**: Anfangs nur der eigene Haushalt, später sollen
  beliebige Personen bestimmte Reminder mit bestimmten Personen teilen können.
  Die Architektur soll das von Anfang an tragen, umgesetzt wird erst der Haushalt.
- **Hauptfeature: Synchronisation zwischen Anwendern** – was geteilt wird und mit
  wem, ist einzeln wählbar.
- **Nebenfeature: Cloud-KI** zur Bewertung und Ideengenerierung, jedes Mal nur nach
  ausdrücklicher Freigabe.
- **Die App bleibt lokal-first.** Das Backend verteilt verschlüsselte Änderungen,
  Fachlogik und Alarme bleiben auf dem Gerät. Ohne Backend funktioniert die App
  vollständig.
- **Kosten**: Ziel 0 € im Leerlauf, **harte Obergrenze 20 €/Monat**.

### Verhältnis zur App-Planung
Die App-Anforderungen sehen für V3 lokale Synchronisation ohne Backend vor (7.18).
Damit beide Wege zusammenpassen, ist das **Sync-Protokoll transportunabhängig**:
dieselben verschlüsselten Änderungen können über das Backend, Nearby Connections
oder Home Assistant laufen. Das Protokoll (Datenformat, Krypto, Konfliktauflösung)
liegt in einem Kotlin-Modul, das später in die App übernommen werden kann. [E]

## 2. Leitprinzipien

1. **Login entscheidet über Zugriff, Schlüssel über Lesbarkeit.** Der Server liefert
   nur an berechtigte Konten aus; lesen kann nur, wer den passenden Schlüssel hat.
2. **Der Server sieht keine Inhalte.** Er speichert Chiffrate und die nötigsten
   Metadaten (wer ist Mitglied wovon, Reihenfolge, Größe, Zeitpunkt).
3. **Datenminimierung**: vom Google-Konto nur die pseudonyme Konto-ID; E-Mail nur,
   wenn man über sie auffindbar sein will. [F2]
4. **EU-only** für alles, was gespeichert oder inhaltlich verarbeitet wird.
5. **Klartext für die KI nur mit Zustimmung**, nur für den freigegebenen Inhalt, ohne
   Speicherung.
6. **Vergessen ist Standard**: Ablaufzeiten auf dem Server, Konto löschen löscht alles.
7. **Kosten sind begrenzt**, nicht nur überwacht.

## 3. Funktionale Anforderungen

### 3.1 Konto, Personen, Geräte
- ID1 **Anmeldung mit Google** (Android: Credential Manager, Test-Client: OAuth für
  Desktop-Apps). Das Backend prüft das Google-ID-Token selbst gegen die öffentlichen
  Schlüssel von Google und gibt ein eigenes, kurzlebiges Sitzungs-Token aus.
  **Kein Firebase Authentication/Identity Platform** nötig. [E]
- ID2 Gespeichert wird nur die Google-Konto-ID (`sub`); E-Mail nur nach Wahl [F2].
- ID3 Eine **Person** hat einen **Personenschlüssel** (siehe 4.1) und mehrere **Geräte**.
  Neues Gerät: per QR-Code von einem vorhandenen Gerät oder mit dem
  Wiederherstellungsschlüssel.
- ID4 **Wiederherstellungsschlüssel** (zum Ausdrucken) entschlüsselt ein
  verschlüsseltes Schlüssel-Backup auf dem Server. Ohne ihn und ohne Gerät sind
  private Daten verloren (gewollt); Geteiltes kann man sich neu freigeben lassen.
- ID5 Missbrauchsschutz: Rate-Limits pro Konto und Gerät; später für den Play Store
  zusätzlich **Play Integrity API** (Anfrage kommt von der echten App). [E]
- ID6 **Gerät abmelden/sperren** aus einem anderen Gerät heraus (verloren, gestohlen).

### 3.2 Kontakte und Freigaben
- F1 **Kontakt** = andere Person, deren Personenschlüssel man kennt und bestätigt hat.
  Anfangs nur **persönlich per QR-Code** (Schlüssel + Konto-ID, gegenseitig).
  Später eine Ferneinladung mit Fingerabdruck-Vergleich. [F5]
- F2 **Freigabe** = ein geteiltes Objekt (Reminder, Checkliste, Liste) mit eigener
  Mitgliederliste, eigenem Schlüssel und eigenem Änderungs-Log. So kann jeder
  Reminder mit genau den gewünschten Personen geteilt werden. [E]
- F3 **Kreise** (z. B. „Haushalt“) sind nur eine gespeicherte Auswahl von Kontakten
  zum schnellen Teilen, kein eigener Schlüssel. Neues Haushaltsmitglied → die App
  fragt, welche bestehenden Freigaben es bekommen soll.
- F4 **Was** geteilt wird, ist pro Freigabe einzeln wählbar (z. B. Reminder und
  Erledigt-Status ja, Nachweis-Text nein). Fotos, Audio, Standort- und
  Bewegungsdaten werden nicht geteilt (App 8.4). [F6]
- F5 **Rechte** pro Mitglied: anfangs alle gleich, später ggf. *bearbeiten* vs. *nur
  abhaken*. [F7]
- F6 **Person entfernen**: Der Server sperrt den Zugriff sofort (Login-Ebene). Der
  Schlüssel wird beim nächsten Schreiben automatisch gewechselt („faule Rotation“).
  Was die Person schon gesehen hat, behält sie – das lässt sich technisch nicht
  verhindern und wird in der App so erklärt.

### 3.3 Synchronisation (Hauptfeature)
- S1 Jede Änderung ist eine **Operation** („Punkt 3 abgehakt von A um T“), lokal
  verschlüsselt und mit dem Personenschlüssel signiert. Empfänger prüfen die
  Signatur selbst – auch ein manipulierter Server kann keine Änderungen fälschen.
- S2 Der Server führt pro Freigabe ein **Append-only-Log** mit fortlaufender
  Sequenznummer und prüft nur: angemeldet, Mitglied, Format, Größe.
- S3 **Konfliktauflösung auf dem Client**: Hybrid Logical Clock pro Operation,
  Last-Writer-Wins pro Feld, Listen und Checklisten als Mengen. [E]
- S4 **Snapshots** pro Freigabe; ältere Operationen werden danach gelöscht.
- S5 **Eigene Geräte**: private Daten können über den privaten Tresor (4.1) zwischen
  den eigenen Geräten synchronisiert bzw. gesichert werden. [F1]
- S6 **Zeitnah in Sekunden**: Nach jeder Änderung schickt der Server einen
  **inhaltslosen Push über Firebase Cloud Messaging** („Freigabe X hat Neues“) an die
  Geräte der anderen Mitglieder; die holen die Änderung dann ab. FCM ist kostenlos
  und auf Android der Standardweg. Dauerverbindungen (WebSocket) würden Cloud Run
  ständig wach halten und kosten. [E]
- S7 Offline-fähig und idempotent (Operations-ID), Abgleich zusätzlich beim
  App-Start und periodisch, falls ein Push verloren geht.
- S8 Alarme und Eskalation bleiben **vollständig lokal**.

### 3.4 Cloud-KI (Nebenfeature)
- K1 Zweck: **Bewertung und Ideengenerierung** (z. B. „ist dieser Nachweis plausibel?“,
  „Vorschläge, wie ich das Ziel leichter schaffe“).
- K2 Medien: **zuerst nur Text**; später Audio, Fotos, evtl. Video.
- K3 **Jedes Mal ausdrückliche Freigabe**: Die App zeigt genau den Text, der gesendet
  wird, und Modell/Region. Kein „immer erlauben“ (vorerst).
- K4 Ablauf: Klartext per TLS an den KI-Endpunkt des Backends → Vertex AI in einer
  EU-Region → Antwort zurück. **Keine Speicherung, kein Logging von Inhalten**,
  Modell- und Promptversion gehen mit der Antwort zurück. [E]
- K5 Modell austauschbar (Gemini zuerst, weil auf Vertex AI mit EU-Endpunkt). [P]
- K6 Kontingent pro Konto und Tag; lokale KI hat Vorrang, wo sie reicht.

### 3.5 Verwaltung und Betrieb
- B1 **Konto löschen** in der App **und über eine Webseite** (Pflicht im Play Store)
  löscht sofort Konto, Geräte, Mitgliedschaften und eigene Operationen.
- B2 Auskunft/Export der eigenen Server-Daten (Art. 15/20); Inhalte hat die App ohnehin.
- B3 Strukturierte Logs ohne Inhalte, ohne dauerhaft gespeicherte IP-Adressen.

## 4. Datenschutz und Sicherheit

### 4.1 Schlüsselmodell [E]

| Schlüssel | Art | Wer hat ihn | Wofür |
|---|---|---|---|
| **Geräteschlüssel** | Signatur-Schlüsselpaar, pro Gerät | nur dieses Gerät (Android Keystore [P]) | Gerät gegenüber dem Server und den eigenen Geräten ausweisen, Gerät einzeln sperrbar |
| **Personenschlüssel** | Schlüsselpaar für Schlüsselaustausch + Signatur | alle eigenen Geräte | Andere verschlüsseln Freigabe-Schlüssel *für mich*; ich signiere Operationen |
| **Tresorschlüssel** (privat) | symmetrisch | alle eigenen Geräte | **Nur eigene Daten**: Sync/Backup zwischen eigenen Geräten |
| **Freigabe-Schlüssel** | symmetrisch, einer pro Freigabe, mit Versionen | alle Mitglieder der Freigabe | Inhalte und Operationen dieser Freigabe |
| **Wiederherstellungsschlüssel** | symmetrisch, ausgedruckt | nur Papier | entschlüsselt das Backup von Personen- und Tresorschlüssel |

- Das entspricht deiner Idee „ein privater Schlüssel plus Mehrfach-Schlüssel für
  Geteiltes“. Ergänzt um den Personenschlüssel, über den Freigabe-Schlüssel sicher
  verteilt werden („Umschlag-Verschlüsselung“): Der Freigabe-Schlüssel wird für
  jedes Mitglied mit dessen öffentlichem Personenschlüssel verpackt und so über den
  Server verteilt. Der Server sieht nur die verpackten Schlüssel.
- Personenschlüssel und Tresorschlüssel liegen nur verpackt auf dem Server
  (verpackt mit dem Wiederherstellungsschlüssel).
- Verfahren: X25519, Ed25519, XChaCha20-Poly1305 bzw. AES-GCM über **Google Tink**
  oder libsodium – keine Eigenbauten. [E, P: Tink-Formate in Kotlin und Go]
- MLS (RFC 9420) ist für kleine Gruppen überdimensioniert, höchstens späteres Lernthema.

### 4.2 Metadaten (bewusst nicht verschlüsselt)
Konto-IDs, öffentliche Schlüssel, wer in welcher Freigabe ist, Sequenznummern,
Zeitpunkte, Größen, FCM-Tokens. Der Server weiß also, **wer mit wem wie oft** teilt,
aber nicht **was**. Wird in der Datenschutzerklärung so benannt.

### 4.3 EU-Datenhaltung
- D1 Alle Ressourcen in einer EU-Region; per Organisationsrichtlinie
  (`gcp.resourceLocations`) erzwingen, falls eine Organisation angelegt wird. [F9]
- D2 Global arbeitende Google-Dienste nur inhaltsfrei: **Google-Login** (nur Prüfung
  des Tokens), **FCM** (leerer Push). Kein Firebase Auth.
- D3 Vertex AI nur über regionale EU-Endpunkte; Caching und Datenspeicherung des
  Modells so weit möglich abschalten. [P]
- D4 Logs in einem Log-Bucket in der EU-Region, kurze Aufbewahrung.

### 4.4 DSGVO und Play Store
- P1 Für den Haushalt greift wohl die Haushaltsausnahme; das Projekt verhält sich
  trotzdem, als gälte die DSGVO voll – mit dem Play Store ist das ohnehin Pflicht.
- P2 Privacy by Design/Default (Art. 25): Datenminimierung, E2E, Ablaufzeiten.
- P3 Google Cloud Data Processing Addendum; Drittlandbezug dokumentieren (EU-US DPF).
- P4 Verzeichnis der Verarbeitungstätigkeiten, Datenschutzerklärung (öffentliche URL),
  Angaben zur Datensicherheit im Play Store, Webseite zum Löschen des Kontos.
- P5 Aufbewahrung: Operationen bis zum Snapshot bzw. max. 90 Tage, Logs max. 30 Tage,
  KI-Anfragen gar nicht.

### 4.5 Sicherheit
- SE1 Nur HTTPS; Sitzungs-Tokens kurzlebig; Signaturen jeder Operation.
- SE2 Secrets im Secret Manager; kein Schlüsselmaterial im Repo.
- SE3 Dependabot, `govulncheck`, Container-Image-Scan.

## 5. Architektur [E]

| Baustein | Wahl | Warum |
|---|---|---|
| Server | **Go** auf **Cloud Run**, skaliert auf 0 | Kaltstart < 1 s, 128–256 MB RAM reichen, kleines Image; bei wenig Nutzung ist fast jede Anfrage ein Kaltstart |
| Protokoll + Test-Client | **Kotlin** (JVM-CLI) | Krypto und Konfliktauflösung laufen nur auf dem Client; das Modul wandert später in die App |
| Schnittstelle | Protobuf oder JSON mit OpenAPI | sprachneutral zwischen Go-Server und Kotlin-Client [F8] |
| Daten | **Firestore** (Native Mode, EU-Region) | Gratis-Kontingent, kein Grundpreis, Gültigkeitsdauer (TTL) für Ablaufzeiten |
| Push | Firebase Cloud Messaging | kostenlos, inhaltslos |
| KI | Vertex AI (EU-Endpunkt) | Bezahlung pro Aufruf |
| Secrets | Secret Manager | |
| Infrastruktur | **OpenTofu** | reproduzierbar, vollständig löschbar |
| CI/CD | GitHub Actions → Artifact Registry → Cloud Run, Workload Identity Federation | keine Schlüsseldateien |

- **Warum Go passt**: Da der Server keine Inhalte versteht, gibt es kaum Code, den
  man mit der App teilen müsste. Die Signaturprüfung (Ed25519) ist in der
  Go-Standardbibliothek. Go ist die am besten unterstützte Sprache auf GCP
  (Client-Bibliotheken, Cloud Run, Functions).
- **Preis dafür**: zwei Sprachen. Die Grenze ist die Schnittstelle; Verträglichkeit
  sichern Tests, die der Kotlin-Test-Client gegen den Go-Server fährt.
- Region: **europe-west1 (Belgien)** – meist günstigste EU-Preisstufe, niedriger
  CO₂-Wert; Unterschied zu Frankfurt praktisch nicht spürbar. [P]

## 6. Kosten

- KO1 Leerlauf **0 €** (Cloud Run und Firestore im Gratis-Kontingent; alte Images
  automatisch löschen). [P: Gratis-Kontingente in der gewählten Region]
- KO2 Budget **20 €/Monat**, Warnungen bei 25/50/90/100 %.
- KO3 **Kill-Switch**: Budget-Benachrichtigung → Pub/Sub → Funktion stoppt Cloud Run
  bzw. trennt das Rechnungskonto. Budgets allein stoppen nichts.
- KO4 Cloud Run `max-instances` 2, `min-instances` 0, kleinste CPU/RAM.
- KO5 KI-Kontingent pro Konto und Tag.
- Hinweis: Das Test-Guthaben läuft in der Regel nach 90 Tagen ab; der Kill-Switch
  muss vorher stehen. [P]

## 7. Qualität

- Q1 Protokoll-Modul mit Property-Based-Tests (Konfliktauflösung konvergiert immer).
- Q2 Go-Server mit Unit-Tests und Firestore-Emulator.
- Q3 Ende-zu-Ende: Test-Client simuliert mehrere Personen/Geräte gegen lokale und
  Cloud-Instanz.
- Q4 Umgebung mit einem Befehl aufbauen und **vollständig wieder löschen**.
- Q5 Dokumentation auf Deutsch (`docs/anforderungen.md`, `design.md`, `stand.md`).

## 8. Nicht-Ziele (vorerst)

- Anbindung an die App
- Dateien/Fotos/Audio in der Cloud, KI für Medien
- Weboberfläche (außer der Pflichtseite zum Löschen, später)
- Ferneinladung, Rechte pro Mitglied, öffentliche Registrierung
- Serverseitige Auswertung oder Benachrichtigung zu Uhrzeiten

## 9. Etappen [E]

| Etappe | Inhalt |
|---|---|
| B0 | GCP-Projekt, Budget + Kill-Switch, OpenTofu-Grundgerüst, CI |
| B1 | Go-Server auf Cloud Run, Google-Login, Sitzungs-Token, Geräteregistrierung |
| B2 | Kotlin-Protokoll-Modul: Schlüsselmodell, Verpacken/Entpacken, Signaturen; Test-Client mit Wiederherstellungsschlüssel |
| B3 | Kontakte per QR (im Test-Client als Text), Freigaben, Append-only-Log, Konfliktauflösung |
| B4 | FCM-Push, Snapshots, Ablaufzeiten, Person entfernen + faule Rotation, Konto löschen |
| B5 | KI-Proxy (Text) mit Vertex AI EU, Freigabe-Ablauf im Test-Client, Kontingente |
| B6 | Play-Store-Vorbereitung: Play Integrity, Datenschutzerklärung, Löschseite, Härtung |

## 10. Offene Fragen

1. **Eigene Geräte**: Sollen auch private (nicht geteilte) Daten über den Tresor
   zwischen eigenen Geräten synchronisiert bzw. als Backup gespeichert werden? Das
   wäre gleichzeitig das Backup, das in der App noch fehlt.
2. **Auffindbarkeit**: Soll man über seine E-Mail-Adresse gefunden werden können
   (für spätere Ferneinladungen), oder bleibt es bei QR/Einladungslink?
3. **Mitbekommen**: Soll man benachrichtigt werden, wenn jemand anderes einen
   geteilten Reminder erledigt („Anna hat die Brunnen gemacht“), oder läuft der Abgleich
   still?
4. **Eigentum**: Hat eine Freigabe einen Besitzer, der allein Personen hinzufügen und
   entfernen darf, oder dürfen das alle Mitglieder?
5. **Ferneinladung**: Wie wichtig ist sie für den Play Store (spätere Etappe)?
6. **Teilbare Teile**: Welche Teile eines Reminders sollen einzeln wählbar sein?
   Vorschlag: Inhalt/Zeitplan, Erledigt-Status/Verlauf, Checklisten-Stand,
   Nachweis-Text.
7. **Rechte**: Reicht anfangs „alle dürfen alles“?
8. **Schnittstelle**: Protobuf (kompakter, Codegenerierung, Lerneffekt) oder JSON
   (einfacher zu debuggen)? Empfehlung: Protobuf über HTTP (Connect-Protokoll).
9. **GCP-Organisation**: Ohne eigene Domain gibt es keine Organisation und damit
   keine Standortrichtlinie – dann sichern wir die Region über OpenTofu und Tests ab.
   Hast du eine eigene Domain (z. B. für Datenschutzerklärung/Löschseite)?
10. **Go-Erfahrung**: Kennst du Go schon, oder soll der Code bewusst erklärend sein?

## 11. Entscheidungen

| Datum | Entscheidung |
|---|---|
| 2026-11-01 | Experimentelles Backend auf GCP als Lernprojekt, vorerst ohne Anbindung an die App |
| 2026-11-01 | EU-Datenhaltung, E2E-Verschlüsselung und DSGVO als Standard; App bleibt lokal-first |
| 2026-11-01 | Synchronisation zwischen Anwendern ist Hauptfeature, Cloud-KI Nebenfeature |
| 2026-11-01 | Langfristig Play Store; anfangs nur Haushalt; später bestimmte Reminder mit bestimmten Personen teilen |
| 2026-11-01 | Kontakte anfangs nur persönlich per QR; sichere Ferneinladung später |
| 2026-11-01 | Anmeldung mit Google als Missbrauchsschutz und Zugriffssperre, zusätzlich zur E2E-Verschlüsselung |
| 2026-11-01 | Wiederherstellungsschlüssel zum Ausdrucken |
| 2026-11-01 | Was geteilt wird und mit wem, ist einzeln wählbar |
| 2026-11-01 | Änderungen sollen in Sekunden bei den anderen ankommen |
| 2026-11-01 | KI zuerst nur für Text (Bewertung, Ideen), jedes Mal ausdrückliche Freigabe |
| 2026-11-01 | Budget maximal 20 €/Monat; Region egal |
| 2026-11-01 | Server in Go (Kaltstart, Ressourcen, GCP-nativ) – Protokoll und Test-Client in Kotlin (Vorschlag, siehe Kap. 5) |
