#!/usr/bin/env sh
set -eu

GO_BIN=${GO_BIN:-"$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/go.sh"}
GO_BIN=$("$GO_BIN")

CGO_ENABLED=0 "$GO_BIN" test ./...
