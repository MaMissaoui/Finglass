#!/usr/bin/env bash
# The Rust checks of CI's "Lint" job. ci.yml and scripts/ci-local.sh both run this file.
set -euo pipefail
cd "$(dirname "$0")/../.."

export RUSTFLAGS="${RUSTFLAGS:--D warnings}"

printf '\n== cargo fmt\n'
cargo fmt --all --check

printf '\n== cargo clippy\n'
cargo clippy --workspace --all-targets --locked
