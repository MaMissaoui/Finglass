#!/usr/bin/env bash
# The Mac installer test, moved out of the Rust test job when the Go app became the packaged one.
# ci.yml and scripts/ci-local.sh both run this file.
set -euo pipefail
cd "$(dirname "$0")/../.."

if [ "$(uname -s)" != "Darwin" ]; then
  echo "the Mac installer test only runs on macOS; skipping." >&2
  exit 0
fi

printf '\n== Mac installer test\n'
bash scripts/tests/install-mac.sh
