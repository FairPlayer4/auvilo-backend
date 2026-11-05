# Entwurf Etappe B1: Server, Google-Login, Sitzungen, Geräte

> Stand: 2026-11-01. Entscheidungen von Kevin **bestätigt** am 2026-11-01 (auch in
> [anforderungen.md](anforderungen.md), Kap. 11). Umgesetzt und abgeschlossen
> (siehe [stand.md](stand.md)). Maßgeblich für Bytes, Kodierung und Fehlercodes
> sind die Kommentare in `proto/avlreminder/v1/`.

## Ziel
Am Ende von B1 kann sich ein Gerät mit einem Google-Konto anmelden, bekommt ein
kurzlebiges Sitzungs-Token, verlängert es ohne erneuten Google-Login und kann andere
eigene Geräte sperren. Der Server läuft auf Cloud Run, wird aus GitHub Actions
deployt und vom Kill-Switch erfasst. Noch **keine** Inhalte, Freigaben oder
E2E-Schlüssel (B2/B3).

## Entscheidungen (am 2026-11-01 bestätigt)

| # | Frage | Empfehlung | Warum |
|---|---|---|---|
| E-B1-1 | Wie weist sich ein Gerät nach dem ersten Login aus? | Mit seinem **Geräteschlüssel**: Es signiert eine Anfrage, der Server gibt ein neues Sitzungs-Token. Kein langlebiges Refresh-Token. | Auf dem Server liegt nur der öffentliche Schlüssel – nichts, was ein Angreifer aus der Datenbank nutzen könnte. Der private Schlüssel verlässt das Gerät nie (Android Keystore). |
| E-B1-2 | Verfahren für den Geräteschlüssel | **ECDSA P-256** statt Ed25519 | Der Geräteschlüssel ist der einzige Schlüssel, der fest im Android Keystore liegt (Personen- und Freigabeschlüssel müssen zwischen Geräten wandern und laufen über Tink, wie in Kap. 4.1). P-256 wird dort überall in Hardware unterstützt, auch im separaten Sicherheitschip (StrongBox); Ed25519 hängt von der Keystore-Version des Herstellers ab (KeyMint ≥ 2), nicht nur von der Android-Version, und StrongBox kann es nicht. Sicherheitsniveau gleich (~128 Bit). |
| E-B1-3 | Form des Sitzungs-Tokens | **Zufälliger Wert**, auf dem Server nur als Hash gespeichert, gültig **1 Stunde**. Kein JWT. | Gesperrte Geräte sind **sofort** draußen (Anforderung F6). Kostet einen Firestore-Lesezugriff je Anfrage – weit im Gratis-Kontingent (50.000/Tag). Kein Signierschlüssel, den man verwalten oder rotieren müsste. |
| E-B1-4 | Wie wird die Google-Konto-ID gespeichert? | Als **HMAC mit geheimem Pfeffer** (Secret Manager), nicht im Klartext | Gelangt die Datenbank nach außen, lässt sie sich nicht mit Google-Konten verknüpfen. Kostet nichts (Gratis-Kontingent Secret Manager). Geht der Pfeffer verloren, muss man sich neu registrieren. (Ergänzung 2026-11-03: Seit B2 hängen Schlüssel-Backup und Datensicherung an der Konto-ID; ohne Pfeffer sind sie nicht mehr auffindbar, siehe [b2b-entwurf.md](b2b-entwurf.md), „Vor echten Daten“.) |
| E-B1-5 | Womit testen wir B1, bevor es den Kotlin-Client gibt? | Kleines **Go-Werkzeug** `server/cmd/testanmeldung` | Der Kotlin-Test-Client kommt mit dem Schlüsselmodell in B2. Für B1 reicht ein Werkzeug, das den Google-Login im Browser macht und den Server aufruft. |
| E-B1-6 | Namen in Schnittstelle und Code | **Deutsch** ohne Umlaute (`AnmeldenRequest`, `geraet_id`) | Wie im Kill-Switch. `buf lint` verlangt die Endungen `Request`/`Response`. |

