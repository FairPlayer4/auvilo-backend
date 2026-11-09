# Image mit buf und den beiden Protobuf-Plugins, für werkzeuge/buf.sh.
# Die Versionen übergibt das Skript aus .github/workflows/ci.yml (Job "proto")
# und server/go.mod, damit lokal genau derselbe Code entsteht wie in der CI.
ARG GO_VERSION
FROM golang:${GO_VERSION}

ARG BUF_VERSION
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_CONNECT_GO_VERSION

# Fehlt eine Version, soll der Bau scheitern statt "latest" zu nehmen.
RUN test -n "$BUF_VERSION" && test -n "$PROTOC_GEN_GO_VERSION" && test -n "$PROTOC_GEN_CONNECT_GO_VERSION" \
 && go install github.com/bufbuild/buf/cmd/buf@"$BUF_VERSION" \
 && go install google.golang.org/protobuf/cmd/protoc-gen-go@"$PROTOC_GEN_GO_VERSION" \
 && go install connectrpc.com/connect/cmd/protoc-gen-connect-go@"$PROTOC_GEN_CONNECT_GO_VERSION" \
 && go clean -cache -modcache

# git braucht "buf breaking" für den Vergleich mit main; das Repo gehört im
# Container einem anderen Nutzer, deshalb safe.directory.
RUN git config --system --add safe.directory '*'

WORKDIR /repo/proto
ENTRYPOINT ["buf"]
