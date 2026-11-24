# Technische und organisatorische Maßnahmen (Art. 32 DSGVO)

Entwurf, Stand 2026-11-14. Gegliedert nach Art. 32 Abs. 1. **Offen** markiert, was vor
dem ersten weiteren Nutzer noch fehlt (b2b-entwurf.md, „Vor dem ersten weiteren
Nutzer“). Einzelheiten stehen in den verlinkten Entwürfen und im Code (`// KRITISCH:`).

## Pseudonymisierung und Verschlüsselung (lit. a)
- **Ende-zu-Ende-Verschlüsselung:** Datensicherung und Schlüssel-Backup werden auf dem
  Gerät verschlüsselt (Google Tink, XChaCha20-Poly1305; [b2-entwurf.md](../b2-entwurf.md),
  [formate.md](../formate.md)). Der Server und der Betreiber sehen nur Chiffrate.
- **Pseudonyme statt Identität:** Gespeichert wird nie die Google-Konto-ID, sondern
  ihre Kennung (HMAC-SHA256 mit geheimem Pfeffer im Secret Manager). Konto-IDs sind
  Zufallswerte. Keine E-Mail-Adresse, kein Name.
- **Transport:** nur HTTPS (Cloud Run). Google verschlüsselt alle Daten im Ruhezustand.
- **Datenminimierung:** Konto mit Erstellungstag und Tag der letzten Aktivität statt
  Zeitpunkten; nach 90 Tagen Sperre bleibt vom Gerät nur ein Grabstein; Logs ohne Inhalte;
  Diagnose-Logger nimmt keine freien Zeichenketten an.
- **Ein Schlüssel, ein Zweck:** Backup-Schlüssel und Freischaltschlüssel sind per HKDF
  mit eigenem Label aus dem Wiederherstellungsschlüssel abgeleitet (Format v2, SE6,
  seit 2026-11-23).

## Vertraulichkeit (lit. b)
- **Zugang zum Dienst:** nur Google-Konten auf der Freigabeliste (ID7); neue Geräte
  dürfen erst nach Freischaltung durch ein vorhandenes Gerät oder den
  Wiederherstellungsschlüssel sperren, überschreiben oder löschen (E-B2b-13).
  Sitzungs-Token signiert, 1 Stunde gültig; gesperrte Geräte wirken sofort.
- **Minimale Rechte in GCP:** jeder Dienst mit eigenem Dienstkonto; der Server darf
  Diagnosen nur anlegen und löschen, nicht lesen; der Lauf `aufraeumen` darf
  Objekte nur auflisten und löschen. Kein Konto hat die Editor-Rolle. Buckets ohne
  öffentlichen Zugriff (`public_access_prevention = enforced`).
- **Keine Schlüsseldateien:** Deploy aus GitHub über Workload Identity Federation, nur
  von `main` und nur nach grüner CI.
- **Betreiberzugang:** Nur Kevin hat Rollen im Projekt. Er kann Metadaten und
  Chiffrate sehen, aber nichts entschlüsseln. Admin-Aktionen stehen im Audit-Log.
- **GitHub-Konto** (ein übernommenes Konto könnte über `main` deployen): 2FA mit
  Passkey für die Anmeldung (seit 2026-11-14), Authenticator-App und GitHub Mobile
  als Reserve.
- **Offen:** Zurücksetzen aller Schlüssel und des Pfeffers zu Beginn der
  Sicherheitsphase (Werte aus dem Eigenbetrieb gelten als bekannt).

## Integrität (lit. b)
- Anmelden und Erneuern sind mit dem Geräteschlüssel signiert (Ed25519); Chiffrate
  sind authentisiert (AEAD), Zähler im Kopf verhindern das unbemerkte Einspielen
  älterer Sicherungen.
- Schreiben nur mit erwarteter Version (`ifGenerationMatch`, Versionen im
  Schlüssel-Backup).
- Code: Tests als Spezifikation (zu jeder Erlaubnis ein Ablehnungsfall), `go vet`,
  `golangci-lint`, Race-Detector, `govulncheck`, `buf breaking`, Gradle-Prüfsummen,
  Dependabot; Server-Image `distroless/static`. Regelmäßige Reviews (`.review/`).

