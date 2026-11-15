#!/usr/bin/env bash
# Structural guard for the release boundary, run from scripts/ci/go.sh.
#
# The workflow's own name and comments say "not released"; this checks
# structure, not words:
#   - installers.yml has no tags trigger, no gh release, no action-gh-release,
#     no contents: write and no secrets reference;
#   - package.yml only runs on macOS runners, and its one gh release create
#     line attaches exactly the DMG variable and dist/SHA256SUMS.txt.
#
# Usage: scripts/ci/check-release-scope.sh [repo-root]
set -euo pipefail

root="${1:-$(cd "$(dirname "$0")/../.." && pwd)}"
installers="$root/.github/workflows/installers.yml"
package="$root/.github/workflows/package.yml"

failures=0

fail() {
  printf 'release scope: %s\n' "$1" >&2
  failures=$((failures + 1))
}

if [[ ! -f "$installers" ]]; then
  fail "installers.yml is missing"
fi

if [[ -f "$installers" ]]; then
  if grep -Eq '^[[:space:]]*tags:' "$installers"; then
    fail "installers.yml has a tags: trigger"
  fi
  if grep -Fq 'gh release' "$installers"; then
    fail "installers.yml mentions gh release"
  fi
  if grep -Fq 'action-gh-release' "$installers"; then
    fail "installers.yml uses action-gh-release"
  fi
  if grep -Eq '^[[:space:]]*contents:[[:space:]]*write' "$installers"; then
    fail "installers.yml asks for contents: write"
  fi
  if grep -Fq 'secrets.' "$installers"; then
    fail "installers.yml references secrets"
  fi
fi

if [[ ! -f "$package" ]]; then
  fail "package.yml is missing"
fi

if [[ -f "$package" ]]; then
  while IFS= read -r line; do
    if [[ "$line" =~ ^[[:space:]]*runs-on:[[:space:]]*(.*)$ ]]; then
      runner="${BASH_REMATCH[1]}"
      runner="${runner%"${runner##*[![:space:]]}"}"
      if [[ "$runner" != macos-* ]]; then
        fail "package.yml runs-on is $runner, expected macos-*"
      fi
    fi
  done <"$package"

  release_lines="$(grep -Fc 'gh release create' "$package" || true)"
  if [[ "$release_lines" -ne 1 ]]; then
    fail "package.yml must have exactly one gh release create line"
  fi
  # shellcheck disable=SC2016,SC1003
  expected_release_line='          gh release create "$TAG" "${dmgs[0]}" dist/SHA256SUMS.txt \'
  exact_lines="$(grep -Fxc -- "$expected_release_line" "$package" || true)"
  if [[ "$exact_lines" -ne 1 ]]; then
    fail "package.yml's gh release create line must attach only the DMG and dist/SHA256SUMS.txt"
  fi
fi

if [[ "$failures" -ne 0 ]]; then
  printf 'release scope: %d problem(s)\n' "$failures" >&2
  exit 1
fi

printf 'release scope: ok\n'
