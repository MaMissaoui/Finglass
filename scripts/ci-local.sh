#!/usr/bin/env bash
# Runs CI locally, as far as this machine can.
#
# - The macOS jobs (Go, Lint, Test) run natively, through the same scripts/ci/*.sh files ci.yml runs.
#   GitHub's runner can't be emulated: Docker can't run macOS, and act's host mode would install rustup
#   and toolchains outside the repo, so this script runs the job scripts directly instead.
# - With Docker running, the Go checks also run in a Linux container. CI is macOS-only until FinDesk parity
#   (docs/PLAN.md), so this is an extra portability check, not a copy of a CI job.
#
# Usage: scripts/ci-local.sh [go] [lint] [test] [linux]   (no arguments: all of them)
# Logs: build/ci-local/<job>.log. Exit status is non-zero if any job failed.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

# The Linux container's Go and golangci-lint. Keep the version in step with ci.yml's golangci-lint-action.
linux_image="golangci/golangci-lint:v2.15.0"

jobs=("$@")
if [ ${#jobs[@]} -eq 0 ]; then
  jobs=(go lint test linux)
fi

log_dir="$repo_root/build/ci-local"
mkdir -p "$log_dir"

results=()
failed=0

run_job() {
  local name=$1
  shift
  local log="$log_dir/$name.log"
  local start=$SECONDS

  printf '%-6s running (log: %s)\n' "$name" "${log#"$repo_root"/}"
  if "$@" >"$log" 2>&1; then
    results+=("$(printf '%-6s PASS  %4ss' "$name" $((SECONDS - start)))")
  else
    results+=("$(printf '%-6s FAIL  %4ss  see %s' "$name" $((SECONDS - start)) "${log#"$repo_root"/}")")
    failed=1
  fi
}

skip_job() {
  results+=("$(printf '%-6s SKIP  %s' "$1" "$2")")
}

linux_go() {
  # A copy of the working tree (tracked and new files, without ignored ones), so the container checks
  # uncommitted changes too and never writes into the checkout.
  local src status=0
  src=$(mktemp -d "${TMPDIR:-/tmp}/finglass-ci-linux.XXXXXX")
  git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf - | tar -xf - -C "$src"

  docker run --rm \
    -v "$src:/src" -w /src \
    -v finglass-ci-go-mod:/go/pkg/mod \
    -v finglass-ci-go-build:/root/.cache/go-build \
    "$linux_image" \
    bash scripts/ci/go.sh || status=$?

  rm -rf "$src"
  return "$status"
}

for job in "${jobs[@]}"; do
  case "$job" in
    go)
      if command -v go >/dev/null && command -v golangci-lint >/dev/null; then
        run_job go bash scripts/ci/go.sh
      else
        skip_job go "needs go and golangci-lint on PATH"
      fi
      ;;
    lint) run_job lint bash scripts/ci/rust-lint.sh ;;
    test) run_job test bash scripts/ci/rust-test.sh ;;
    linux)
      if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
        run_job linux linux_go
      else
        skip_job linux "Docker is not running"
      fi
      ;;
    *)
      echo "unknown job: $job (expected go, lint, test or linux)" >&2
      exit 2
      ;;
  esac
done

printf '\n'
printf '%s\n' "${results[@]}"
exit "$failed"