## Verfügbarkeit und Belastbarkeit (lit. b)
- Die App ist lokal-first: Fällt das Backend aus, funktioniert sie weiter, und die
  Daten bleiben auf dem Gerät. **Kosten gehen vor Verfügbarkeit:** Der Kill-Switch
  sperrt den Dienst bei 100 % Budget oder Anfrageflut und trennt ab 150 % das
  Rechnungskonto.
- Grenzen: höchstens 2 Instanzen, Timeout, Größenlimits, Tageszähler je Konto,
  Egress- und Anfragealarm.
- Löschschutz für Firestore, Dienst und Bucket gegen `tofu destroy`.

## Wiederherstellbarkeit (lit. c)
- Ältere Fassungen der Datensicherung 7 Tage; Wiederherstellungsschlüssel im
  Ende-zu-Ende-verschlüsselten Passwortmanager (Hauptweg) oder auf Papier für neue Geräte; lokales Backup der App unabhängig vom Server.
- Infrastruktur vollständig in OpenTofu, Zustand versioniert.
- **Offen:** Offline-Kopie des neuen Pfeffers (Sicherheitsphase); Wiederherstellung des
  Pfeffers einmal durchspielen (Kap. 11, 2026-11-07).

## Löschung
- Konto löschen mit vollständigem, wiederholbarem Löschweg und täglichem Lauf;
  Buckets ohne Soft Delete; Lebenszyklusregeln (Fassungen 7 Tage, Diagnosen 14 Tage);
  Logs 30 Tage; TTL für Sitzungen und Zähler.
- Fristen aus P5 im täglichen Lauf: Konten nach 24 Monaten ohne Anmeldung oder
  Erneuerung (Termin in 7 Tagen, dann der Löschweg), gesperrte Geräte nach 90 Tagen
  (Grabstein). Tests im Vertragstest des Speichers und in `dienst/aufraeumen_test.go`.
- Datensicherungen und Schlüssel-Backups ohne Konto (eine Anfrage, die kurz vor dem
  Löschen angenommen war) löscht der tägliche Lauf, sobald sie älter als eine Stunde
  sind (seit 2026-11-19).
- Konto löschen in der App: umgesetzt in V1.3c2 (App-Branch `feature/konto-loeschen`, 2026-11-14), noch nicht installiert ([README.md](README.md), K1).

## Überprüfung und Organisation (lit. d)
- CI auf `develop`, `main` und bei PRs, nach `main` nur mit grüner CI; lokales
  Prüfskript wie die CI (`werkzeuge/pruefen.sh`), Tests in der
  Cloud nach Änderungen an Löschen, Limits und Kill-Switch (`stand.md`).
- Review-Läufe mit Gedächtnis (`.review/state.md`); Sicherheits- und
  Datenschutzprüfung 2026-11-12.
- Regeln für KI-Werkzeuge: Claude liest keine Geheimnisse und keine Daten anderer
  Nutzer (`CLAUDE.md`, ab der Sicherheitsphase streng).
- Ablauf bei Datenpannen: [datenpannen.md](datenpannen.md).
- *Konto löschen* und *Sicherung löschen* erst nach der Displaysperre (SE5, umgesetzt in der App V1.3c2 (2026-11-15, noch nicht installiert)).

## Bekannte Grenzen
- **Metadaten:** Größe und Zeitpunkt der Sicherungen zeigen, wie viel jemand die App
  nutzt (Kap. 4.2); IP-Adressen 30 Tage.
- **Langlebige Personenschlüssel** (B3-8): Wer einen erbeutet, liest bis zur Rotation
  (B4) mit. Betrifft erst B3.
- **Passwortdienst:** Liegt der Wiederherstellungsschlüssel ohne geräteinterne
  Verschlüsselung im Passwortmanager, kann ein Angreifer mit dem Google-Konto alles
  entschlüsseln. Deshalb sagt die App das vor dem Speichern und rät, den Passwortmanager
  selbst mit seinem Wiederherstellungscode auf Papier zu schützen (A-15, App V1.3c3, 2026-11-15, noch nicht installiert).
- **Admin-Audit-Log** liegt global (`_Required`), enthält aber nur Betreiberaktionen.
