#!/usr/bin/env sh
set -eu

MIN_GO_MAJOR=1
MIN_GO_MINOR=25
BOOTSTRAP_VERSION=1.26.4

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TOOLS_DIR="$ROOT_DIR/.tools"

go_version_minor() {
	"$1" version 2>/dev/null | awk '{print $3}' | sed 's/^go//' | awk -F. '{print $1 " " $2}'
}

go_is_new_enough() {
	version=$("$1" version 2>/dev/null | awk '{print $3}' | sed 's/^go//') || return 1
	major=$(printf '%s\n' "$version" | awk -F. '{print $1}')
	minor=$(printf '%s\n' "$version" | awk -F. '{print $2}')

	if [ "${major:-0}" -gt "$MIN_GO_MAJOR" ]; then
		return 0
	fi
	if [ "${major:-0}" -eq "$MIN_GO_MAJOR" ] && [ "${minor:-0}" -ge "$MIN_GO_MINOR" ]; then
		return 0
	fi
	return 1
}

if command -v go >/dev/null 2>&1 && go_is_new_enough "$(command -v go)"; then
	command -v go
	exit 0
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
esac

case "$os/$arch" in
	darwin/amd64)
		sha256=05dc9b5f9997744520aaebb3d5deaa7c755371aebbfb7f97c2511a9f3367538d
		;;
	darwin/arm64)
		sha256=b62ad2b6d7d2464f12a5bcad7ff47f19d08325773b5efd21610e445a05a9bf53
		;;
	linux/amd64)
		sha256=1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f
		;;
	*)
		echo "go 1.25+ is required; automatic bootstrap is not configured for $os/$arch" >&2
		exit 1
		;;
esac

toolchain_dir="$TOOLS_DIR/go$BOOTSTRAP_VERSION-$os-$arch"
go_bin="$toolchain_dir/go/bin/go"
archive="$TOOLS_DIR/go$BOOTSTRAP_VERSION.$os-$arch.tar.gz"

if [ ! -x "$go_bin" ]; then
	mkdir -p "$TOOLS_DIR"
	url="https://go.dev/dl/go$BOOTSTRAP_VERSION.$os-$arch.tar.gz"
	echo "System Go is too old; downloading Go $BOOTSTRAP_VERSION for $os/$arch..." >&2
	curl -L -o "$archive" "$url"
	actual=$(shasum -a 256 "$archive" | awk '{print $1}')
	if [ "$actual" != "$sha256" ]; then
		echo "checksum mismatch for $archive" >&2
		echo "expected: $sha256" >&2
		echo "actual:   $actual" >&2
		exit 1
	fi
	rm -rf "$toolchain_dir"
	mkdir -p "$toolchain_dir"
	tar -C "$toolchain_dir" -xzf "$archive"
fi

printf '%s\n' "$go_bin"