## Abläufe

### Erstes Anmelden eines Geräts

```
Gerät                                   Google                  Server
  | 1. Geräteschlüssel erzeugen (P-256)    |                        |
  | 2. Login im Browser, nonce =           |                        |
  |    b64url(SHA-256(DER-Schlüssel)) ---->|                        |
  |<------------------ ID-Token (nonce) ---|                        |
  | 3. Anmelden(ID-Token, öffentl. Schlüssel, Signatur) ----------->|
  |                                        |   4. prüft (s. u.)     |
  |<--------------------- Konto-ID, Geräte-ID, Sitzungs-Token ------|
```

Der Server prüft in Schritt 4 (`// KRITISCH:`, jede Regel mit Ablehnungstest):
1. Signatur des ID-Tokens gegen die öffentlichen Schlüssel von Google
2. Aussteller `https://accounts.google.com`, Zielgruppe ist eine **unserer
   Client-IDs**, nicht abgelaufen, höchstens **5 Minuten** alt
3. `nonce` im Token = SHA-256 des mitgeschickten öffentlichen Schlüssels (DER),
   base64url ohne Auffüllung. So passt ein abgefangenes ID-Token nur zu genau
   diesem Geräteschlüssel.
4. Signatur über das ID-Token mit dem Geräteschlüssel. So beweist das Gerät, dass es
   den privaten Schlüssel hat.

Danach: Konto zu dieser Google-Kennung suchen oder anlegen, Gerät anlegen (gleicher
Schlüssel = gleiches Gerät, also wiederholbar), Sitzung ausstellen.

Jedes Gerät meldet sich in B1 selbst per Google an. In B2/B3 kommen die E2E-Schlüssel
dazu: Ein neues Gerät kann dann zwar angemeldet sein, aber nichts lesen, bis ein
vorhandenes Gerät (QR) oder der Wiederherstellungsschlüssel es freischaltet.

### Sitzung verlängern
`SitzungErneuern(Geräte-ID, Zeitpunkt, Signatur)`. Der Server prüft:
Gerät existiert und ist **aktiv**, Zeitpunkt höchstens 2 Minuten entfernt, Signatur
mit dem gespeicherten öffentlichen Schlüssel stimmt. Kein Google-Login nötig.

### Jede andere Anfrage
Header `Authorization: Bearer <Token>`. Ein Connect-Interceptor (Vorschaltstufe vor
allen Diensten) sucht den Hash des Tokens in Firestore, prüft die Ablaufzeit und
legt Konto- und Geräte-ID in den Kontext. Ohne gültige Sitzung: `unauthenticated`.

### Gerät sperren
`GeraetSperren(Geräte-ID)` – nur für Geräte des **eigenen** Kontos. Setzt den Status
auf `gesperrt` und löscht alle Sitzungen dieses Geräts; es kann keine neue Sitzung
mehr holen. Das eigene Gerät zu sperren ist erlaubt (= abmelden).

**Grenze (Review Lauf 5, aufgehoben 2026-11-05 durch E-B2b-13):** Gesperrt wird der
Geräteschlüssel, nicht das Gerät. Ist auf einem gestohlenen Gerät das Google-Konto
noch angemeldet, kann es einen neuen Geräteschlüssel erzeugen und sich als neues
Gerät wieder anmelden; danach darf es wie jedes Gerät andere Geräte sperren und
Backup oder Datensicherung löschen. Sperren hilft deshalb nur zusammen mit dem
Entfernen des Google-Zugangs auf dem Gerät (Google-Konto → Sicherheit → Geräte).
Seit E-B2b-13 ist ein so neu angemeldetes Gerät nicht freigeschaltet: Andere
Geräte sperren, Backup überschreiben und Datensicherung speichern oder löschen darf
es erst, wenn ein freigeschaltetes Gerät es bestätigt (`GeraetFreischalten`) oder es
mit dem Wiederherstellungsschlüssel signiert ([b2b-entwurf.md](b2b-entwurf.md)).

