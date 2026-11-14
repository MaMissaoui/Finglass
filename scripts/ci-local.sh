#!/usr/bin/env bash
# Runs CI locally, as far as this machine can.
#
# - The macOS jobs run natively, through the same scripts/ci/*.sh files ci.yml runs. GitHub's runner can't be
#   emulated: Docker can't run macOS, so this script runs the job scripts directly instead.
# - With Docker running, the Go checks also run in a Linux container. CI is macOS-only until FinDesk parity
#   (docs/PLAN.md), so this is an extra portability check, not a copy of a CI job.
#
# The jobs run in parallel lanes: Go (lint, tests and the real-KDF tests at once), then frontend and package
# in sequence (both run npm ci in frontend/), the installer, and the Linux container.
#
# Usage: scripts/ci-local.sh [go] [frontend] [package] [install] [linux] [secret-scan]
# (no arguments: all of the default jobs; secret-scan is optional)
# Logs and status lines: build/ci-local/<job>.log and build/ci-local/<job>.status.
# Exit status is non-zero if any job failed.
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

want_go=0
want_frontend=0
want_package=0
want_install=0
want_linux=0
want_secret_scan=0

for job in "${jobs[@]}"; do
  case "$job" in
    go) want_go=1 ;;
    frontend) want_frontend=1 ;;
    package) want_package=1 ;;
    install) want_install=1 ;;
    linux) want_linux=1 ;;
    secret-scan) want_secret_scan=1 ;;
    *)
      echo "unknown job: $job (expected go, frontend, package, install, linux or secret-scan)" >&2
      exit 2
      ;;
  esac
done

log_dir="$repo_root/build/ci-local"
mkdir -p "$log_dir"
rm -f "$log_dir"/*.status

overall_start=$SECONDS
pids=()

run_job() {
  local name=$1
  shift
  local log="$log_dir/$name.log"
  local status="$log_dir/$name.status"
  local start=$SECONDS

  printf '%-10s running (log: %s)\n' "$name" "${log#"$repo_root"/}"
  if "$@" >"$log" 2>&1; then
    printf '%-10s PASS  %4ss\n' "$name" $((SECONDS - start)) >"$status"
  else
    printf '%-10s FAIL  %4ss  see %s\n' "$name" $((SECONDS - start)) "${log#"$repo_root"/}" >"$status"
  fi
}

skip_job() {
  local name=$1
  local reason=$2
  printf '%-10s SKIP  %s\n' "$name" "$reason" >"$log_dir/$name.status"
}

spawn() {
  "$@" &
  last_pid=$!
  pids+=("$last_pid")
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

# Lane 1: the three Go sub-jobs at once.
if [ "$want_go" -eq 1 ]; then
  if command -v go >/dev/null; then
    spawn run_job go-test bash scripts/ci/go.sh test
    spawn run_job go-realkdf bash scripts/ci/go.sh test-real-kdf
    if command -v golangci-lint >/dev/null; then
      spawn run_job go-lint bash scripts/ci/go.sh lint
    else
      skip_job go-lint "needs golangci-lint on PATH"
    fi
  else
    skip_job go-lint "needs go and golangci-lint on PATH"
    skip_job go-test "needs go on PATH"
    skip_job go-realkdf "needs go on PATH"
  fi
fi

# frontend/node_modules holds a Go package (flatted); go.mod ignores that folder, so ./... in the Go lane never
# walks it while npm ci recreates it.

# Lane 2: frontend then package, sequentially because both run npm ci in frontend/.
if [ "$want_frontend" -eq 1 ] && ! command -v npm >/dev/null; then
  skip_job frontend "needs node and npm on PATH"
  want_frontend=0
fi
if [ "$want_package" -eq 1 ] && [ "$(uname -s)" != Darwin ]; then
  skip_job package "macOS only"
  want_package=0
fi
if [ "$want_frontend" -eq 1 ] || [ "$want_package" -eq 1 ]; then
  (
    if [ "$want_frontend" -eq 1 ]; then
      run_job frontend bash scripts/ci/frontend.sh
    fi
    if [ "$want_package" -eq 1 ]; then
      run_job package bash scripts/ci/go-package.sh
    fi
  ) &
  pids+=("$!")
fi

# Lane 3: the Mac installer test.
if [ "$want_install" -eq 1 ]; then
  if [ "$(uname -s)" = Darwin ]; then
    spawn run_job install bash scripts/ci/install-mac.sh
  else
    skip_job install "macOS only"
  fi
fi

# Lane 4: the Linux portability container.
if [ "$want_linux" -eq 1 ]; then
  if command -v docker >/dev/null && docker info >/dev/null 2>&1; then
    spawn run_job linux linux_go
  else
    skip_job linux "Docker is not running"
  fi
fi

# Optional: the product-id scan, exactly as before.
if [ "$want_secret_scan" -eq 1 ]; then
  if [ -n "${ID:-}" ]; then
    spawn run_job secret-scan bash scripts/ci/secret-scan.sh
  else
    skip_job secret-scan "ID is not set"
  fi
fi

if [ ${#pids[@]} -gt 0 ]; then
  for pid in "${pids[@]}"; do
    wait "$pid" || true
  done
fi

printf '\n'
failed=0
order=(go-lint go-test go-realkdf frontend package install linux secret-scan)
for job in "${order[@]}"; do
  status="$log_dir/$job.status"
  if [ -f "$status" ]; then
    cat "$status"
    if grep -q 'FAIL' "$status"; then
      failed=1
    fi
  fi
done
printf '\nTotal wall time: %ss\n' $((SECONDS - overall_start))
exit "$failed"
