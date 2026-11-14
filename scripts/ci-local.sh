#!/usr/bin/env bash
# Runs CI locally, as far as this machine can.
#
# - The macOS jobs (Go, Installer) run natively, through the same scripts/ci/*.sh files ci.yml runs.
#   GitHub's runner can't be emulated: Docker can't run macOS, so this script runs the job scripts directly
#   instead.
# - With Docker running, the Go checks also run in a Linux container. CI is macOS-only until FinDesk parity
#   (docs/PLAN.md), so this is an extra portability check, not a copy of a CI job.
#
# Usage: scripts/ci-local.sh [go] [frontend] [package] [install] [linux] [secret-scan]
# (no arguments: all of the default jobs; secret-scan is optional)
# Logs: build/ci-local/<job>.log. Exit status is non-zero if any job failed.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

# The Linux container: golangci-lint's Go image plus Wails's GTK4/WebKitGTK headers (scripts/ci/linux.Dockerfile,
# whose base image version must stay in step with ci.yml's golangci-lint-action). Built locally, cached by Docker.
linux_image="finglass-ci-linux:local"

jobs=("$@")
if [ ${#jobs[@]} -eq 0 ]; then
  jobs=(go frontend package install linux)
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

  printf '%-8s running (log: %s)\n' "$name" "${log#"$repo_root"/}"
  if "$@" >"$log" 2>&1; then
    results+=("$(printf '%-8s PASS  %4ss' "$name" $((SECONDS - start)))")
  else
    results+=("$(printf '%-8s FAIL  %4ss  see %s' "$name" $((SECONDS - start)) "${log#"$repo_root"/}")")
    failed=1
  fi
}

skip_job() {
  results+=("$(printf '%-8s SKIP  %s' "$1" "$2")")
}

# shellcheck disable=SC2329 # passed to run_job, which invokes it.
linux_go() {
  # A copy of the working tree (tracked and new files, without ignored ones), so the container checks
  # uncommitted changes too and never writes into the checkout.
  local src status=0
  src=$(mktemp -d "${TMPDIR:-/tmp}/finglass-ci-linux.XXXXXX")
  git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf - | tar -xf - -C "$src"

  docker build -q -t "$linux_image" -f scripts/ci/linux.Dockerfile scripts/ci >/dev/null || return 1

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
    frontend)
      if command -v npm >/dev/null; then
        run_job frontend bash scripts/ci/frontend.sh
      else
        skip_job frontend "needs node and npm on PATH"
      fi
      ;;
    package)
      if [ "$(uname -s)" = Darwin ]; then
        run_job package bash scripts/ci/go-package.sh
      else
        skip_job package "macOS only"
      fi
      ;;
    install)
      if [ "$(uname -s)" = Darwin ]; then
        run_job install bash scripts/ci/install-mac.sh
      else
        skip_job install "macOS only"
      fi
      ;;
    linux)
      if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
        run_job linux linux_go
      else
        skip_job linux "Docker is not running"
      fi
      ;;
    secret-scan)
      if [ -n "${ID:-}" ]; then
        run_job secret-scan bash scripts/ci/secret-scan.sh
      else
        skip_job secret-scan "ID is not set"
      fi
      ;;
    *)
      echo "unknown job: $job (expected go, frontend, package, install, linux or secret-scan)" >&2
      exit 2
      ;;
  esac
done

printf '\n'
printf '%s\n' "${results[@]}"
exit "$failed"
