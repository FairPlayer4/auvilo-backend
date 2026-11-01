# Anforderungen – avlreminder-backend

> Experimentelles Backend für avlreminder (Android-App, Repo `FairPlayer4/avlreminder`)
> auf Google Cloud.
> Stand: 2026-11-01 – dritter Entwurf (Backup, keine Suche, Benachrichtigungen,
> Protobuf, Rechte, bezahltes Medien-Backup als Idee), wird iterativ verfeinert.
> Siehe auch: [gcp-einrichtung.md](gcp-einrichtung.md), [go-review.md](go-review.md).
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
3. **Datenminimierung**: vom Google-Konto nur die pseudonyme Konto-ID, keine E-Mail,
   kein Name. **Keine Suche nach Personen** – einladen kann nur, wer sich kennt.
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
- ID2 Gespeichert wird nur die Google-Konto-ID (`sub`), keine E-Mail-Adresse. Es gibt
  kein Verzeichnis und keine Suche nach Personen.
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
  Später eine Ferneinladung per Link, den man über einen eigenen Kanal schickt, mit
  Fingerabdruck-Vergleich. **Keine Suche** – nur wer sich kennt, kann sich einladen.
- F2 **Freigabe** = ein geteiltes Objekt (Reminder, Checkliste, Liste) mit eigener
  Mitgliederliste, eigenem Schlüssel und eigenem Änderungs-Log. So kann jeder
  Reminder mit genau den gewünschten Personen geteilt werden. [E]
- F3 **Kreise** (z. B. „Haushalt“) sind nur eine gespeicherte Auswahl von Kontakten
  zum schnellen Teilen, kein eigener Schlüssel. Neues Haushaltsmitglied → die App
  fragt, welche bestehenden Freigaben es bekommen soll.
- F4 **Was** geteilt wird, ist pro Freigabe einzeln wählbar. Anfangs: Inhalt und
  Zeitplan, Erledigt-Status und Verlauf, Checklisten-Stand, Nachweis-Text. Ändert
  sich, wenn die Reminder in der App fertig sind. Fotos, Audio, Standort- und
  Bewegungsdaten werden nicht geteilt (App 8.4).
- F5 **Rechte**: anfangs dürfen alle Mitglieder alles (auch Personen hinzufügen und
  entfernen) – geteilt wird nur mit vertrauenswürdigen Personen. Vorbereitung für
  später, ohne Mehraufwand jetzt: [E]
  - Jedes Mitglied hat im Datenmodell schon ein Feld **Rolle** (vorerst immer
    `mitglied`).
  - Auch Änderungen an der Mitgliederliste sind signierte Operationen im Log. Damit
    lassen sich später Regeln wie „nur der Besitzer darf entfernen“ prüfen, ohne
    das Format zu ändern.
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
- S5 **Eigene Geräte und Backup**: private Daten werden über den Tresor (4.1)
  verschlüsselt zwischen den eigenen Geräten synchronisiert. Das ist gleichzeitig das
  **Backup**, das der App noch fehlt (ohne Fotos und Audio, siehe 3.6).
- S6 **Zeitnah in Sekunden**: Nach jeder Änderung schickt der Server einen
  **inhaltslosen Push über Firebase Cloud Messaging** („Freigabe X hat Neues“) an die
  Geräte der anderen Mitglieder; die holen die Änderung dann ab. FCM ist kostenlos
  und auf Android der Standardweg. Dauerverbindungen (WebSocket) würden Cloud Run
  ständig wach halten und kosten. [E]
- S7 Offline-fähig und idempotent (Operations-ID), Abgleich zusätzlich beim
  App-Start und periodisch, falls ein Push verloren geht.
- S8 Alarme und Eskalation bleiben **vollständig lokal**.
- S9 **Benachrichtigung bei Änderungen anderer** („Anna hat die Brunnen gemacht“): Die
  App erzeugt sie lokal aus der entschlüsselten Operation. Der Push selbst bleibt
  inhaltslos; der Server weiß nicht, was passiert ist. Pro Freigabe abschaltbar.

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
- B3 Strukturierte Logs ohne Inhalte, ohne dauerhaft gespeicherte IP-Adressen
  (die Anfrage-Logs von Cloud Run mit IP höchstens 30 Tage, siehe Kap. 11).

