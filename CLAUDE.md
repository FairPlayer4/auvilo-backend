# avlreminder-backend – Hinweise für Claude

Experimentelles Backend für avlreminder (Android-App in `../avlreminder`) auf Google
Cloud. Lernprojekt, vorerst **nicht mit der App verbunden**. Schwerpunkte:
Synchronisation zwischen Anwendern mit E2E-Verschlüsselung, EU-Datenhaltung, DSGVO,
Cloud-KI nur nach Freigabe, möglichst günstig (max. 20 €/Monat).
Kommunikation auf **Deutsch, mit ß**.

## Wo was steht
- [docs/anforderungen.md](docs/anforderungen.md) – Anforderungen, offene Fragen (Kap. 10), Entscheidungen (Kap. 11)

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
