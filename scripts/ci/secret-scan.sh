#!/usr/bin/env bash
# Fails when the FinTS product id secret appears in the checked-out tree or in
# any commit. Prints only which check failed; never the id, a commit or a
# matching line.
set -euo pipefail
cd "$(dirname "$0")/../.."

id="${ID:-}"
if [[ -z "$id" ]]; then
  echo "product id secret not available; skipping"
  exit 0
fi

# A scan that cannot run must fail, never pass: not a repository, or a shallow
# clone whose history is incomplete.
if ! shallow="$(git rev-parse --is-shallow-repository 2>/dev/null)"; then
  echo "the secret scan needs a git checkout" >&2
  exit 1
fi
if [[ "$shallow" != "false" ]]; then
  echo "the secret scan needs the full history (fetch-depth: 0)" >&2
  exit 1
fi

# git grep exits 0 when it finds the id, 1 when it does not, and >1 on errors.
status=0
git grep -F -q -e "$id" >/dev/null 2>&1 || status=$?
if [[ "$status" -eq 0 ]]; then
  echo "the FinTS product id appears in the checked-out tree" >&2
  exit 1
fi
if [[ "$status" -ne 1 ]]; then
  echo "the tree scan failed" >&2
  exit 1
fi

if ! commits="$(git log --all -S"$id" --format=%H 2>/dev/null)"; then
  echo "the history scan failed" >&2
  exit 1
fi
if [[ -n "$commits" ]]; then
  echo "the FinTS product id appears in the commit history" >&2
  exit 1
fi

echo "no FinTS product id leak found"
