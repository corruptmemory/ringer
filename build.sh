#!/usr/bin/env bash
# build.sh — the ONLY entry point for building and testing ringer.
set -euo pipefail
cd "$(dirname "$0")"

HTMX_VERSION="2.0.4"
IDIOMORPH_VERSION="0.7.3"
VENDOR_DIR="internal/hud/static/vendor"

RACE=""
RUN_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --test) RUN_TESTS=1 ;;
    --race) RACE="-race" ;;
    --refresh-htmx)
      mkdir -p "$VENDOR_DIR"
      curl -fsSL "https://unpkg.com/htmx.org@${HTMX_VERSION}/dist/htmx.min.js" -o "$VENDOR_DIR/htmx.min.js"
      echo "refreshed htmx ${HTMX_VERSION}"; exit 0 ;;
    --refresh-idiomorph)
      mkdir -p "$VENDOR_DIR"
      curl -fsSL "https://unpkg.com/idiomorph@${IDIOMORPH_VERSION}/dist/idiomorph-ext.min.js" -o "$VENDOR_DIR/idiomorph.min.js"
      echo "refreshed idiomorph ${IDIOMORPH_VERSION}"; exit 0 ;;
    *) echo "usage: ./build.sh [--test [--race]] | [--refresh-htmx] | [--refresh-idiomorph]" >&2; exit 2 ;;
  esac
done

# Generate templ views (*_templ.go) before formatting/vetting/building.
go tool templ generate

UNFORMATTED=$(gofmt -l cmd internal 2>/dev/null || true)
if [ -n "$UNFORMATTED" ]; then
  echo "gofmt needed on:" >&2; echo "$UNFORMATTED" >&2; exit 1
fi

go vet ./...
CGO_ENABLED=0 go build -o ringer ./cmd/ringer

if [ "$RUN_TESTS" = "1" ]; then
  # -race implies cgo-capable toolchain for the test binary only; the shipped
  # binary above is always CGO_ENABLED=0.
  go test $RACE ./...
fi
