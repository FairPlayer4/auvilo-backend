# GCP-Einrichtung – was Kevin vorab tun muss

> Stand: 2026-11-01. Ziel: alles vorbereitet, damit wir Etappe B0 (Projekt, Budget,
> Kill-Switch) und B1 (erster Server auf Cloud Run) deployen können.
> Punkte mit **[Kevin]** gehen nur mit deinem Google-Konto im Browser; alles andere
> erledigt Claude per `gcloud`/OpenTofu, sobald du angemeldet bist und Go gibst.

## Begriffe in 1 Minute

| Begriff | Bedeutung |
|---|---|
| **Rechnungskonto** (Billing Account) | Hier hängt das Guthaben bzw. die Zahlungsart. Ein Rechnungskonto kann mehrere Projekte bezahlen. |
| **Projekt** | Container für alle Ressourcen (Cloud Run, Firestore …). Hat eine weltweit eindeutige ID. Projekt löschen = alles weg (gut für ein Lernprojekt). |
| **API aktivieren** | Jeder Dienst muss pro Projekt eingeschaltet werden, z. B. `run.googleapis.com`. |
| **IAM** | Wer darf was. Du bist *Owner*. Programme laufen als **Dienstkonto** (Service Account) mit möglichst wenig Rechten. |
| **Organisation** | Optionale Ebene über Projekten, braucht eine eigene Domain. Brauchen wir vorerst nicht. |
| **Budget** | Sendet nur Warnungen – stoppt **nichts**. Deshalb bauen wir den Kill-Switch selbst. |

## Schritt 1 – Konto absichern [Kevin]
- Am Google-Konto, mit dem du GCP nutzt, **Bestätigung in zwei Schritten** einschalten
  (myaccount.google.com → Sicherheit). Wer dieses Konto hat, kann Kosten verursachen.

## Schritt 2 – Rechnungskonto prüfen [Kevin]
- console.cloud.google.com → *Abrechnung*. Dort sollte ein Rechnungskonto mit deinem
  Test-Guthaben stehen.
- Vermerke, **bis wann das Guthaben gilt** (meist 90 Tage). Solange du nicht auf ein
  *volles Konto* umstellst, wird nach Ablauf nichts berechnet, aber die Dienste
  stoppen. Für dieses Projekt reicht das vorerst. [P]
- Nichts weiter anlegen – Projekt, Budget usw. macht Claude reproduzierbar mit
  OpenTofu.

## Schritt 3 – gcloud anmelden [Kevin] ✅ erledigt
Die Werkzeuge sind installiert (siehe [entwicklungsumgebung.md](entwicklungsumgebung.md)).
In einem neuen Terminal nacheinander ausführen; jeweils öffnet sich der Browser:

```bash
gcloud auth login
```

```bash
gcloud auth application-default login
```

Der erste Befehl ist für `gcloud` selbst, der zweite für Programme wie OpenTofu und
lokale Go-Tests gegen GCP. Danach Claude Bescheid geben.

## Schritt 4 – Go für B0 geben [Kevin] ✅ erledigt am 2026-11-01
Claude zeigt dir vorher genau, was angelegt wird, und legt dann an:
1. Projekt (ID z. B. `avlreminder-exp-<zufall>`), mit deinem Rechnungskonto verknüpft
2. Bucket in der EU für den OpenTofu-Zustand
3. Budget 20 €/Monat mit Warnungen und Kill-Switch (Pub/Sub + kleine Funktion)
4. APIs: Cloud Run, Artifact Registry, Firestore, Secret Manager, Pub/Sub,
   Cloud Billing Budget, IAM Credentials
5. Firestore-Datenbank in `europe-west1`
6. Workload Identity Federation für GitHub Actions (Deploy ohne Schlüsseldatei)

Alles davon kostet im Leerlauf 0 € bzw. Cent-Beträge.

**Bootstrap** (einmalig per gcloud, vor dem ersten `tofu init`; so wurde B0 angelegt):

```bash
gcloud projects create avlreminder-exp-1ef2 --name=avlreminder-exp
```

