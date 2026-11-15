#!/usr/bin/env bash
# Tests scripts/ci/check-release-scope.sh against the real tree and small bad
# samples. Runs offline and is part of scripts/ci/go.sh.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
guard="$repo_root/scripts/ci/check-release-scope.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/finglass-release-scope-test.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

passed=0

pass() {
  passed=$((passed + 1))
  printf 'ok - %s\n' "$1"
}

fail() {
  printf 'FAIL - %s\n' "$1" >&2
  exit 1
}

make_fixture() {
  local dir=$1
  mkdir -p "$dir/.github/workflows"
  cp "$repo_root/.github/workflows/installers.yml" "$dir/.github/workflows/installers.yml"
  cp "$repo_root/.github/workflows/package.yml" "$dir/.github/workflows/package.yml"
}

assert_passes() {
  local dir=$1 label=$2 log="$tmp/pass.log"
  if ! bash "$guard" "$dir" >"$log" 2>&1; then
    cat "$log" >&2
    fail "$label"
  fi
  pass "$label"
}

assert_fails() {
  local dir=$1 label=$2 needle=$3 log="$tmp/fail.log"
  if bash "$guard" "$dir" >"$log" 2>&1; then
    fail "$label (the guard passed)"
  fi
  if ! grep -Fq -- "$needle" "$log"; then
    cat "$log" >&2
    fail "$label (the guard failed for another reason)"
  fi
  pass "$label"
}

# The real tree must pass.
assert_passes "$repo_root" "the real tree passes"

# A tags trigger in installers.yml must fail.
fixture="$tmp/tags"
make_fixture "$fixture"
printf '    tags: ["v*"]\n' >>"$fixture/.github/workflows/installers.yml"
assert_fails "$fixture" "a tags: trigger in installers.yml fails" "tags:"

# A secrets reference in installers.yml must fail.
fixture="$tmp/secrets"
make_fixture "$fixture"
# shellcheck disable=SC2016
printf '  token: ${{ secrets.FINGLASS_FINTS_PRODUCT_ID }}\n' >>"$fixture/.github/workflows/installers.yml"
assert_fails "$fixture" "a secrets reference in installers.yml fails" "secrets"

# A non-macOS runner in package.yml must fail.
fixture="$tmp/runner"
make_fixture "$fixture"
sed 's/runs-on: macos-latest/runs-on: ubuntu-latest/' \
  "$fixture/.github/workflows/package.yml" >"$fixture/.github/workflows/package.yml.new"
mv "$fixture/.github/workflows/package.yml.new" "$fixture/.github/workflows/package.yml"
assert_fails "$fixture" "an ubuntu-latest runner in package.yml fails" "runs-on"

printf '\n%d release-scope cases passed.\n' "$passed"
