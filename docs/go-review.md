# Go-Code reviewen – Anleitung für Kevin

> Für jemanden mit C#-Hintergrund, der Go grob kennt. Ziel: mit wenig Zeit die
> wichtigen Stellen sicher prüfen – vor allem **Tests** und **kritischen Code**.

## 1. Reihenfolge beim Review

1. **Tests zuerst** (`*_test.go`). Sie sind die Spezifikation. Frage dich:
   - Beschreiben die Testnamen das gewünschte Verhalten vollständig?
   - Gibt es zu jedem „darf“ einen Test für „darf nicht“ (fremdes Konto, kein
     Mitglied, abgelaufenes Token, zu große Nachricht)?
   - Fehlt ein Fall, den du aus den Anforderungen kennst? → Das ist der wertvollste
     Review-Kommentar.
2. **Kritischer Code** – markiert mit `// KRITISCH:`: Anmeldung, Mitgliedschaft,
   Signaturprüfung, Löschen, Kosten-Grenzen. Hier jede Zeile lesen.
3. **Schnittstelle** (`proto/*.proto`): Felder, Namen, was der Server sieht.
   Jedes Feld, das kein Chiffrat ist, ist Metadaten → bewusst?
4. Der Rest: überfliegen, Struktur und Namen.

## 2. Go für C#-Entwickler – was anders ist

| Go | C#-Entsprechung / Unterschied | Worauf achten |
|---|---|---|
| `val, err := f()` und `if err != nil { return … }` | Statt Exceptions sind Fehler normale Rückgabewerte | **Kein Fehler darf verschluckt werden.** `_ = f()` oder fehlendes `if err` ist verdächtig. |
| `fmt.Errorf("lade Freigabe: %w", err)` | wie `InnerException` | Fehler werden mit Kontext weitergereicht; an der API-Grenze nie interne Details an den Client geben. |
| `defer x.Close()` | wie `using`/`finally` | Läuft beim Verlassen der Funktion. In Schleifen kann `defer` sich stauen. |
| `ctx context.Context` als erster Parameter | wie `CancellationToken` | Muss bis zur Datenbank durchgereicht werden, sonst laufen abgebrochene Anfragen weiter. |
| Großbuchstabe am Anfang = öffentlich | `public`/`internal` | Paket `internal/…` ist von außen nicht importierbar. |
| Interfaces werden **implizit** erfüllt | kein `: IFoo` | Kleine Interfaces beim Verwender definieren (z. B. `Store`), damit Tests eine Attrappe einsetzen können. |
| Nullwerte: `""`, `0`, `nil`, leere Struct | kein `null` für Werttypen | Ist ein leerer Wert gültig, oder fehlt eine Prüfung? |
| `go f()` startet eine Goroutine | wie `Task.Run`, aber sehr billig | Jede Goroutine braucht ein Ende (Context) und Schutz gemeinsamer Daten (`sync.Mutex`). Tests mit `-race` finden Fehler. |
| Pointer `*T` vs. Wert `T` | Referenz- vs. Werttyp | Methoden, die etwas ändern, brauchen `*T`. |
| Keine Vererbung, keine Generics-Hierarchien | Komposition | Viel „langweiliger“, direkter Code ist in Go Absicht, kein Mangel. |

## 3. Checkliste für kritischen Code

- [ ] Wird **zuerst** geprüft, wer anfragt (Token gültig), **dann**, ob er darf
      (Mitglied dieser Freigabe), **dann** erst gelesen/geschrieben?
- [ ] Kommt die Konto-ID **aus dem Token**, nie aus dem Request-Body?
- [ ] Werden Größen begrenzt (Nachrichtengröße, Anzahl pro Anfrage)?
- [ ] Wird jeder Fehler behandelt, und landet **kein Inhalt** (Chiffrat, Schlüssel,
      Token, KI-Text) im Log?
- [ ] Löschen: Werden **alle** Stellen erfasst (Konto, Geräte, Mitgliedschaften,
      Operationen, FCM-Token)? Gibt es einen Test, der danach nichts mehr findet?
- [ ] Zeit und Zufall kommen aus einer Quelle, die Tests steuern können (keine
      verstreuten `time.Now()`).
- [ ] Krypto nur aus Standardbibliothek/Tink, keine selbst gebauten Verfahren;
      Vergleiche von Geheimnissen mit `subtle.ConstantTimeCompare`.

## 4. Werkzeuge – laufen in der CI, lokal genauso

```bash
go test -race -cover ./...
```

```bash
golangci-lint run
```

```bash
govulncheck ./...
```

```bash
buf lint && buf breaking --against '.git#branch=main'
```

- `-race` findet gleichzeitige Zugriffe ohne Schutz.
- `-cover` zeigt die Abdeckung; für kritische Pakete erwarten wir nahezu 100 %.
- `go test -run 'TestName/Teilname' -v ./pfad/...` führt einen einzelnen Fall aus.

## 5. Tests lesen

Go-Tests sind meist **tabellengetrieben**:

```go
tests := []struct {
	name string // beschreibt das Verhalten
	req  Request
	want error  // erwartetes Ergebnis
}{
	{"Mitglied darf anhängen", reqVonMitglied, nil},
	{"lehnt Schreiben ohne Mitgliedschaft ab", reqVonFremdem, ErrKeinZugriff},
}
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) { … })
}
```

Die Tabelle ist das, was du prüfst: Zeile für Zeile ein Fall. Ein fehlender Fall ist
ein fehlender Tabelleneintrag – leicht zu entdecken und leicht zu ergänzen.

## 6. Für die Arbeit: Go statt Node.js auf GCP

- Ein statisches Binary, Container-Image oft < 20 MB, Kaltstart meist deutlich unter
  einer Sekunde, Speicherbedarf häufig ein Bruchteil von Node.js.
- Nebenläufigkeit ohne Callback-/Promise-Ketten; eine Cloud-Run-Instanz kann viele
  Anfragen gleichzeitig bedienen (`concurrency`).
- Die Werkzeuge (Format, Test, Race-Detector, Vuln-Check) gehören zum Standard –
  wenig Konfigurationsstreit im Team.
- Lernkurve gering; die größte Umstellung ist die explizite Fehlerbehandlung.
