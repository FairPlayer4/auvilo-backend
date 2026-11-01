# Container-Image des Servers, zwei Stufen:
#  1. "bauen": kompiliert den Go-Server im offiziellen Go-Image.
#  2. Laufzeit: nur die fertige Datei auf "distroless/static" – kein Shell, kein
#     Paketmanager, läuft als Nicht-root (Benutzer 65532).
#
# Bauen (aus dem Ordner server/): docker build -t server .
# Die Basis-Images sind per Digest festgelegt; Dependabot schlägt Updates vor.

FROM golang:1.27-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 AS bauen
WORKDIR /src

# Erst nur die Abhängigkeiten laden: Diese Schicht bleibt im Cache, solange
# go.mod und go.sum gleich bleiben.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: rein statische Datei, läuft ohne C-Bibliotheken.
# -trimpath: keine lokalen Pfade in der Datei. -s -w: ohne Debug-Infos (kleiner).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --from=bauen /server /server
USER nonroot:nonroot
ENTRYPOINT ["/server"]
