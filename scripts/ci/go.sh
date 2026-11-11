#!/usr/bin/env bash
# The Go checks of CI's "Go" job. ci.yml and scripts/ci-local.sh both run this file, so they cannot drift apart.
# Needs go and golangci-lint on PATH (CI installs golangci-lint with its action first).
set -euo pipefail
cd "$(dirname "$0")/../.."

step() { printf '\n== %s\n' "$*"; }

step "gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  printf 'not gofmt-formatted:\n%s\n' "$unformatted" >&2
  exit 1
fi

step "go vet"
go vet ./...

step "golangci-lint"
golangci-lint run ./...

step "go test"
go test ./...
