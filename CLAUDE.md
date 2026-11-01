# avlreminder-backend – Hinweise für Claude

Experimentelles Backend für avlreminder (Android-App in `../avlreminder`) auf Google
Cloud. Lernprojekt, vorerst **nicht mit der App verbunden**. Schwerpunkte:
Synchronisation zwischen Anwendern mit E2E-Verschlüsselung, EU-Datenhaltung, DSGVO,
Cloud-KI nur nach Freigabe, möglichst günstig (max. 20 €/Monat).
Kommunikation auf **Deutsch, mit ß**.

## Wo was steht
- [docs/anforderungen.md](docs/anforderungen.md) – Anforderungen, offene Fragen (Kap. 10), Entscheidungen (Kap. 11)
- [docs/gcp-einrichtung.md](docs/gcp-einrichtung.md) – was Kevin in GCP vorab tun muss
- [docs/entwicklungsumgebung.md](docs/entwicklungsumgebung.md) – installierte Werkzeuge
- [docs/go-review.md](docs/go-review.md) – Review-Anleitung; Code und Tests danach ausrichten

## Code
- Go-Code erklärend kommentieren (Kevin kennt Go nur grob). Kevin liest vor allem
  Tests und Code mit `// KRITISCH:` – Tests sind die Spezifikation (tabellengetrieben,
  deutsche Testfallnamen, zu jeder Erlaubnis ein Ablehnungsfall).

## Zusammenarbeit
- Kevin entwickelt die Anforderungen iterativ mit Claude: Feedback und Rückfragen geben,
  Empfehlung statt Optionsliste.
- Neue Anforderungen und Entscheidungen in `docs/anforderungen.md` festhalten
  (Entscheidungstabelle mit Datum).
- Keine echten Inhalte aus Kevins App, keine Schlüssel oder Secrets ins Repo.
- GCP-Ressourcen, die Geld kosten, erst nach Kevins Go anlegen.

## Git
- Remote `origin`: GitHub `FairPlayer4/avlreminder-backend` (privat). `develop` ist der
  Hauptarbeitsbranch; `main` nur per Pull Request.
- Commit-Nachrichten auf Deutsch.
