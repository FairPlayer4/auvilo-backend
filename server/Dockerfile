# Container-Image des Servers, zwei Stufen:
#  1. "bauen": kompiliert den Go-Server im offiziellen Go-Image.
#  2. Laufzeit: nur die fertige Datei auf "distroless/static" – kein Shell, kein
#     Paketmanager, läuft als Nicht-root (Benutzer 65532).
#
# Bauen (aus dem Ordner server/): docker build -t server .
# Die Basis-Images sind per Digest festgelegt; Dependabot schlägt Updates vor.

FROM golang:1.27-trixie@sha256:f18c3d73a184fb0582828510722e723b63a93f7188d11dd223d9be6f6880e65d AS bauen
WORKDIR /src

# Erst nur die Abhängigkeiten laden: Diese Schicht bleibt im Cache, solange
# go.mod und go.sum gleich bleiben.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO_ENABLED=0: rein statische Datei, läuft ohne C-Bibliotheken.
# -trimpath: keine lokalen Pfade in der Datei. -s -w: ohne Debug-Infos (kleiner).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=bauen /server /server
USER nonroot:nonroot
ENTRYPOINT ["/server"]
