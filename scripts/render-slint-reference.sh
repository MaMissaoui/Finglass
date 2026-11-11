#!/usr/bin/env bash
# Renders the Slint app's reference screenshots, the visual target of the Go UI (docs/PLAN.md, G1, G7–G8).
#
# They are not committed: the Rust UI stays in git history, so this script checks the last Rust UI commit out
# into a temporary worktree and runs its headless screenshot suite there. Only the seeded, synthetic fixtures are
# rendered; the real-data test is skipped because FINGLASS_SCREENSHOT_DATA is never set here.
#
# Builds go to target/slint-reference, their own cache, so they never invalidate the main target/ build.
#
# Usage: scripts/render-slint-reference.sh <output-dir> [git-ref]
#   git-ref defaults to 107faa8, the last commit before the Go migration (Finglass 0.3.4 plus the chart fix).
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: $0 <output-dir> [git-ref]" >&2
  exit 2
fi

repo_root=$(cd "$(dirname "$0")/.." && pwd)
ref=${2:-107faa8}
mkdir -p "$1"
out=$(cd "$1" && pwd)

case "$out/" in
  "$repo_root"/*)
    echo "the output folder must be outside the checkout: $out" >&2
    exit 2
    ;;
esac

work=$(mktemp -d "${TMPDIR:-/tmp}/finglass-slint-ref.XXXXXX")
cleanup() {
  git -C "$repo_root" worktree remove --force "$work/tree" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

git -C "$repo_root" worktree add --quiet --detach "$work/tree" "$ref"

(
  cd "$work/tree"
  unset FINGLASS_SCREENSHOT_DATA FINGLASS_SCREENSHOT_PASSPHRASE
  FINGLASS_HOME="$work/home" FINGLASS_SCREENSHOTS="$out" \
    CARGO_TARGET_DIR="$repo_root/target/slint-reference" \
    cargo test --quiet -p finglass --test screenshots
)

count=$(find "$out" -name '*.png' | wc -l | tr -d ' ')
echo "Rendered $count reference screenshots from $ref into $out"
