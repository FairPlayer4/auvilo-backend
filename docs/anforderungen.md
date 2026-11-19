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
- ID4 **Wiederherstellungsschlüssel** (zum Ausdrucken oder im Passwortdienst, App-Anf. 7.33) entschlüsselt ein
  verschlüsseltes Schlüssel-Backup auf dem Server. Ohne ihn und ohne Gerät sind
  private Daten verloren (gewollt); Geteiltes kann man sich neu freigeben lassen.
  **Hauptweg ist ein Ende-zu-Ende-verschlüsselter Passwortmanager** (Kap. 11, 2026-11-15),
  Papier der Ausweg. Vor dem Speichern sagt die App: Ohne geräteinterne Verschlüsselung
  des Passwortmanagers hängt die E2E-Verschlüsselung am Google-Konto; den
  Passwortmanager selbst mit seinem Wiederherstellungscode auf Papier schützen.
- ID5 Missbrauchsschutz: Rate-Limits pro Konto und Gerät; später für den Play Store
  zusätzlich **Play Integrity API** (Anfrage kommt von der echten App). [E]
- ID6 **Gerät abmelden/sperren** aus einem anderen Gerät heraus (verloren, gestohlen).
- ID7 **Zugang nur nach Freigabe** (2026-11-14, umgesetzt und deployt mit PR #25; App-Anf. 7.34 OV1): Der Server
  nimmt nur Google-Konten an, deren **Kennung** (`anmeldung.Kennung`, HMAC aus Pfeffer
  und `sub`) auf einer Liste steht. Nicht die Konto-ID: Die ist zufällig und nach
  *Konto löschen* neu.
  - Liste wie `DIAGNOSE_KONTEN` als Umgebungsvariable `FREIGEGEBENE_KENNUNGEN` über
    OpenTofu (`freigegebene_kennungen` in `werte.auto.tfvars`); ein Firestore-Dokument
    mit Admin-Befehl erst, wenn sie sich oft ändert. **Leer heißt niemand**; wer den
    Server selbst betreibt und offen haben will, setzt ausdrücklich `*` (nur allein).
    Ein Tippfehler (nicht 64 Zeichen 0-9a-f) verhindert Plan und Serverstart.
  - Antwort `failed_precondition` mit Fehlerdetail `NichtFreigegeben { kennung }`
    (anmeldung.proto); Client: `KontoNichtFreigegeben`, der Test-Client nennt die Kennung.
  - **Entzug** wirkt auch beim Erneuern (Kevin, 2026-11-14): `SitzungErneuern` liest die
    Kennung des Kontos (eine Firestore-Abfrage, nur wenn nicht `*`) und lehnt ab, wenn
    sie nicht mehr auf der Liste steht. Wer gestrichen wird, ist spätestens nach einer
    Stunde draußen.
  - Startliste (Kevin, 2026-11-14): App- und Test-Konto. Am 2026-11-14 gab es in
    Firestore nur eine Kennung (Konto `65eab9f8…`); das alte Testkonto ist gelöscht.
    Die Kennung des Test-Kontos kommt beim ersten abgelehnten Anmelden des Test-Clients.
  - Nicht freigegeben: Der Server prüft das Google-Token, legt aber **nichts** an (kein
    Konto, keine Kennung, keine Sitzung, kein Gerät, kein Tageszähler) und antwortet
    mit „nicht freigegeben“ und der Kennung. Die App zeigt sie mit „Schick diesen Code
    an den Betreiber“; Kevin trägt sie ein. Die Prüfung kommt vor jedem Firestore-Zugriff.
  - Ein neuer Pfeffer (Sicherheitsphase, Punkt 3) ändert alle Kennungen: danach die
    Liste neu füllen (zuerst Kevins neue Kennung).
  - Wer einen eigenen Server betreibt, setzt in der App eigene Server-URL und
    Web-Client-ID (App-Anf. 7.34 OV1); der offizielle Server bleibt geschlossen.
- ID8 **Kosten bei Angriffen begrenzen** (2026-11-14, umgesetzt und deployt mit PR #25): Die Adresse ist kein
  Geheimnis (sie steht in jeder APK und im Repo); geschützt wird über Grenzen, nicht
  über Verstecken. Schon da: `max_instance_count` 2, Timeout 30 s, Größenlimits je
  Methode, Tageszähler je Konto, Alarm bei viel ausgehendem Verkehr, Kill-Switch über
  das Budget. Ergänzen:
  - **Ungültiges früh und ohne Firestore ablehnen:** Heute liest der Interceptor für
    jedes Bearer-Token eine Sitzung aus Firestore, also kostet auch ein erfundenes Token
    einen Lesezugriff. Sitzungs-Token mit Server-Signatur (HMAC), die vor dem Lesen
    geprüft wird; Anmelden mit nicht freigegebener Kennung ohne Firestore (ID7).
    Umgesetzt: Token = base64url(256 Bit Zufall) „.“ base64url(HMAC, 128 Bit), Schlüssel
    aus dem Pfeffer abgeleitet (`sitzung.Aussteller`). Alte Tokens gelten nach dem
    Deploy nicht mehr; die Geräte erneuern einmal (wie nach einer Rotation).
    Die Vorprüfung ohne Firestore gilt für Bearer-Tokens. Die offenen Prozeduren prüfen
    vor dem Speicher nur, was nichts kostet: `SitzungErneuern` lehnt einen falschen
    Zeitpunkt ohne Lesen ab (seit 2026-11-19, Review-Lauf 11); eine erfundene Geräte-ID
    mit passendem Zeitpunkt kostet einen Lesezugriff. Das deckt der Anfrage-Alarm.
  - **Schneller als das Budget:** Abrechnungsdaten kommen mit Stunden Verzug, eine Flut
    kann bis dahin über 20 € kosten. Umgesetzt: Alarm `anfragen` (alarm.tf) auf
    `run.googleapis.com/request_count` des Servers, **mehr als 10.000 je Stunde**
    (Kevin, 2026-11-14; normal sind etwa 30 je Gerät und Tag). Er geht per Pub/Sub ins
    Thema `budget-meldungen`; der Kill-Switch erkennt Alarme am Feld `incident` und
    sperrt bei offenem Alarm mit Label `killswitch=sperren` die Dienste, trennt aber nie
    die Abrechnung. Dazu eine Mail.
  - Nicht umgesetzt: kürzerer Timeout (Tests zeigen, dass die 30 s für 8 MiB über
    langsames Netz nötig sind; für eine Flut kleiner Anfragen spart er nichts) und
    Grenze je IP (bei höchstens 2 Instanzen und Angreifern mit vielen Adressen wenig
    Nutzen; der Anfrage-Alarm deckt die Flut ab). Play Integrity erst mit dem Play
    Store (ID5). Cloud Armor braucht einen Load Balancer mit Grundgebühr über dem
    Budget, deshalb nicht.

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
- K7 **Erster Anwendungsfall: Schnellerinnerung aus Sprache** (App-Anf. 7.35,
  2026-11-15). Die App erkennt Sprache und Zeitangabe lokal und legt die Erinnerung
  sofort an. Das Backend wertet nur Sätze aus, die der lokale Parser nicht versteht
  („nach dem Essen“, „morgen früh vor der Arbeit“), und nur nach Freigabe je Anfrage
  (K3). Die App bleibt ohne Backend voll nutzbar. [E]
  - Schnittstelle wie sonst Protobuf über Connect; JSON nur zwischen Go und Vertex AI,
    mit strukturierter Ausgabe (`responseSchema`), damit die Antwort dem Schema folgt.
  - Anfrage: der Satz, dazu Ortszeit mit Zeitzone und Wochentag. Antwort: Text,
    `in_minuten` oder eine Uhrzeit, `sicher` (ja/nein), Modell- und Promptversion (K4).
    Die KI rechnet keine Uhrzeiten um; das macht die App.
  - Der Server prüft die Antwort (`// KRITISCH:`, tabellengetrieben mit
    Ablehnungsfällen): Text nicht leer und begrenzt lang, Zeit zwischen 1 min und
    7 Tagen. Ist sie ungültig, gibt es einen zweiten Versuch, danach einen Fehler; die
    App behält dann ihre lokale Erinnerung. Die App prüft die Antwort noch einmal.
  - Modell: ein kleines, schnelles auf dem EU-Endpunkt (z. B. Gemini Flash-Lite). Ein
    Aufruf kostet Bruchteile eines Cents; das Kontingent aus K6 gilt.
  - Der Satz wird nicht gespeichert und nicht geloggt (K4); diktierte Sätze enthalten
    leicht Dritte oder Gesundheitsdaten.

### 3.5 Verwaltung und Betrieb
- B1 **Konto löschen** in der App **und über eine Webseite** (Pflicht im Play Store)
  löscht sofort Konto, Geräte, Mitgliedschaften und eigene Operationen, dazu die
  Datensicherung (alle Fassungen), das Schlüssel-Backup, Diagnosen, Sitzungen,
  Tageszähler und die Kennung, damit eine neue Anmeldung ein neues Konto bekommt.
  Erst den Termin setzen, dann den Zugang abschneiden (Kennung, Sitzungen, Geräte
  gesperrt), dann die Inhalte, zuletzt Geräte, Schlüssel-Backup und Konto: Bricht es
  ab, findet der regelmäßige Lauf das Konto über den Termin und macht weiter
  (Kap. 11, 2026-11-12, Umsetzung). Tageszähler laufen von selbst ab (TTL nach etwa
  zwei Tagen, Firestore löscht danach mit Verzögerung; zusammen wenige Tage). In der App vor dem ersten weiteren Nutzer (vorgezogen aus B4,
  Kap. 11, 2026-11-12).
  **Ohne Zugang** (Kap. 11, 2026-11-12):
  - Ein *freigeschaltetes* Gerät löscht sofort. Ein nicht freigeschaltetes Gerät
    (nur Google-Login, z. B. Handy und Wiederherstellungsschlüssel verloren) darf die
    Löschung nur **beantragen**: Der Server setzt `loeschen_ab` auf 7 Tage später.
    Bis dahin läuft das Konto normal weiter, und jedes freigeschaltete Gerät kann den
    Antrag abbrechen (`KontoLoeschenAbbrechen`). Die Wartezeit schützt davor, dass
    jemand mit einem übernommenen Google-Konto sofort alles vernichtet.
  - Vorhandene Geräte erfahren vom Antrag bei ihrem nächsten Aufruf (der stündliche
    Worker reicht, Push erst mit B4); die App zeigt eine Benachrichtigung mit
    *Abbrechen*.
  - Fällige Anträge arbeitet derselbe regelmäßige Lauf ab wie die Fristen aus P5;
    es gibt nur einen Löschpfad.
  - Die **Webseite** nutzt dieselbe Regel: „Mit Google anmelden“ (Web-Client), der
    Server prüft das ID-Token wie bei der App und setzt den Antrag. Bis B6 genügt
    dort die Anleitung „App installieren, anmelden, Konto löschen“.
  - Ist auch das **Google-Konto** weg, lässt sich das Konto nicht zuordnen (es gibt
    weder E-Mail noch Namen, nur den HMAC der Google-ID). Das deckt Art. 11 DSGVO,
    wenn die Datenschutzerklärung es sagt; die Frist aus P5 löscht nach 24 Monaten
    ohne Anmeldung. Eine Mail-Adresse ist kein Löschweg, sie bekommt nur die
    Anleitung.
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
| **Wiederherstellungsschlüssel** | symmetrisch, ausgedruckt oder im Passwortdienst | Papier oder Passwortdienst | entschlüsselt das Backup von Personen- und Tresorschlüssel |

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
aber nicht **was**. Auch pseudonym bleiben das personenbezogene Daten, ebenso die
IP-Adressen in den Anfrage-Logs (30 Tage). Größe und Zeitpunkt der Sicherungen
zeigen, wie viel jemand die App nutzt; auf feste Größen aufgefüllt wird vorerst
nicht (Kap. 11, 2026-11-12). Wird in der Datenschutzerklärung so benannt.

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
- P3 Google Cloud Data Processing Addendum (in der Console akzeptieren); Drittlandbezug
  dokumentieren (EU-US DPF): Google-Anmeldung (dort ist Google selbst Verantwortlicher),
  FCM und der globale Log-Bucket `_Required` (Admin-Audit, vor allem Kevins Aktionen).
- P4 Verzeichnis der Verarbeitungstätigkeiten, Datenschutzerklärung (öffentliche URL),
  Angaben zur Datensicherheit im Play Store, Webseite zum Löschen des Kontos. Dazu
  die TOMs (Art. 32) auf einer Seite, ein Ablauf für Datenpannen (Art. 33/34,
  72 Stunden) und vor dem ersten Tester eine Testerinformation. Entwürfe und offene
  Punkte: [datenschutz/README.md](datenschutz/README.md) (seit 2026-11-14).
- P5 Aufbewahrung: Operationen bis zum Snapshot bzw. max. 90 Tage, Logs max. 30 Tage,
  KI-Anfragen gar nicht. Ein Konto ohne Anmeldung und ohne Erneuerung der Sitzung
  seit 24 Monaten bekommt einen Löschtermin in 7 Tagen und wird dann auf demselben
  Weg wie B1 gelöscht (Kap. 3.5). Gesperrte Geräte werden nach 90 Tagen durch einen
  Grabstein ersetzt (nur Geräte-ID und Konto, bis das Konto gelöscht wird). Beides
  macht der tägliche Lauf `aufraeumen`; umgesetzt 2026-11-14 (Kap. 11).
- P6 **Rechtsgrundlagen:** Vertrag (Art. 6 Abs. 1 lit. b) für Anmeldung, Sicherung und
  Teilen; berechtigtes Interesse (lit. f) für Server-Logs, Limits und
  Missbrauchsschutz; Einwilligung (lit. a, je Sendung) für Diagnosen.
- P7 **DSFA** (Art. 35) vor dem ersten weiteren Nutzer: Erinnerungen können
  Gesundheitsdaten sein (Medikamente, Arzttermine; Art. 9). Wegen E2E liest der
  Betreiber sie nie; die DSFA hält fest, warum das trägt.
- P8 **Mindestalter** 16 Jahre (Art. 8). Ob jüngere Familienmitglieder mit Einwilligung
  der Eltern teilen dürfen, wird vor B3 entschieden (Kap. 10).
- P9 **Diagnosen von Testern** liest Claude erst, wenn es dafür einen
  Auftragsverarbeitungsvertrag mit Anthropic gibt (Übermittlung in die USA). Bis dahin
  hat nur Kevins Konto eine Diagnose-Erlaubnis (E-D-3).
- P10 **Löschseite im Web** (Play-Richtlinie zur Kontolöschung, festgehalten 2026-11-15):
  Wer die App nicht mehr hat, kann dort die Löschung seines Kontos beantragen, ohne die
  App neu zu installieren. Die Adresse kommt ins Formular zur Datensicherheit der
  Play Console. **Vor dem Play-Store-Release** (frühestens mit App-V2), nicht vorher.
  - Die Seite nennt App- und Entwicklernamen wie im Store, die Schritte, was gelöscht
    wird (alles des Kontos, für alle Geräte), was bleibt und wie lange (Server-Logs
    30 Tage, Grabsteine, Kap. 3.5) und den Weg in der App (*Konto löschen*, R-3).
  - **Mit Google anmelden** auf der Seite: Der Server bildet aus dem ID-Token das
    Pseudonym wie bei der App-Anmeldung und stellt einen **Löschantrag** (B1, K1): Ohne
    freigeschaltetes Gerät gilt die Wartezeit von 7 Tagen, die anderen Geräte zeigen
    den Antrag an und können ihn abbrechen. Eine E-Mail allein reicht nicht, weil der
    Server keine E-Mail-Adressen kennt und sie keinem Konto zuordnen kann; eine
    Kontaktadresse steht trotzdem als Ausweg da (z. B. wenn der Dienst gesperrt ist).
  - Keine Cookies außer für die Anmeldung, keine Analyse, keine fremden Skripte außer
    Googles Anmeldung. Server-Logs wie beim Dienst (30 Tage).
  - Auf derselben Seite die **Datenschutzerklärung** (P4, öffentliche Adresse für Play
    und den OAuth-Zustimmungsbildschirm).
  - **Hosting:** eigener kleiner Cloud-Run-Dienst `web` in `var.region` (statische
    Seiten), mit eigenem Dienstkonto und öffentlich über `infra/module/oeffentlich`.
    Den Löschantrag nimmt der bestehende Dienst an (neue Zielgruppe: Web-Client-ID).
    Kosten im kostenlosen Kontingent von Cloud Run, Adresse `*.run.app` ohne Domain.
    Nicht GitHub Pages, Firebase Hosting oder ein öffentlicher Bucket: Die ersten beiden
    liefern nicht nur aus der EU aus, ein Bucket mit HTTPS unter eigener Adresse braucht
    einen Load Balancer (rund 18 €/Monat).
  - **Domain** (offene Frage Kap. 10): Für Play reicht `*.run.app`. Geht der
    OAuth-Zustimmungsbildschirm von *Testen* in *Produktion*, verlangt Google für Startseite
    und Datenschutzerklärung wahrscheinlich eine eigene, in der Search Console bestätigte
    Domain (`run.app` lässt sich nicht bestätigen). Dann eine günstige Domain (etwa
    5–15 €/Jahr) per Domain-Zuordnung auf denselben Dienst.

### 4.5 Sicherheit
- SE1 Nur HTTPS; Sitzungs-Tokens kurzlebig; Signaturen jeder Operation (gemeint sind
  die Sync-Operationen ab B3; in B1/B2 signieren Anmelden und Erneuern, alles andere
  trägt das Sitzungs-Token, siehe Kap. 11).
- SE2 Secrets im Secret Manager; kein Schlüsselmaterial im Repo.
- SE3 Dependabot, `govulncheck`, Container-Image-Scan (erfüllt durch distroless,
  `govulncheck` und Dependabot, siehe Kap. 11).
- SE4 **Krypto im Client, Prüfen auch im Server** (geschärft 2026-11-10, E13): Inhalte
  ver- und entschlüsseln nur die Clients; der Server kennt keinen Schlüssel zu Inhalten.
  Er prüft aber Signaturen, die über Zugang entscheiden (Anmelden mit dem
  Geräteschlüssel, Freischalten mit dem aus dem Wiederherstellungsschlüssel
  abgeleiteten Schlüssel). Begriffe: Ein Gerät ist *freigeschaltet* (E-B2b-13), ein
  Konto hat eine *Diagnose-Erlaubnis* (E-D-3).
- SE5 **Zerstörerische Aktionen** (Konto löschen, Sicherung löschen, Gerät sperren;
  umgesetzt in der App V1.3c2 (2026-11-15, noch nicht installiert); Gerät sperren gibt es nur im Test-Client)
  nur nach Bestätigung mit der Displaysperre des Geräts (BiometricPrompt, auch mit
  Geräte-PIN). Sonst löscht, wer ein entsperrtes Handy in die Hand bekommt, alle
  Fassungen endgültig (kein Soft Delete). Eine Wartezeit im Server erst, wenn es
  mehrere Nutzer gibt (Kap. 11, 2026-11-12).
- SE6 **Ein Schlüssel, ein Zweck:** Aus dem Wiederherstellungsschlüssel wird jeder
  benutzte Schlüssel per HKDF mit eigenem Label abgeleitet (Schlüssel-Backup und
  Freischaltschlüssel); heute dient er zugleich direkt als AEAD-Schlüssel. Als
  Format v2 vor der Rotation, weil danach kein altes Backup mehr gelesen werden muss.

### 4.6 Vorgaben für B3 (Review-Lauf 9, A2 und A4, festgehalten 2026-11-10; B3-5 bis B3-8 aus der Sicherheitsprüfung vom 2026-11-12)
- B3-1 **Die Geräteliste des Servers ist nur eine Zugangssperre.** Maßgeblich für die
  Clients ist das signierte Log der Geräte (wer wen hinzugefügt oder entfernt hat).
  Ein Client vertraut der Liste vom Server nicht, sondern prüft das Log; weicht die
  Liste davon ab, meldet er das (Diagnose-Ereignis und Hinweis), statt ihr zu folgen.
  Ein manipulierter Server kann so Geräte aussperren, aber keines unbemerkt
  hinzufügen.
- B3-2 **Umschläge sind an ihren Zusammenhang gebunden:** Freigabe-ID und Version
  gehen als zusätzliche Daten (AAD) in die Verschlüsselung ein, damit ein Server
  einen Umschlag nicht an eine andere Freigabe oder eine ältere Fassung hängen kann.
- B3-3 **Der Absender signiert jeden Umschlag** (Ed25519 seines Personenschlüssels);
  der Empfänger prüft die Signatur gegen das Log aus B3-1, bevor er entschlüsselt.
- B3-4 **`ausGeheimenBytes` prüft den Keyset-Typ:** Ein Keyset der falschen Art
  (z. B. ein Signaturschlüssel, wo ein Tresorschlüssel erwartet wird) wird
  abgelehnt, bevor es benutzt wird.
- B3-5 **Der Server kann nichts unbemerkt weglassen.** Jede Operation einer Freigabe
  trägt eine fortlaufende Nummer je Absender und den Hash der letzten Operation, die
  der Absender kannte, beides unter der Signatur aus B3-3. Ein Client meldet Lücken
  (fehlende Nummer, unbekannter Vorgänger), statt darüber hinwegzugehen. Hält der
  Server Mitglieder in getrennten Verläufen, fällt das auf, sobald eine Operation aus
  dem einen Zweig im anderen ankommt. Signaturen allein erkennen Zurückhalten nicht
  (etwa „Person entfernt“ oder eine Terminabsage).
- B3-6 **Person entfernen (B4) heißt neuer Freigabe-Schlüssel**, spätestens vor der
  nächsten Änderung (faule Rotation); was sie schon hatte, behält sie. Die App sagt
  das beim Entfernen. Das Format aus B3 sieht die Versionen dafür vor (B3-2).
- B3-7 **Öffentliche Personenschlüssel nur nach Vergleich des Fingerabdrucks** (QR,
  F1). Einen Schlüssel, den nur der Server liefert, benutzt kein Client; so bleibt
  auch ein manipulierter Server ohne Zugriff auf Inhalte.
- B3-8 **Bekannte Grenze bis B4:** Personenschlüssel sind langlebig; wer einen
  erbeutet, liest bis zur Rotation (B4) alles mit. In Datenschutzerklärung und TOMs
  benennen.

## 5. Architektur [E]

| Baustein | Wahl | Warum |
|---|---|---|
| Server | **Go** auf **Cloud Run**, skaliert auf 0 | Kaltstart < 1 s, 128–256 MB RAM reichen, kleines Image; bei wenig Nutzung ist fast jede Anfrage ein Kaltstart |
| Protokoll + Test-Client | **Kotlin** (JVM-CLI) | Krypto und Konfliktauflösung laufen nur auf dem Client; das Modul wandert später in die App |
| Schnittstelle | **Protobuf** mit **Connect** (connectrpc.com), Schemas mit `buf` verwaltet | sprachneutral, typsicher in Go und Kotlin, läuft über normales HTTP und kann zum Debuggen auch JSON sprechen |
| Daten | **Firestore** (Native Mode, EU-Region) | Gratis-Kontingent, kein Grundpreis, Gültigkeitsdauer (TTL) für Ablaufzeiten |
| Datensicherungen | **Cloud Storage** (EU-Region, versioniert) | ein Chiffrat je Konto bis 8 MiB, ältere Fassungen als Rückfallebene (B2b) |
| Push | Firebase Cloud Messaging | kostenlos, inhaltslos |
| KI | Vertex AI (EU-Endpunkt) | Bezahlung pro Aufruf |
| Secrets | Secret Manager | |
| Infrastruktur | **OpenTofu** | reproduzierbar, vollständig löschbar |
| CI/CD | GitHub Actions → Artifact Registry → Cloud Run, Workload Identity Federation | keine Schlüsseldateien |

- **Warum Go passt**: Da der Server keine Inhalte versteht, gibt es kaum Code, den
  man mit der App teilen müsste. Was er prüft (Ed25519-Signaturen für Anmelden und
  Freischalten, SE4), kann die Go-Standardbibliothek. Die Signaturen zwischen
  Personen (B3-1, B3-3) prüfen dagegen nur die Clients. Go ist die am besten unterstützte Sprache auf GCP
  (Client-Bibliotheken, Cloud Run, Functions).
- **Preis dafür**: zwei Sprachen. Die Grenze ist die Schnittstelle; Verträglichkeit
  sichern Tests, die der Kotlin-Test-Client gegen den Go-Server fährt.
- **Ein Repo für alles** (Monorepo): `proto/` (Schnittstelle), `server/` (Go),
  `client/` (Kotlin: Protokoll, Verbindung zum Server, Test-Client), `infra/` (OpenTofu),
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
  von Hand wieder geöffnet wird – auch ein `tofu apply` öffnet ihn nicht, nur
  `tofu apply -var=gesperrte_dienste_oeffnen=true` (Modul `infra/module/oeffentlich`).
- KO3a Jeder Cloud-Run-Dienst läuft mit **eigenem Dienstkonto**. Der Kill-Switch
  sperrt, indem er dem Internet (`allUsers`) das Aufrufrecht entzieht; er darf nur
  Dienste auflisten und ihre IAM-Richtlinie setzen, aber keinen Dienst ändern und
  kein Laufzeitkonto benutzen (kein actAs).
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
- Q8 Dokumentation auf Deutsch (`docs/anforderungen.md`, Entwürfe je Etappe, `stand.md`).

## 8. Nicht-Ziele (vorerst)

- Anbindung an die App über das Daten-Backup (B2b) hinaus
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
| B2b | Daten-Backup für eine Person (Tresorschlüssel, Cloud Storage EU, ältere Fassungen), Formate festschreiben, `client/` bereit für die App; danach Einbindung als Backup in eine interne App-Variante ([b2b-entwurf.md](b2b-entwurf.md)) |
| B3 | Kontakte per QR (im Test-Client als Text), Freigaben, Append-only-Log, Konfliktauflösung |
| B4 | FCM-Push, Snapshots, Ablaufzeiten, Person entfernen + faule Rotation (Konto löschen kommt vorher, Kap. 11, 2026-11-12) |
| B5 | KI-Proxy (Text) mit Vertex AI EU, Freigabe-Ablauf im Test-Client, Kontingente; erster Anwendungsfall Schnellerinnerung aus Sprache (K7) |
| B6 | Play-Store-Vorbereitung: Play Integrity, Datenschutzerklärung, Löschseite, Härtung |

## 10. Offene Fragen

1. **Ferneinladung**: Wie wichtig ist sie für den Play Store? (Spätere Etappe.)
2. **Kinder in der Familie:** Dürfen Familienmitglieder unter 16 mit Einwilligung der
   Eltern teilen (P8)? Vor B3.
3. **Diagnosen von Testern:** AV-Vertrag mit Anthropic, oder bleiben Tester ohne
   Diagnose-Erlaubnis (P9)? Vor dem ersten Tester, der Diagnosen senden soll.
4. **Eigene Domain** für Löschseite und Datenschutzerklärung (P10)? Für Play reicht
   `*.run.app`; vor dem Wechsel des OAuth-Zustimmungsbildschirms in *Produktion*
   prüfen, ob Google eine bestätigte Domain verlangt. Vor dem Play-Store-Release.

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
| 2026-11-01 | B1: Test mit Go-Werkzeug `testanmeldung`; Kotlin-Test-Client ab B2 (ersetzt: seit B2 testet der Kotlin-Test-Client) |
| 2026-11-01 | Namen in Schnittstelle und Code deutsch, ohne Umlaute |
| 2026-11-01 | Abgefangene Anfragen dürfen sich im Zeitfenster wiederholen (Anmelden ca. 5 min, Erneuern ±2 min); keine Merkliste benutzter Signaturen. Schutz: TLS, kurze Fenster, Sitzung 1 h (Test `TestWiederholteAnfrageImZeitfensterGiltBewusst`) |
| 2026-11-01 | Geräte-IDs (pseudonym) dürfen in Logs stehen, zur Fehlersuche und Missbrauchsabwehr (Art. 6 Abs. 1 f). Einzeln löschen geht in Cloud Logging nicht; stattdessen: Konto löschen trennt sofort die Zuordnung (Firestore), Logs laufen nach 30 Tagen ab (P5). In die Datenschutzerklärung (P4); vor B6 von jemandem mit Datenschutzerfahrung prüfen lassen |
| 2026-11-01 | Anfrage-Logs von Cloud Run (mit IP-Adresse und User-Agent) bleiben 30 Tage in `logs-eu`, zur Missbrauchsabwehr (Art. 6 Abs. 1 f); „nicht dauerhaft“ im Sinne von B3. In die Datenschutzerklärung (P4) |
| 2026-11-01 | Keine Offline-Kopie des Pfeffers, solange es keine echten Nutzer gibt; Schutz gegen `tofu destroy` (`prevent_destroy`) und gegen das Zerstören einzelner Versionen (30 Tage wiederherstellbar), nicht gegen das Löschen des ganzen Secrets. Vor echtem Betrieb neu entscheiden (am 2026-11-04 entschieden) |
| 2026-11-01 | B2: Krypto mit Google Tink (JVM und Android); Personenschlüssel = HPKE/X25519 + Ed25519, Tresor- und Freigabe-Schlüssel XChaCha20-Poly1305 ([b2-entwurf.md](b2-entwurf.md)) |
| 2026-11-01 | B2: Wiederherstellungsschlüssel = 256 Bit Zufall, gedruckt als Base32 in Vierergruppen mit Prüfzeichen |
| 2026-11-01 | B2: Server speichert je Konto ein verschlüsseltes Schlüssel-Backup mit Version (kein Überschreiben eines neueren); Konto-ID als zugehörige Daten |
| 2026-11-01 | B2: Das erste Gerät ohne Backup erzeugt die Schlüssel (Version 0 verhindert doppelte); neues Gerät per QR erst in B3 |
| 2026-11-01 | B2: Kotlin-Code aus proto im Gradle-Build erzeugt (javalite, connect-kotlin), nicht eingecheckt; gemeinsame Testvektoren für Kotlin und Go |
| 2026-11-02 | B2: Wiederherstellungsschlüssel wird angezeigt und bestätigt, bevor etwas gespeichert oder hochgeladen wird; danach lokal ablegen (mit offenem Backup), dann hochladen. Scheitert das Hochladen, setzt der nächste Lauf fort (Review-Lauf 4) |
| 2026-11-02 | Abläufe, die auch die App braucht (z. B. Einrichtung), liegen in `protokoll` hinter Schnittstellen (Server, lokale Ablage); `testclient` enthält nur CLI, Connect-Anbindung und Dateiablage (Review A1) |
| 2026-11-02 | Tests prüfen Verhalten, nicht die geprüfte Konstante selbst (kein Test, der nur `VERSUCHE == 3` nachspricht) |
| 2026-11-02 | Gradle-Prüfsummen (`verification-metadata.xml`) noch nicht; bekannte Grenze, vor der App einführen (ersetzt am selben Tag: in B2b eingeführt) |
| 2026-11-02 | Review A2 (Mitgliedschaft: Server-Liste oder signiertes Log) im B3-Entwurf klären; A3 (Client-Formate maßgeblich beschreiben, feste Vektoren) wird in B2b vorgezogen |
| 2026-11-02 | Etappe B2b vor B3 eingeschoben: Das Backend kommt früh als verschlüsseltes Backup in die App (interne Variante), für echte Tests von B1/B2. Kein verlässliches Backup (Kill-Switch); das lokale Backup bleibt. Entwurf: [b2b-entwurf.md](b2b-entwurf.md) |
| 2026-11-02 | B2b: Daten-Backup ist eine undurchsichtige Nutzlast; die App legt ihre Room-Datenbank ab (gzip), ohne Fotos und Audio. Eigenes Datenformat erst in B3 |
| 2026-11-02 | B2b: Sicherung mit dem Tresorschlüssel verschlüsselt; Kopf im Chiffrat mit Zähler, Zeit, Geräte-ID, Schema-Version. Kleinerer Zähler vom Server → Warnung, kein stilles Wiederherstellen |
| 2026-11-02 | B2b: Ablage in Cloud Storage (`var.region`), ein Objekt je Konto, 3 ältere Fassungen, nach 30 Tagen gelöscht (am 2026-11-03 geändert: 7 Tage, höchstens 100, am 2026-11-04: 200); Schreiben nur mit erwarteter Version (`ifGenerationMatch`) |
| 2026-11-02 | B2b: Ohne Synchronisation ein sicherndes Gerät; ein anderes übernimmt nur nach Rückfrage. Höchstens 8 MiB je Sicherung, Übertragung über den Server, Löschen von Anfang an |
| 2026-11-02 | B2b: Formate maßgeblich in `docs/formate.md` mit festen Testvektoren (A3); Gradle-Prüfsummen; Modul `verbindung`; Einbindung per Composite Build |
| 2026-11-02 | B2b: Testen nur Kevin und wenige Bekannte im internen Test-Track, eigene App-Variante; Testerinformation statt Datenschutzerklärung bis B6 |
| 2026-11-02 | Testphase: Claude darf Wiederherstellungsschlüssel von Testkonten sehen. Vor echten Daten werden Schlüssel-Backup, Datensicherung und Testdateien gelöscht und neu erzeugt; ab dann (Sicherheitsphase, von Claude vorher angekündigt) sieht Claude keine Schlüssel mehr. Liste verbotener Geheimnisse in `CLAUDE.md` |
| 2026-11-03 | Gestohlenes Gerät mit Google-Login kann sich nach dem Sperren neu anmelden: bekannte Grenze bis vor echten Daten; dann dürfen neue Geräte erst nach Freischaltung (QR oder Wiederherstellungsschlüssel) sperren und löschen, vorhandene Geräte erfahren von neuen ([b1-entwurf.md](b1-entwurf.md)) |
| 2026-11-03 | Datensicherung: Sitzung wird auf HTTP-Ebene geprüft, bevor der Inhalt gelesen und ein Platz belegt wird (`sitzung.VorDemLesen`) |
| 2026-11-03 | Schlüssel-Backup: Version beginnt nach dem Löschen wieder bei 1; Clients speichern nur mit erwarteter Version 0, Grabstein erst bei Bedarf (`schluessel.proto`) |
| 2026-11-03 | Datensicherung: Wiederherstellen heißt übernehmen; abgelehnte Fassungen merkt sich das Gerät als gesehen (Untergrenze); Löschen behält den Zähler (`Sicherungsablauf.loeschen`) |
| 2026-11-03 | Datensicherung: ältere Fassungen 7 Tage, höchstens 100, statt 3 Fassungen und 30 Tage ([b2b-entwurf.md](b2b-entwurf.md), E-B2b-4) (am 2026-11-04 geändert: höchstens 200) |
| 2026-11-03 | Kill-Switch: WARN (Alarm-Mail) nur bei neu gesperrten Diensten oder Trennen; ist schon alles gesperrt, INFO |
| 2026-11-03 | Löschschutz (Firestore, Cloud Run, Bucket) wird vor echten Daten eingeschaltet ([b2b-entwurf.md](b2b-entwurf.md), „Vor echten Daten“) |
| 2026-11-03 | SE1 „Signaturen jeder Operation“ meint die Sync-Operationen ab B3; bis dahin signieren nur Anmelden und Erneuern |
| 2026-11-03 | SE3 Image-Scan: kein eigener Scanner. Das Server-Image ist `distroless/static` (keine Shell, kaum Pakete) mit einer Go-Datei; diese prüft `govulncheck` in der CI, das Basis-Image hält Dependabot aktuell. Ein Scanner in GCP kostet je Image, einer in der CI wäre eine weitere fremde Action |
| 2026-11-03 | Datensicherung: Bucket ohne Soft Delete – Löschen löscht sofort (DSGVO Art. 17); vor Fehlern der App schützen die älteren Fassungen |
| 2026-11-03 | Server: `GOMEMLIMIT=192MiB` bei 256 MiB; Parallelität bleibt 80, weil Sicherungen auf vier gleichzeitig begrenzt sind |
| 2026-11-03 | Kill-Switch: zweiter Alarm, wenn die Funktion 23,5 h nicht aufgerufen wurde (Absenz-Bedingung, ca. 0,10 $/Monat) |
| 2026-11-04 | Datensicherung: App sichert automatisch höchstens stündlich und nur bei Änderung, dazu manuell (höchstens einmal pro Minute); ältere Fassungen 7 Tage, höchstens 200 |
| 2026-11-04 | Kill-Switch: WARN (Alarm-Mail) auch, wenn ab 100 % kein Dienst gefunden wird – sonst schützt er unbemerkt nichts (z. B. falsche REGION) |
| 2026-11-04 | Pfeffer: Zu Beginn der Sicherheitsphase legt Kevin allein eine Offline-Kopie im Passwortmanager ab; sie schützt auch gegen das Löschen des ganzen Secrets. Ablauf zum Sichern und Wiederherstellen in [gcp-einrichtung.md](gcp-einrichtung.md); der Schutz in OpenTofu (`prevent_destroy`, 30 Tage für zerstörte Versionen) ist per `tofu test` abgesichert |
| 2026-11-05 | Kill-Switch sperrt ohne actAs: Er entfernt `allUsers` aus der IAM-Richtlinie der Dienste (statt Ingress „nur intern“); Rechte nur noch `run.services.list`, `getIamPolicy`, `setIamPolicy`. Ein `tofu apply` öffnet gesperrte Dienste nur mit `-var=gesperrte_dienste_oeffnen=true` |
| 2026-11-05 | Löschschutz vor echten Daten: Firestore mit Löschschutz und `ABANDON`, Cloud Run `deletion_protection`, Bucket der Datensicherungen ohne `force_destroy`. „Vollständig löschbar“ (Q7) bleibt über das Löschen des Projekts |
| 2026-11-05 | Datensicherung: höchstens 48 Sicherungen je Konto und Tag (UTC), danach `resource_exhausted` bis Mitternacht UTC; Holen und Löschen frei ([b2b-entwurf.md](b2b-entwurf.md), E-B2b-12) (am 2026-11-06 geändert: Holen höchstens 50, Löschen höchstens 20 je Tag) |
| 2026-11-05 | Neue Geräte erst nach Freischaltung (E-B2b-13): Ein weiteres Gerät darf nur lesen, sich abmelden, das erste Backup anlegen und sich freischalten; sperren, überschreiben und löschen erst, wenn ein freigeschaltetes Gerät zustimmt oder es mit einem aus dem Wiederherstellungsschlüssel abgeleiteten Ed25519-Schlüssel signiert ([b2b-entwurf.md](b2b-entwurf.md)) |
| 2026-11-06 | Nach Review-Lauf 8: Diagnosen nur von freigeschalteten Geräten (zusätzlich zu E-D-3) |
| 2026-11-06 | Nach Review-Lauf 8: Das erste Schlüssel-Backup nimmt der Server von einem nicht freigeschalteten Gerät nur an, wenn das Konto kein freigeschaltetes, ungesperrtes Gerät hat (in der Transaktion geprüft); sonst `permission_denied` ([b2b-entwurf.md](b2b-entwurf.md), E-B2b-13) |
| 2026-11-06 | Nach Review-Lauf 8: Datensicherung holen höchstens 50-mal, löschen höchstens 20-mal je Konto und Tag (UTC), sonst `resource_exhausted`. Dazu ein Egress-Alarm (Mail, wenn der Server in 23 Stunden mehr als 1 GiB sendet; ca. 0,40 $/Monat, `infra/alarm.tf`) |
| 2026-11-06 | Nach Review-Lauf 8: Gesperrt wirkt sofort über den Interceptor (er liest das Gerät bei jedem Aufruf); `GeraetSperren` setzt erst das Merkmal und löscht dann die Sitzungen in Stapeln (wiederholbar). Höchstens 200 Sitzungen je Gerät und Tag (UTC), Anmelden und Erneuern zusammen |
| 2026-11-06 | Nach Review-Lauf 8: höchstens 10 nicht freigeschaltete, ungesperrte Geräte je Konto; ein weiteres neues Gerät bekommt beim Anmelden `resource_exhausted` |
| 2026-11-07 | Nach Review-Lauf 8 (Frage 6): Lehnt der Mensch die Übernahme einer fremden Sicherung ab, merkt das Gerät sich das (`GemerkteSicherung.abgelehnt`). Die automatische Sicherung ruht dann, ohne den Server aufzurufen; wieder in Gang kommt sie, wenn der Mensch von Hand sichert (fragt erneut), wiederherstellt oder löscht. Ob sich die Sicherung auf dem Server geändert hat, ließe sich nur mit einem Aufruf prüfen, der selbst zum Tageslimit zählt |
| 2026-11-07 | Nach Review-Lauf 8 (Frage 7): Bis zu 2 Instanzen bleiben; die Grenze von 4 gleichzeitigen Sicherungen gilt je Instanz, zusammen also bis zu 8. Bekannte Grenze in stand.md |
| 2026-11-07 | Nach Review-Lauf 7 (Frage 1) bestätigt: Scheitert das Ablegen nach dem Zählen, ist der Versuch verbraucht (Diagnosen und Sicherungen); festgehalten durch `TestGescheitertesAblegenZaehltTrotzdem` |
| 2026-11-07 | Nach Review-Lauf 7 (Frage 3): Die Wiederherstellung des Pfeffers (gcp-einrichtung.md, „Secret gelöscht“) wird vor der Sicherheitsphase einmal durchgespielt; danach wird die Anleitung angepasst |
| 2026-11-07 | Ergebnis der Pfeffer-Probe: Beim Import der Secret-Version liest der Provider den Pfeffer in den Zustand, und der Plan will die Version ersetzen. Darum verwaltet OpenTofu nur noch das Secret, nicht den Wert (`removed`, angewendet am selben Tag); Version 1 entsteht und wird wiederhergestellt per gcloud, beim Wiederherstellen wird nur das Secret importiert. Der Server liest weiter fest Version 1 (tofu-Test) |
| 2026-11-07 | **Eigenbetrieb** bis zum ersten weiteren Nutzer: Echte Daten nur von Kevin; die App darf gegen die Cloud sichern, Claude darf Pfeffer, Wiederherstellungsschlüssel, Keysets, Geräteschlüssel und Sitzungen kennen (nur bei Bedarf). Nie: Passwörter, 2FA, Dienstkonto-Schlüssel, Google-ID-Tokens, Auth-Codes, OAuth-Client-Secret, Klartext-Inhalte, Daten anderer. Die Sicherheitsphase beginnt vor dem Teilen mit einer anderen Person, dem ersten Tester oder der Veröffentlichung; dann wird nach Checkliste (b2b-entwurf.md) alles zurückgesetzt und mit neuen Secrets eingerichtet. Ersetzt den Beginn „erste Einrichtung der App gegen die Cloud“ (2026-11-02) |
| 2026-11-07 | App-Einbindung (B2b Schritt 7): Entwurf im App-Repo (`../avlreminder/docs/backup-entwurf.md`, A-1 bis A-14) bestätigt. Im Eigenbetrieb **keine eigene App-Variante**: Das Backup kommt in den normalen Build hinter den Schalter (App-Anf. 7.28 PS8), damit Kevins echte Daten gesichert werden; Variante und interner Test-Track kommen mit dem ersten Tester (präzisiert E-B2b-11). Composite Build mit festem Commit dieses Repos, in der App-CI per Deploy-Key; Anmelden und Erneuern wandern aus dem Test-Client nach `verbindung`. Für die App braucht der Server die Web-Client-ID in `google_client_ids` |
| 2026-11-08 | Nonce der Google-Anmeldung mit 16 Zufallsbytes je Anmeldung (`AnmeldenRequest.nonce_zufall`, Nonce = SHA-256(Geräteschlüssel ‖ Zufall)); ohne oder mit anderer Länge lehnt der Server ab. Anlass: Mit der festen Nonce gab Google beim erneuten Anmelden desselben Geräts das zwischengespeicherte ID-Token zurück, das der Server als zu alt ablehnte (Test in der App). Die 5 Minuten Höchstalter bleiben. Alte Clients können sich nicht mehr anmelden; im Eigenbetrieb nur Kevins App und der Test-Client |
| 2026-11-08 | Wiederherstellungsschlüssel: Die App bietet automatisch an, ihn im Passwortdienst des Handys zu sichern (Credential Manager), und holt ihn beim Wiederherstellen von dort (App-Anf. 7.33). Präzisiert ID4: Papier oder Passwortdienst. Der Server erfährt den Schlüssel weiterhin nie |
| 2026-11-04 | Diagnose-Log aus der App ([diagnose-entwurf.md](diagnose-entwurf.md)): unverschlüsselt, ohne Inhalte (nur feste Ereignisse, typisierte Felder, Stapel ohne Meldungstext), nur auf Knopfdruck, nur Konten mit Diagnose-Erlaubnis, eigener Bucket in der EU, 14 Tage; Claude darf diese Logs lesen |
| 2026-11-09 | Nach Review-Lauf 9 (Frage 1): Beim Hochladen des Schlüssel-Backups prüft der Server **zuerst die erwartete Version**, dann „geschützt“. Nimmt er das erste Backup eines nicht freigeschalteten Geräts an (`nurUngeschuetzt`), schaltet er es **in derselben Transaktion frei** (wie E-B2b-13 es sagt). Lehnt er mit `GeraetNichtFreigeschaltet` ab, verwirft `Einrichtung` das offene Backup und sagt, dass der angezeigte Schlüssel nicht gilt. Die Testattrappe der App bildet diese Serverregel nach |
| 2026-11-09 | Nach Review-Lauf 9 (Frage 2): Lehnt der Mensch das Wiederherstellen einer **fremden** Fassung ab, ruht das automatische Sichern weiter (die Rückfrage gilt als „Dieses Gerät übernimmt die Sicherung?“), bis er von Hand sichert, wiederherstellt oder löscht. Bei der eigenen Fassung ruht es nicht (seit `c6aa2c5`) |
| 2026-11-09 | Nach Review-Lauf 9 (Frage 3): Der Worker kann die Rückfrage „übernehmen?“ nicht beantworten lassen; **unbeantwortet gilt wie „nein“** (E-B2b-6, App A-10) |
| 2026-11-09 | Nach Review-Lauf 9 (Frage 4): Zusätzlich zu den 10 offenen Geräten höchstens **10 neue Geräte je Konto und Tag** (`kontingent`, Art `neues_geraet`, sonst `resource_exhausted` beim Anmelden). Sonst lässt sich die Grenze durch Selbstsperren umgehen, und gesperrte Geräte sammeln sich unbegrenzt. Vor der Sicherheitsphase |
| 2026-11-09 | Nach Review-Lauf 10 (Frage 1): **„Cloud-Daten löschen“ sperrt alle Geräte des Kontos** (`GeraeteAuflisten`, dann `sperren` je Gerät, dieses zuletzt), als Ablauf in `protokoll`, den auch der Test-Client nutzt. Sonst legt ein zweites Gerät die Sicherung ohne Schlüssel-Backup neu an. App: R-3 in `backup-entwurf.md` |
| 2026-11-09 | Nach Review-Lauf 9 (R2–R4): Adapter mit festem Token (R2) bis B3 zurückgestellt; der Knopf „Mit Wiederherstellungsschlüssel freischalten“ (R3) kommt zusammen mit der Serverregel vom selben Tag (Frage 1); R4 (lokale Cloud-Schlüssel verwerfen) ist durch *Dieses Gerät abmelden* in der App erledigt und steht in der Checkliste „Vor dem ersten weiteren Nutzer“ |
| 2026-11-10 | R-4 (App-Repo `docs/wiederherstellen-entwurf.md`): `DatensicherungDienst` listet die Fassungen (`FassungenAuflisten`: Version, Zeitpunkt, Größe) und holt eine ältere über ihre Version (`SicherungHolen.version`, 0 = die neueste). Auflisten zählt mit Holen zusammen (50 je Tag). Im Client als eigene Schnittstelle `FassungsArchiv` neben `SicherungsAblage`, damit der Sicherungsablauf und die Fakes der App unverändert bleiben; die App nutzt sie später |
| 2026-11-10 | Phase C (Review-Lauf 10 R3, Lauf 8 E8): **W-7 im `Sicherungsablauf`**: Nach dem Wiederherstellen der Fassung eines anderen Geräts merkt sich das Gerät Version 0, die nächste Sicherung fragt vor dem Ersetzen (vorher setzte das nur die App). **Abgelehnte Sitzung**: `unauthenticated` vom Dienst bei Aufrufen mit Sitzung wird `SitzungAbgelehnt`; `Anmeldung.mitSitzung` erneuert dann einmal, und erst das Erneuern sagt „gesperrt“ oder „neu anmelden“. Ein `Fehlergrund` als Connect-Error-Detail (E8, mittelfristig) bleibt zurückgestellt: Jeder Code hat je Aufruf nur eine Bedeutung, die Adapter bilden ihn je Aufruf ab |
| 2026-11-10 | Phase D (Review-Lauf 8 E2, Lauf 9 R2/A2/A4, E10/E13): **Richtlinie je Prozedur** an einer Stelle im Server (`offen`, `nurSitzung`, `freigeschaltet`, `eigeneRegel`; unbekannte Prozeduren abgelehnt, Test über alle Prozeduren der Deskriptoren). **Adapter holen das Token vor jedem Aufruf** (R2, löst die Zurückstellung vom 2026-11-09 ab). Diagnose: „Diagnose-Erlaubnis“ statt „freigeschaltetes Konto“; Regel „Krypto nur im Client“ als SE4 geschärft. Vorgaben für B3 als Kap. 4.6 (A2, A4) |
| 2026-11-12 | Sicherheits- und Datenschutzprüfung vor der Sicherheitsphase (Konzept und Code von Server, Client, App und Infrastruktur): Nach der Rotation ist die Sicherung für eine Person sicher. Vor dem ersten weiteren Nutzer kommen dazu: **Konto löschen** (B1 in Kap. 3.5, vorgezogen aus B4), **Bestätigung mit der Displaysperre** für zerstörerische Aktionen (SE5), **Format v2** mit HKDF-Labels (SE6) und die Dokumente aus P3, P4, P6 und P7. **Aufbewahrungsfristen** (P5: 24 Monate ohne Anmeldung, gesperrte Geräte 90 Tage) spätestens zu B6. Liste in [b2b-entwurf.md](b2b-entwurf.md), „Vor dem ersten weiteren Nutzer“ |
| 2026-11-12 | Wiederherstellungsschlüssel: **Papier ist der Standard.** Der Speicher-Dialog des Passwortdienstes öffnet sich nicht mehr von selbst; der Knopf dafür nennt vorher die Folge: Ohne geräteinterne Verschlüsselung des Passwortmanagers kann, wer das Google-Konto übernimmt, ein eigenes Gerät freischalten und alles entschlüsseln (der heutige Hinweis kommt erst nach dem Speichern). Holen aus dem Passwortdienst beim Wiederherstellen bleibt. Ändert den Eintrag vom 2026-11-08 und in der App A-15 und 7.33 WS1 |
| 2026-11-12 | Diagnose: Bis es einen AV-Vertrag mit Anthropic gibt, hat **nur Kevins Konto** eine Diagnose-Erlaubnis (P9); Tester senden keine Diagnosen |
| 2026-11-12 | B3: Vorgaben **B3-5 bis B3-8** (Kap. 4.6): Lücken und aufgespaltene Verläufe erkennen, Person entfernen heißt neuer Freigabe-Schlüssel, öffentliche Schlüssel nur nach Vergleich des Fingerabdrucks, langlebige Personenschlüssel als bekannte Grenze bis B4 |
| 2026-11-12 | Metadaten: Sicherungen werden vorerst nicht auf feste Größen aufgefüllt; Größe und Zeitpunkt nennt die Datenschutzerklärung (Kap. 4.2) |
| 2026-11-12 | Konto löschen **ohne Zugang** (B1 in Kap. 3.5): Ein freigeschaltetes Gerät löscht sofort; mit Google-Login allein (nicht freigeschaltetes Gerät oder Webseite) nur ein **Antrag mit 7 Tagen Wartezeit**, den jedes freigeschaltete Gerät abbrechen kann und von dem vorhandene Geräte beim nächsten Aufruf erfahren. Ohne Google-Konto ist keine Zuordnung möglich (Art. 11 DSGVO); dann löscht die Frist aus P5. Keine Löschung per Mail |
| 2026-11-12 | Konto löschen, **Umsetzung** (Server und Client, `konto.proto`): `KontoLoeschen` setzt immer zuerst den Termin (sofort oder 7 Tage später) und löscht dann im selben Weg wie der regelmäßige Lauf: Zugang abschneiden, Datensicherung (alle Fassungen) und Diagnosen löschen, zuletzt Geräte, Schlüssel-Backup und Konto. Abbrechen geht nur vor dem Termin; ab dann gilt die Löschung als begonnen (`failed_precondition`), damit nie ein halb gelöschtes Konto ohne Termin bleibt. Der Lauf ist der Unterbefehl `aufraeumen` desselben Images (Cloud Run Job mit eigenem Dienstkonto, Cloud Scheduler). `loeschen_ab` steht in den Antworten von Anmelden, SitzungErneuern und GeraeteAuflisten. Die App verwirft ihre Schlüssel nur nach `geloescht = true` |
| 2026-11-13 | **CI nur auf `main`** (GitHub-Minuten, privates Repo): keine Läufe auf `develop` und bei PRs, reine Doku-Merges ohne Lauf; dafür lokal `werkzeuge/pruefen.sh` (vor jedem Push) und `pruefen.sh voll` (vor PR nach `main`, mit Race-Detector, govulncheck, buf breaking, tofu test, Image, actionlint), Go und OpenTofu in Linux-Containern. Kleine Jobs zusammengelegt, Image-Test nur noch im Deploy, Dependabot monatlich. Mit der Veröffentlichung als Open Source wieder auf `develop` und bei PRs prüfen |
| 2026-11-14 | Vor der Veröffentlichung des App-Repos (App-Anf. 7.34): **Zugang nur nach Freigabe** über eine Liste von Kennungen, leer heißt niemand; nicht Freigegebene bekommen ihre Kennung zum Weitergeben, der Server legt nichts an (ID7). **Kosten bei Angriffen** über Grenzen statt versteckte Adresse: signierte Sitzungs-Token ohne Firestore-Lesen bei Fälschungen, Alarm auf die Anfragezahl löst den Kill-Switch aus (ID8). Das Zurücksetzen der Sicherheitsphase behält Projekt und Adresse; die Adresse ist kein Geheimnis |
| 2026-11-14 | ID7/ID8 umgesetzt (Kevin): Entzug der Freigabe wirkt auch beim Erneuern der Sitzung; Kill-Switch sperrt ab mehr als 10.000 Anfragen je Stunde; Startliste App- und Test-Konto. Kürzerer Timeout und Grenze je IP bewusst nicht |
| 2026-11-14 | **Fristen aus P5 umgesetzt** ([datenschutz/README.md](datenschutz/README.md) K2): Das Konto speichert `zuletzt_aktiv` (nur der Tag), gesetzt beim Anmelden und Erneuern, höchstens ein Schreibvorgang je Konto und Tag. Der tägliche Lauf merkt Konten nach 24 vollen Monaten ohne Aktivität zur Löschung in 7 Tagen vor. Der Termin wirkt wie ein Antrag: Die Geräte sehen ihn, nur ein freigeschaltetes Gerät bricht ab, eine Anmeldung allein hebt ihn nicht auf (der Speicher unterscheidet die beiden nicht). Gesperrte Geräte ersetzt der Lauf nach 90 Tagen durch einen **Grabstein** (Kevin): Schlüssel und Zeitpunkte verschwinden, Geräte-ID und Konto bleiben in `geraete_grabsteine`, bis das Konto gelöscht wird. Sonst meldete sich ein gestohlenes Handy mit seinem alten Schlüssel als neues Gerät an. Konten aus der Zeit davor erfasst die Frist erst nach ihrer nächsten Anmeldung; Firestore wird zu Beginn der Sicherheitsphase ohnehin geleert |
| 2026-11-14 | Backend wird mit der App veröffentlicht (App-Anf. 7.34, Kevin): Lizenz **Apache-2.0**; Historie bleibt (nur Gmail- und GitHub-Adressen, keine Testdaten); Doku war schon ohne Benutzerpfade; CI bei PRs und GitHub-Einstellungen erst kurz davor |
| 2026-11-14 | **Konto löschen in der App** (K1, Kevin nach Vorschlag): ersetzt *Cloud-Daten löschen* (App R-3). Neu anfangen heißt Konto löschen und neu anmelden; die Kennung bleibt freigegeben. Ein nicht freigeschaltetes Gerät beantragt und behält seine Schlüssel; einen Termin, den der Server bei Anmeldung oder Erneuerung meldet, zeigt die App mit Benachrichtigung. Damit vorhandene Geräte innerhalb der 7 Tage davon erfahren, erneuert der stündliche Lauf die Sitzung spätestens nach 23 Stunden, auch ohne geänderte Daten; nur unter den Bedingungen der automatischen Sicherung (eingeschaltet, freigeschaltet, einmal von Hand gesichert) |
| 2026-11-15 | **SE5 in der App** (Kevin): *Konto löschen* und *Sicherung löschen* (löscht ebenfalls alle Fassungen endgültig) erst nach der Displaysperre, abgefragt vor jedem Kontakt zum Server; ohne Displaysperre geht es weiter und wird protokolliert, damit Löschen (Art. 17) möglich bleibt. Abbrechen, Fehlversuche und fehlender Sensor heißen nein. Eine Wartezeit im Server weiterhin erst mit mehreren Nutzern |
| 2026-11-15 | **Wiederherstellungsschlüssel: Passwortmanager als Hauptweg** (Kevin): Papier geht leicht verloren, weil man es fast nie braucht; einen Ende-zu-Ende-verschlüsselten Passwortmanager nutzt man regelmäßig. Die App bietet ihn als Hauptknopf an, aber nur nach dem Hinweis vom 2026-11-12 und mit dem Rat, den Passwortmanager selbst mit seinem Wiederherstellungscode auf Papier zu schützen (dort liegt das größte Risiko). Von selbst öffnet sich kein Speicher-Dialog; Papier bleibt der Weg ohne Passwortmanager. Die App kann nicht prüfen, ob der Passwortmanager Ende-zu-Ende verschlüsselt; das trägt der Hinweis. Ändert den Eintrag vom 2026-11-12; App V1.3c3, 2026-11-15, noch nicht installiert |
| 2026-11-15 | **Löschseite im Web** (P10, Kevin): Pflicht für den Play Store, vor dem Release. *Mit Google anmelden* stellt einen Löschantrag wie in der App (7 Tage Wartezeit ohne freigeschaltetes Gerät), weil der Server keine E-Mail-Adressen kennt. Hosting als eigener Cloud-Run-Dienst `web` in der EU-Region unter `*.run.app`, im kostenlosen Kontingent; eine günstige eigene Domain erst, wenn der OAuth-Zustimmungsbildschirm in *Produktion* geht und Google sie verlangt (Kap. 10, Frage 4). Ersetzt den Hinweis „GitHub Pages reicht“ in gcp-einrichtung.md (nicht nur EU) |
| 2026-11-15 | **Erster KI-Anwendungsfall: Schnellerinnerung aus Sprache** (K7, Kevin: „Ja alle Punkte passen, erst lokal, dann per Bestätigung mit Hinweis Cloud KI“): Spracherkennung und Zeitangabe zuerst lokal in der App, die Erinnerung entsteht sofort und auch ohne Netz. Die Cloud-KI wertet nur Sätze aus, die der lokale Parser nicht versteht, und nur nach Freigabe je Anfrage; K3 bleibt unverändert. Strukturierte Ausgabe statt mehrerer Versuche, Prüfung im Server und in der App, die KI rechnet keine Uhrzeiten. Teil von B5; App-Seite in App-Anf. 7.35 |
| 2026-11-19 | **Anfragealarm bleibt bei 10.000 Anfragen je Stunde** (Review Lauf 11, Frage 3; Kevin: „Alle Fragen nach Vorschlag umsetzen“): Jeder, auch ohne Konto, kann den Kill-Switch so mit Anfragen für weniger als einen Cent auslösen (Cloud Run: 0,40 $ je Million Anfragen) und alle Dienste der Region sperren. Das ist bewusst so: Kosten gehen vor Verfügbarkeit, und abgelehnte Anfragen kosten genauso. Die Richtlinie meldet sich alle 30 Minuten erneut beim Kill-Switch (`renotify_interval`), weil Eventarc eine verlorene Meldung nicht wiederholt. Der Pfad Alarm → Pub/Sub → Kill-Switch wird einmal in der Cloud getestet (eigene Richtlinie mit niedriger Schwelle auf einen Testdienst). Die Schwelle prüfen vor dem ersten weiteren Nutzer ([b2b-entwurf.md](b2b-entwurf.md)) |
| 2026-11-19 | **R-4 „ältere Fassung einspielen“ in `protokoll`** (Review Lauf 11, Empfehlung; Kevin: „Ja, R-4“): `Sicherungsablauf.fassungEinspielen(archiv, version, …)` entschlüsselt, prüft das Schema, fragt nach und spielt ein. Danach merkt sich das Gerät `max(Untergrenze, Zähler der Fassung)` und Version 0. Die Version muss größer als 0 sein; „die neueste“ bleibt `wiederherstellen`. | Der Zähler darf nicht sinken (E-B2b-3), sonst nähme der Server eine ältere Sicherung als neueste an. Version 0 statt der Version der Fassung: Mit ihr scheiterte jede weitere Sicherung an der Versionsprüfung; mit 0 ersetzt die nächste die neueste Fassung, eine fremde erst nach Rückfrage (W-7). Ablehnen merkt sich nichts, Einspielen beendet ein Ruhen wie Wiederherstellen. |