Signaturen enthalten immer einen festen Zweck-Präfix (`avlreminder/anmelden/v1`,
`avlreminder/sitzung/v1`), damit eine Signatur für das eine nie für das andere passt.

## Schnittstelle (Skizze, `proto/avlreminder/v1/`)

```protobuf
service AnmeldeDienst {
  rpc Anmelden(AnmeldenRequest) returns (AnmeldenResponse);
  rpc SitzungErneuern(SitzungErneuernRequest) returns (SitzungErneuernResponse);
}

service GeraeteDienst {          // braucht eine Sitzung
  rpc GeraeteAuflisten(GeraeteAuflistenRequest) returns (GeraeteAuflistenResponse);
  rpc GeraetSperren(GeraetSperrenRequest) returns (GeraetSperrenResponse);
}

message AnmeldenRequest {
  string google_id_token = 1;
  bytes geraeteschluessel = 2;   // öffentlicher Schlüssel, P-256, PKIX/DER
  bytes signatur = 3;            // ECDSA über Zweck-Präfix + ID-Token
}
message AnmeldenResponse {
  string konto_id = 1;
  string geraet_id = 2;
  Sitzung sitzung = 3;
  bool neues_konto = 4;
}
message Sitzung {
  string token = 1;
  google.protobuf.Timestamp laeuft_ab = 2;
}
message SitzungErneuernRequest {
  string geraet_id = 1;
  google.protobuf.Timestamp zeitpunkt = 2;
  bytes signatur = 3;
}
message Geraet {
  string geraet_id = 1;
  google.protobuf.Timestamp angemeldet_am = 2;
  bool gesperrt = 3;
  bool dieses_geraet = 4;
}
```

Gerätenamen gibt es bewusst nicht im Klartext; ein Name kommt in B2 verschlüsselt
dazu (nur die eigenen Geräte können ihn lesen).

## Daten in Firestore

| Sammlung | Dokument-ID | Felder |
|---|---|---|
| `google_kennungen` | HMAC(Pfeffer, `sub`) | `konto_id` |
| `konten` | zufällige Konto-ID | `erstellt` (Tag) |
| `geraete` | Geräte-ID (aus dem Schlüssel) | `konto_id`, `schluessel` (öffentlich), `gesperrt`, `erstellt`, `gesperrt_am` |
| `sitzungen` | SHA-256 des Tokens | `konto_id`, `geraet_id`, `laeuft_ab` (TTL-Feld: Firestore löscht abgelaufene) |

`geraete` liegt auf oberster Ebene (nicht unter `konten/{id}`): Beim Erneuern kennt
der Server nur die Geräte-ID und liest das Gerät direkt; das Konto steht im Feld
`konto_id`. Konto und Kennung entstehen in einer Transaktion. Bewusst **nicht** gespeichert:
E-Mail, Name, IP-Adressen, „zuletzt gesehen“.

## Code (`server/`, eigenes Go-Modul)

| Paket | Inhalt |
|---|---|
| `cmd/server` | Start, Konfiguration aus Umgebungsvariablen |
| `cmd/testanmeldung` | Werkzeug: Google-Login im Browser (Loopback, PKCE), Anmelden, Geräte auflisten/sperren |
| `internal/geraeteschluessel` | Geräteschlüssel lesen (nur P-256), Geräte-ID, Nonce, signierte Nachrichten |
| `internal/anmeldung` | **KRITISCH**: ID-Token, Nonce und Signatur prüfen |
| `internal/sitzung` | **KRITISCH**: Token erzeugen/prüfen, Interceptor |
| `internal/speicher` | Firestore-Zugriff hinter einer Schnittstelle; Tests gegen den Firestore-Emulator |
| `internal/dienst` | Connect-Handler, verbindet die Pakete |
| `internal/testhilfe` | simuliertes Google und simulierte Geräte für Tests |
| `gen/` | von `buf generate` erzeugt, eingecheckt (die CI prüft, dass er aktuell ist) |

