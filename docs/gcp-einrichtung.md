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

Alles davon kostet im Leerlauf 0 € bzw. Cent-Beträge. Ergebnis: [stand.md](stand.md).

## Später – Schritte im Browser je Etappe

### B1: Google-Login einrichten [Kevin, mit Anleitung]
Die Seite für den Zustimmungsbildschirm lässt sich nicht per API anlegen:
1. Console → *Google Auth Platform* → *Branding*: App-Name „avlreminder (Test)“,
   deine E-Mail als Support-Adresse.
2. *Zielgruppe*: **Extern**, Status **Testen**. Dich (und später den Haushalt) als
   **Testnutzer** eintragen. Nur Testnutzer können sich anmelden – gleich ein
   Missbrauchsschutz.
3. *Datenzugriff*: nur `openid` (keine E-Mail, kein Profil).
4. *Clients* → *Client erstellen* → Typ **Desktop-App** (für den Test-Client).
   Client-ID an Claude geben; das Client-Secret einer Desktop-App gilt nicht als
   geheim, kommt aber trotzdem nicht ins Repo.

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