### 3.6 Idee für später: Backup von Fotos und Audio (kostenpflichtig)
Optionales Backup der Nachweis-Fotos und -Audios mit einem Kontingent, zum Preis
**kostendeckend plus etwas Puffer** – kein Gewinnziel. Nicht Teil der ersten Etappen.
- MB1 Dateien werden **auf dem Gerät mit dem Tresorschlüssel verschlüsselt**;
  Cloud Storage in der EU sieht nur Chiffrate. Gerade weil es sehr persönliche Daten
  sind, ist E2E hier Pflicht, nicht Option.
- MB2 Eigene Datenschutz-Betrachtung vor dem Start (Datenschutz-Folgenabschätzung als
  Übung), Löschung bei Kündigung nach kurzer Frist.
- MB3 Bezahlung im Play Store über **Google Play Billing** (Pflicht für digitale
  Inhalte; Google behält 15 %). Server prüft das Abo und setzt das Kontingent.
- MB4 Kosten grob: Speicher in der EU ca. 2 Cent pro GB und Monat, dazu Abruf beim
  Wiederherstellen. [P]
- MB5 Nachweise mit Standort bleiben ausgeschlossen, solange 8.4 der App gilt.

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
- D1 Alle Ressourcen in einer EU-Region. Ohne eigene Domain gibt es keine
  GCP-Organisation und damit keine Standortrichtlinie (`gcp.resourceLocations`).
  Deshalb legt OpenTofu die Region zentral fest (Positivliste der EU-Regionen), und
  die CI prüft das: `tofu test` plant mit simulierten Providern und prüft die
  Standorte (`infra/tests/eu_regionen.tftest.hcl`), `infra/pruefe-regionen.sh`
  findet fest eingetragene Standorte. Eine Organisation kann später nachgerüstet werden.
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
| Schnittstelle | **Protobuf** mit **Connect** (connectrpc.com), Schemas mit `buf` verwaltet | sprachneutral, typsicher in Go und Kotlin, läuft über normales HTTP und kann zum Debuggen auch JSON sprechen |
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
- **Ein Repo für alles** (Monorepo): `proto/` (Schnittstelle), `server/` (Go),
  `client/` (Kotlin: Protokoll-Modul und Test-Client), `infra/` (OpenTofu),
  `docs/`. Eine Änderung an der Schnittstelle ist so ein einziger Commit. [E]
- Region: **europe-west1 (Belgien)** – meist günstigste EU-Preisstufe, niedriger
  CO₂-Wert; Unterschied zu Frankfurt praktisch nicht spürbar. [P]

## 6. Kosten

- KO1 Leerlauf **0 €** (Cloud Run und Firestore im Gratis-Kontingent; alte Images
  automatisch löschen). [P: Gratis-Kontingente in der gewählten Region]
- KO2 Budget **20 €/Monat**, Warnungen bei 25/50/90/100 %.
- KO3 **Kill-Switch**: Budget-Benachrichtigung → Pub/Sub → Funktion stoppt Cloud Run
  bzw. trennt das Rechnungskonto. Budgets allein stoppen nichts.
  **Kosten gehen vor Verfügbarkeit**: Hohe Kosten deuten auf einen Fehler oder einen
  Angriff hin. Ein vom Kill-Switch gesperrter Dienst bleibt gesperrt, bis er bewusst
  von Hand wieder geöffnet wird – auch ein `tofu apply` öffnet ihn nicht
  (`lifecycle { ignore_changes = [ingress] }` an jedem Cloud-Run-Dienst).
- KO3a Jeder Cloud-Run-Dienst läuft mit **eigenem Dienstkonto**; dieses wird in
  `cloud_run_laufzeitkonten` (infra/kosten.tf) eingetragen, damit der Kill-Switch
  ihn sperren darf. Der Kill-Switch selbst darf nur Dienste lesen und ändern.
