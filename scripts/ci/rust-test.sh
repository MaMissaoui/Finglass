#!/usr/bin/env bash
# The checks of CI's "Test" job. ci.yml and scripts/ci-local.sh both run this file.
set -euo pipefail
cd "$(dirname "$0")/../.."

export RUSTFLAGS="${RUSTFLAGS:--D warnings}"
# Full debug info makes the generated Slint UI code slow to compile and link; file and line numbers in
# backtraces are all CI needs.
export CARGO_PROFILE_DEV_DEBUG="${CARGO_PROFILE_DEV_DEBUG:-line-tables-only}"

printf '\n== cargo test\n'
cargo test --workspace --locked

if [ "$(uname -s)" = Darwin ]; then
  printf '\n== Mac installer test\n'
  bash scripts/tests/install-mac.sh
fi