Bibliotheken: `connectrpc.com/connect`, `github.com/coreos/go-oidc/v3` (prüft
Google-ID-Tokens und lässt sich in Tests mit eigenen Schlüsseln füttern),
`cloud.google.com/go/firestore`. ECDSA und SHA-256 aus der Standardbibliothek.

Die Tests sind die Spezifikation, Beispiele für Ablehnungen: falsche Zielgruppe,
abgelaufen, zu alt, falscher Aussteller, Nonce passt nicht zum Schlüssel, Signatur
von anderem Schlüssel, gesperrtes Gerät erneuert, fremdes Gerät sperren,
abgelaufene Sitzung, Token nach dem Sperren.

## Infrastruktur

- Cloud-Run-Dienst `server` in `var.region`: eigenes Dienstkonto `server`,
  `ignore_changes` für das Image (setzt der Deploy), 0–2 Instanzen, 1 CPU nur
  während Anfragen, 256 MiB. Öffentlich aufrufbar (`allUsers` als Invoker, Modul
  `module/oeffentlich`) – die Anmeldung prüft die App selbst. Der Kill-Switch
  sperrt, indem er `allUsers` entfernt (seit 2026-11-05; vorher Ingress).
- Rechte des Kontos `server`: Firestore lesen/schreiben, nur **dieses eine** Secret
  (Pfeffer) lesen.
- Pfeffer: Secret im Secret Manager. OpenTofu erzeugt den Wert so, dass er **nicht**
  im Zustand landet (write-only, OpenTofu ≥ 1.11).
- Firestore: TTL-Regel für `sitzungen.laeuft_ab`.
- Client-IDs als Variable `google_client_ids` (nicht geheim).
- Container: mehrstufiges `Dockerfile`, Laufzeit-Image `distroless/static` als
  Nicht-root. Umgesetzt: Beim Anlegen läuft ein Platzhalter-Image; GitHub Actions
  baut und deployt bei jedem Push auf `main`, der `server/**` ändert (Workload Identity, Konto `deploy` bekommt Deploy-Rechte nur
  auf diesen Dienst).
- Kosten im Leerlauf: 0 €.

## Mit B1 zu erledigen (aus Review Lauf 1)
- **E4** (erledigt): CI prüft beide Go-Module (Kill-Switch und Server) als Matrix;
  gofmt als eigener Schritt.
- **E5** (erledigt 2026-11-01, Repo `funktionen`): eigenes Registry-Repo für die Images der Funktion, damit die Aufräumregeln
  für Server-Images sie nicht treffen.
- **E8** (erledigt 2026-11-01, 60 s fürs Sperren): Zeitlimit beim Trennen vom Rechnungskonto.
- Neu in der CI: `buf lint`, `buf breaking`, Prüfung des erzeugten Codes.

## Reihenfolge
1. Schnittstelle (`proto/`, buf) und Server-Gerüst mit Tests, lokal gegen den
   Firestore-Emulator – **ohne GCP**
2. CI erweitern (E4, buf)
3. Infrastruktur: Plan zeigen, nach deinem Go anwenden; erstes Image
4. Test in der Cloud mit `testanmeldung` (du meldest dich im Browser an)
5. Deploy aus GitHub Actions bei Merge nach `main`
6. E5, E8, Doku, Review-Lauf 2

## Bewusst nicht in B1
Rate-Limits pro Konto (kommt mit B6; bis dahin begrenzen 2 Instanzen und der
Kill-Switch die Kosten), Konto löschen (B4), Play Integrity (B6), Web- und
Android-Client (mit der App-Anbindung).