- KO4 Cloud Run `max-instances` 2, `min-instances` 0, kleinste CPU/RAM.
- KO5 KI-Kontingent pro Konto und Tag.
- Hinweis: Das Test-Guthaben läuft in der Regel nach 90 Tagen ab; der Kill-Switch
  muss vorher stehen. [P]

## 7. Qualität

Kevin liest vor allem **Tests und kritischen Code**, nicht den ganzen Code. Deshalb
tragen die Tests die Spezifikation. Anleitung zum Review: [go-review.md](go-review.md).

- Q1 **Tests als lesbare Spezifikation**: tabellengetriebene Tests, Testfälle mit
  deutschen Namen, die das Verhalten beschreiben („lehnt Schreiben ohne
  Mitgliedschaft ab“). Zu jeder Erlaubnis gehört ein Test für die Ablehnung.
- Q2 **Kritischer Code ist markiert und klein**: Anmeldung, Prüfung der
  Mitgliedschaft, Signaturprüfung, Löschen und Kosten-Grenzen liegen in eigenen,
  kurzen Paketen mit Kommentar `// KRITISCH:` und sehr hoher Testabdeckung.
- Q3 Code bewusst **erklärend kommentiert** (Go-Eigenheiten kurz begründet), da Go
  neu ist.
- Q4 Prüfwerkzeuge in CI und lokal: `go vet`, `golangci-lint`, `go test -race`,
  `govulncheck`, `buf lint` und `buf breaking` (keine versehentlichen
  Schnittstellenbrüche).
- Q5 Go-Server mit Firestore-Emulator getestet; Protokoll-Modul (Kotlin) mit
  Property-Based-Tests (Konfliktauflösung konvergiert immer).
- Q6 Ende-zu-Ende: Test-Client simuliert mehrere Personen/Geräte gegen lokale und
  Cloud-Instanz.
- Q7 Umgebung mit einem Befehl aufbauen und **vollständig wieder löschen**.
- Q8 Dokumentation auf Deutsch (`docs/anforderungen.md`, `design.md`, `stand.md`).

## 8. Nicht-Ziele (vorerst)

- Anbindung an die App
- Dateien/Fotos/Audio in der Cloud (Idee 3.6), KI für Medien
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

