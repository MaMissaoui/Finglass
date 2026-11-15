#!/usr/bin/env bash
# The Go checks of the portability probe: build, vet and the tagged test suite
# on whatever host runs this. .github/workflows/portability.yml runs it on
# Ubuntu and Windows; it also runs locally on macOS.
#
# With GITHUB_STEP_SUMMARY set, the job summary lists build, vet and test as
# passed or failed for this OS. Exit status is non-zero when a check failed;
# the workflow job is continue-on-error, so the probe never blocks CI.
set -euo pipefail
cd "$(dirname "$0")/../.."

os_label="${PORTABILITY_OS:-}"
if [ -z "$os_label" ]; then
  case "$(uname -s)" in
    Darwin) os_label="macOS" ;;
    Linux) os_label="Linux" ;;
    MINGW*|MSYS*|CYGWIN*) os_label="Windows" ;;
    *) os_label="$(uname -s)" ;;
  esac
fi

summary_rows=""
overall=0

step_begin() {
  printf '\n== %s (%s)\n' "$1" "$os_label"
}

step_end() {
  printf '== %s (%ss)\n' "$1" "$2"
}

record() {
  summary_rows="${summary_rows}| $1 | $2 |\n"
}

run_check() {
  local name=$1
  shift
  local start=$SECONDS
  local status=0
  step_begin "$name"
  "$@" || status=$?
  if [ "$status" -eq 0 ]; then
    record "$name" "passed"
    printf '== %s: passed\n' "$name"
  else
    record "$name" "failed"
    printf '== %s: FAILED (exit %s)\n' "$name" "$status" >&2
    overall=1
  fi
  step_end "$name" $((SECONDS - start))
}

# shellcheck disable=SC2329 # invoked by the EXIT trap below.
write_summary() {
  [ -n "${GITHUB_STEP_SUMMARY:-}" ] || return 0
  {
    printf '## Portability (probe) - %s\n\n' "$os_label"
    printf '| Check | Result |\n'
    printf '| --- | --- |\n'
    printf '%b' "$summary_rows"
  } >> "$GITHUB_STEP_SUMMARY"
}
trap write_summary EXIT

step_begin "go env"
start=$SECONDS
go env || overall=1
step_end "go env" $((SECONDS - start))

run_check "go build" go build ./...
run_check "go vet" go vet ./...
run_check "go test (finglass_testkdf)" go test -tags finglass_testkdf ./...

printf '\n== Portability summary (%s)\n' "$os_label"
printf '| Check | Result |\n'
printf '| --- | --- |\n'
printf '%b' "$summary_rows"

exit "$overall"
