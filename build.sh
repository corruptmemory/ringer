#!/usr/bin/env bash
# build.sh — the ONLY entry point for building and testing ringer.
set -euo pipefail
cd "$(dirname "$0")"

RACE=""
RUN_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --test) RUN_TESTS=1 ;;
    --race) RACE="-race" ;;
    *) echo "usage: ./build.sh [--test [--race]]" >&2; exit 2 ;;
  esac
done

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