```bash
gcloud billing projects link avlreminder-exp-1ef2 --billing-account=<RECHNUNGSKONTO>
```

```bash
gcloud services enable storage.googleapis.com serviceusage.googleapis.com cloudresourcemanager.googleapis.com cloudbilling.googleapis.com billingbudgets.googleapis.com --project avlreminder-exp-1ef2
```

```bash
gcloud storage buckets create gs://avlreminder-exp-1ef2-tfstate --location=europe-west1 --uniform-bucket-level-access --public-access-prevention --project avlreminder-exp-1ef2
```

```bash
gcloud storage buckets update gs://avlreminder-exp-1ef2-tfstate --versioning
```

```bash
gcloud auth application-default set-quota-project avlreminder-exp-1ef2
```

Ergebnis: [stand.md](stand.md).

## Später – Schritte im Browser je Etappe

### B1: Google-Login einrichten [Kevin, mit Anleitung] ✅ erledigt am 2026-11-01
Client-ID des Test-Clients (Desktop-App, nicht geheim):
`82416816442-vka4ve2no02681umqkn8b0rk37ri37qd.apps.googleusercontent.com`

Zustimmungsbildschirm und OAuth-Client lassen sich nicht per API/OpenTofu anlegen.
Oben links muss das Projekt **avlreminder-exp** ausgewählt sein.

1. console.cloud.google.com/auth/overview → *Google Auth Platform* → **Jetzt starten**.
   Der Assistent fragt:
   - App-Informationen: Name **avlreminder (Test)**, Support-E-Mail: deine Adresse
   - Zielgruppe: **Extern** (ohne Organisation die einzige Wahl)
   - Kontaktdaten: deine Adresse
   - Richtlinie zu Nutzerdaten akzeptieren → **Erstellen**
2. *Zielgruppe*: Status bleibt **Testen** (nicht veröffentlichen). Unter
   **Testnutzer** dein Google-Konto eintragen, den Haushalt später. Nur Testnutzer
   können sich anmelden – gleich ein Missbrauchsschutz.
3. *Datenzugriff*: **keine** Bereiche hinzufügen. `openid` ist immer dabei und reicht:
   Das ID-Token enthält dann nur die Konto-ID (`sub`), keine E-Mail und keinen Namen
   (Anforderung ID2). Ohne sensible Bereiche ist auch keine Prüfung durch Google nötig.