1. **Ferneinladung**: Wie wichtig ist sie für den Play Store? (Spätere Etappe.)

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
| 2026-11-01 | Server in Go (Kaltstart, Ressourcen, GCP-nativ) – Protokoll und Test-Client in Kotlin (siehe Kap. 5) |
| 2026-11-01 | Private Daten werden über den Tresor zwischen eigenen Geräten synchronisiert (= Backup) |
| 2026-11-01 | Idee für später: kostenpflichtiges, E2E-verschlüsseltes Backup von Fotos/Audio, kostendeckend |
| 2026-11-01 | Keine Suche nach Personen; einladen kann nur, wer sich kennt |
| 2026-11-01 | Benachrichtigung, wenn andere etwas an einer Freigabe ändern |
| 2026-11-01 | Rechte anfangs: alle Mitglieder dürfen alles; Rolle im Datenmodell schon vorgesehen |
| 2026-11-01 | Teilbare Teile anfangs: Inhalt/Zeitplan, Erledigt-Status/Verlauf, Checklisten-Stand, Nachweis-Text |
| 2026-11-01 | Schnittstelle mit Protobuf (Connect, buf) |
| 2026-11-01 | Keine eigene Domain und keine GCP-Organisation vorerst |
| 2026-11-01 | Code erklärend; gute Tests sind die Hauptgrundlage für Kevins Review |
| 2026-11-01 | GCP-Projekt `avlreminder-exp-1ef2`, Region europe-west1; B0 angelegt (siehe stand.md) |
| 2026-11-01 | Kill-Switch trennt nur dieses Projekt vom Rechnungskonto, nicht das ganze Konto (Schwellen vorläufig 100 %/150 %) |
| 2026-11-01 | Budget zählt Kosten vor Abzug von Guthaben |
| 2026-11-01 | Kosten vor Verfügbarkeit: gesperrte Dienste öffnet nur ein bewusster manueller Schritt, nie ein `tofu apply` |
| 2026-11-01 | Jeder Cloud-Run-Dienst mit eigenem Dienstkonto; Editor-Rolle der Google-Standardkonten entfernt |
| 2026-11-01 | Budget zählt auch Gratis-Kontingent mit (`EXCLUDE_ALL_CREDITS`) – erlaubt später einen echten Test des Kill-Switch |
| 2026-11-01 | Keine Pflicht-Statuschecks auf GitHub (Free, privat); „main nur per PR, CI grün“ ist Konvention |
| 2026-11-01 | Kill-Switch-Schwellen bestätigt: ab 100 % Dienste sperren, ab 150 % Projekt vom Rechnungskonto trennen |
| 2026-11-01 | B1: Gerät holt neue Sitzungen mit Signatur seines Geräteschlüssels, kein Refresh-Token ([b1-entwurf.md](b1-entwurf.md)) |
| 2026-11-01 | B1: Geräteschlüssel ECDSA P-256 (Android Keystore inkl. StrongBox); Personen- und Freigabeschlüssel bleiben wie in 4.1 |
| 2026-11-01 | B1: Sitzungs-Token zufällig, 1 Stunde gültig, auf dem Server nur als Hash; Sperren wirkt sofort |
| 2026-11-01 | B1: Google-Konto-ID nur als HMAC mit geheimem Pfeffer (Secret Manager) gespeichert |
| 2026-11-01 | B1: Test mit Go-Werkzeug `testanmeldung`; Kotlin-Test-Client ab B2 |
| 2026-11-01 | Namen in Schnittstelle und Code deutsch, ohne Umlaute |
| 2026-11-01 | Abgefangene Anfragen dürfen sich im Zeitfenster wiederholen (Anmelden ca. 5 min, Erneuern ±2 min); keine Merkliste benutzter Signaturen. Schutz: TLS, kurze Fenster, Sitzung 1 h (Test `TestWiederholteAnfrageImZeitfensterGiltBewusst`) |
| 2026-11-01 | Geräte-IDs (pseudonym) dürfen in Logs stehen, zur Fehlersuche und Missbrauchsabwehr (Art. 6 Abs. 1 f). Einzeln löschen geht in Cloud Logging nicht; stattdessen: Konto löschen trennt sofort die Zuordnung (Firestore), Logs laufen nach 30 Tagen ab (P5). In die Datenschutzerklärung (P4); vor B6 von jemandem mit Datenschutzerfahrung prüfen lassen |
| 2026-11-01 | Anfrage-Logs von Cloud Run (mit IP-Adresse und User-Agent) bleiben 30 Tage in `logs-eu`, zur Missbrauchsabwehr (Art. 6 Abs. 1 f); „nicht dauerhaft“ im Sinne von B3. In die Datenschutzerklärung (P4) |
| 2026-11-01 | Keine Offline-Kopie des Pfeffers, solange es keine echten Nutzer gibt; Schutz in GCP (`prevent_destroy`, 30 Tage wiederherstellbar). Vor echtem Betrieb neu entscheiden |
| 2026-11-01 | B2: Krypto mit Google Tink (JVM und Android); Personenschlüssel = HPKE/X25519 + Ed25519, Tresor- und Freigabe-Schlüssel XChaCha20-Poly1305 ([b2-entwurf.md](b2-entwurf.md)) |
| 2026-11-01 | B2: Wiederherstellungsschlüssel = 256 Bit Zufall, gedruckt als Base32 in Vierergruppen mit Prüfzeichen |
| 2026-11-01 | B2: Server speichert je Konto ein verschlüsseltes Schlüssel-Backup mit Version (kein Überschreiben eines neueren); Konto-ID als zugehörige Daten |
| 2026-11-01 | B2: Erstes Gerät (neues_konto) erzeugt die Schlüssel; neues Gerät per QR erst in B3 |
| 2026-11-01 | B2: Kotlin-Code aus proto im Gradle-Build erzeugt (javalite, connect-kotlin), nicht eingecheckt; gemeinsame Testvektoren für Kotlin und Go |
