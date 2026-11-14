#!/usr/bin/env bash
# The Go checks of CI. ci.yml and scripts/ci-local.sh both run this file, so they cannot drift apart.
# Needs go and golangci-lint on PATH (CI installs golangci-lint with its action first).
#
# Usage: scripts/ci/go.sh [all|lint|test|test-real-kdf]
#   all            lint, test and test-real-kdf (the default)
#   lint           gofmt, go vet (with and without the test tag), golangci-lint, the kdftest guard,
#                  the bindings staleness check and the product-id tests
#   test           the whole suite with the cheap test-only KDF
#   test-real-kdf  the storage packages without the tag (the real KDF end to end)
set -euo pipefail
cd "$(dirname "$0")/../.."

step_name=""
step_start=0

step_begin() {
  step_name=$1
  step_start=$SECONDS
  printf '\n== %s\n' "$step_name"
}

step_end() {
  printf '== %s (%ss)\n' "$step_name" $((SECONDS - step_start))
}

run_lint() {
  step_begin "gofmt"
  unformatted=$(gofmt -l .)
  if [ -n "$unformatted" ]; then
    printf 'not gofmt-formatted:\n%s\n' "$unformatted" >&2
    exit 1
  fi
  step_end

  step_begin "go vet"
  go vet ./...
  step_end

  step_begin "go vet (finglass_testkdf)"
  go vet -tags finglass_testkdf ./...
  step_end

  step_begin "golangci-lint"
  golangci-lint run ./...
  step_end

  step_begin "kdftest guard"
  # The test helper must never reach the production binary.
  if go list -deps ./cmd/finglass | grep -Fq 'internal/core/db/kdftest'; then
    echo "internal/core/db/kdftest is in the dependencies of ./cmd/finglass" >&2
    exit 1
  fi
  # The tagged file must not be part of a normal build.
  if go list -tags '' -deps -f '{{.ImportPath}} {{.GoFiles}}' ./internal/core/db | grep -Fq 'testkdf.go'; then
    echo "testkdf.go is part of the normal ./internal/core/db build" >&2
    exit 1
  fi
  # Packaging and the workflows must never pass the test tag.
  if grep -rn -- 'finglass_testkdf' scripts .github packaging 2>/dev/null |
    grep -vE '^scripts/ci/|^scripts/ci-local\.sh:' | grep -q .; then
    echo "a file outside scripts/ci/ and scripts/ci-local.sh mentions finglass_testkdf" >&2
    exit 1
  fi
  step_end

  step_begin "bindings"
  go tool wails3 generate bindings -ts ./cmd/finglass
  if ! git diff --exit-code -- frontend/bindings; then
    echo "frontend/bindings is stale; commit the regenerated bindings" >&2
    exit 1
  fi
  step_end

  step_begin "product id"
  bash scripts/tests/product-id.sh
  step_end
}

run_test() {
  step_begin "go test (finglass_testkdf)"
  go test -tags finglass_testkdf ./...
  step_end
}

run_test_real_kdf() {
  step_begin "go test (real KDF)"
  go test ./internal/core/db/... ./internal/core/backup/... ./internal/core/profiles/...
  step_end
}

mode="${1:-all}"
case "$mode" in
  all)
    run_lint
    run_test
    run_test_real_kdf
    ;;
  lint)
    run_lint
    ;;
  test)
    run_test
    ;;
  test-real-kdf)
    run_test_real_kdf
    ;;
  *)
    echo "usage: $0 [all|lint|test|test-real-kdf]" >&2
    exit 2
    ;;
esac