4. *Clients* → **Client erstellen** → Anwendungstyp **Desktop-App**, Name
   **Test-Client** → Erstellen. Im Dialog **JSON herunterladen**.
   - Die JSON-Datei **nicht** ins Repo legen, sondern in den Ordner
     `%APPDATA%\avlreminder\` (Name so lassen, wie Google ihn vergibt:
     `client_secret_….json`).
   - Claude nur die **Client-ID** nennen (`….apps.googleusercontent.com`); sie ist
     nicht geheim und kommt in die Server-Konfiguration. Das Secret einer Desktop-App
     gilt laut Google nicht als geheim, bleibt aber trotzdem lokal.

Hinweise:
- Im Status *Testen* zeigt Google beim Anmelden „Google hat diese App nicht
  überprüft“ – für Testnutzer normal, weiter über *Fortfahren*.
- Für die Android-App kommen später ein Web- und ein Android-Client dazu (Etappe
  mit App-Anbindung); der Server akzeptiert dann mehrere Client-IDs.

### Vor echten Daten: Pfeffer offline sichern [Kevin, allein]
Mit dem Pfeffer bildet der Server aus der Google-Konto-ID die Konto-ID. Ohne ihn
findet kein Gerät sein Konto, sein Schlüssel-Backup und seine Datensicherung wieder.
OpenTofu schützt ihn vor `tofu destroy`; eine zerstörte Version bleibt 30 Tage
wiederherstellbar. **Nicht** geschützt ist das Löschen des ganzen Secrets (Konsole,
gcloud): Das geht sofort und endgültig. Dagegen hilft nur diese Kopie.

Claude sieht den Pfeffer nie. Darum in einem **eigenen** Terminal ausführen, nicht im
Terminal-Bereich der Claude-App, und den Wert nicht anzeigen lassen.

**Sichern** (einmal, zu Beginn der Sicherheitsphase; Git Bash):
```bash
gcloud secrets versions access 1 --secret=pfeffer --project=avlreminder-exp-1ef2 | clip
```
Vorher prüfen, dass der Zwischenablageverlauf von Windows (Win+V) und seine
Synchronisierung aus sind; sonst bleibt der Wert dort liegen. In den
Passwortmanager einfügen (Eintrag „avlreminder Pfeffer“), danach die
Zwischenablage leeren (z. B. ein Leerzeichen kopieren; bei eingeschaltetem
Verlauf im Win+V-Menü „Alle löschen“). Der Wert besteht aus 64 Buchstaben und
Ziffern.

**Kopie prüfen** (Wert erneut aus dem Passwortmanager kopieren): Beide Zeilen
müssen gleich sein. Der Hash ist unbedenklich, er verrät den Pfeffer nicht.
```bash
gcloud secrets versions access 1 --secret=pfeffer --project=avlreminder-exp-1ef2 | sha256sum
powershell -NoProfile -Command Get-Clipboard | tr -d '\r\n' | sha256sum
```
Danach die Zwischenablage wieder leeren.

**Version zerstört** (innerhalb von 30 Tagen; die Version steht auf „deaktiviert“):
```bash
gcloud secrets versions enable 1 --secret=pfeffer --project=avlreminder-exp-1ef2
```

**Secret gelöscht** – Achtung: vorher **kein** `tofu apply`, das legte einen neuen,
zufälligen Pfeffer an.
1. Secret neu anlegen und den Wert aus der Zwischenablage hochladen (Git Bash,
   Wert vorher aus dem Passwortmanager kopieren):
   ```bash
   gcloud secrets create pfeffer --project=avlreminder-exp-1ef2 --replication-policy=user-managed --locations=europe-west1 --version-destroy-ttl=2592000s
   powershell -NoProfile -Command Get-Clipboard | tr -d '\r\n' | gcloud secrets versions add pfeffer --project=avlreminder-exp-1ef2 --data-file=-
   ```
2. OpenTofu den neuen Stand übernehmen lassen (im Ordner `infra/`):
   ```bash
   tofu state rm google_secret_manager_secret_version.pfeffer google_secret_manager_secret.pfeffer
   tofu import google_secret_manager_secret.pfeffer projects/avlreminder-exp-1ef2/secrets/pfeffer
   tofu import google_secret_manager_secret_version.pfeffer projects/avlreminder-exp-1ef2/secrets/pfeffer/versions/1
   ```
   Danach darf `tofu plan` keine neue Version des Pfeffers zeigen; die
   Zugriffsrechte des Servers legt `tofu apply` wieder an.
3. Prüfen: Ein vorhandenes Gerät meldet sich an und findet seine Datensicherung.
   Findet es keine, ist der Wert falsch (z. B. mit Zeilenumbruch hochgeladen).

### B4: Firebase für Push [Kevin, mit Anleitung]
- console.firebase.google.com → *Projekt hinzufügen* → **bestehendes GCP-Projekt**
  wählen. Google Analytics **ausschalten**.

### B6: Play Store
- Google-Play-Konsole: einmalig 25 US-$ Entwicklergebühr; Identitätsprüfung.
- Datenschutzerklärung und Löschseite brauchen eine öffentliche URL. **Eine eigene
  Domain ist nicht nötig** – GitHub Pages reicht. Eine Domain (ca. 10–15 €/Jahr)
  lohnt sich erst, wenn du eine GCP-Organisation willst.

## Warum keine Domain/Organisation jetzt
Eine Organisation würde u. a. erlauben, Regionen außerhalb der EU technisch zu
verbieten. Für ein Projekt mit einer Person ersetzen wir das durch eine zentral
gesetzte Region in OpenTofu und einen CI-Test, der den Plan prüft. Nachrüsten geht
jederzeit, das Projekt lässt sich dann in die Organisation verschieben.
