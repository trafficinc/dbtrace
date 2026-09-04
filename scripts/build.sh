#!/usr/bin/env sh
set -eu

GO_BIN=${GO_BIN:-"$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/go.sh"}
GO_BIN=$("$GO_BIN")

mkdir -p dist

CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 "$GO_BIN" build -o dist/dbtrace-darwin-amd64 ./cmd/dbtrace
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_BIN" build -o dist/dbtrace-darwin-arm64 ./cmd/dbtrace
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$GO_BIN" build -o dist/dbtrace-windows-amd64.exe ./cmd/dbtrace
