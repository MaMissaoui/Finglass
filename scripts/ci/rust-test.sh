#!/usr/bin/env bash
# The checks of CI's "Test" job. ci.yml and scripts/ci-local.sh both run this file.
set -euo pipefail
cd "$(dirname "$0")/../.."

export RUSTFLAGS="${RUSTFLAGS:--D warnings}"

printf '\n== cargo test\n'
cargo test --workspace --locked
