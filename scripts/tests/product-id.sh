#!/usr/bin/env bash
# Tests the product-id plumbing without building the app: the secret scan, the
# packaging script's --validate-only mode, and the -ldflags -X path. Runs
# offline. The product id is never printed.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
scan_script="$repo_root/scripts/ci/secret-scan.sh"
package_script="$repo_root/scripts/go-package-macos.sh"
test_product_id="TESTPRODUCT0000000000000"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/finglass-product-id-test.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

passed=0

pass() {
  passed=$((passed + 1))
  printf 'ok - %s\n' "$1"
}

fail() {
  printf 'FAIL - %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local file=$1 text=$2 label=$3
  grep -F -q -- "$text" "$file" || fail "$label"
}

assert_not_contains() {
  local file=$1 text=$2 label=$3
  if grep -F -q -- "$text" "$file"; then
    fail "$label"
  fi
}

git_env=(env GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_TERMINAL_PROMPT=0)

# make_secret_repo <name> creates a throwaway repository with the scan script at
# the path it expects, so the script's cd lands in the fixture, not this tree.
make_secret_repo() {
  local name=$1
  local repo="$tmp/secret/$name"
  mkdir -p "$repo/scripts/ci"
  cp "$scan_script" "$repo/scripts/ci/secret-scan.sh"
  "${git_env[@]}" git -C "$repo" init -q
  "${git_env[@]}" git -C "$repo" config user.name "Product ID Test"
  "${git_env[@]}" git -C "$repo" config user.email "product-id@example.invalid"
  "${git_env[@]}" git -C "$repo" config commit.gpgsign false
  printf 'initial\n' >"$repo/README.txt"
  "${git_env[@]}" git -C "$repo" add .
  "${git_env[@]}" git -C "$repo" commit -q -m initial
  printf '%s\n' "$repo"
}

run_secret_scan() {
  local repo=$1 id=$2 log=$3
  local rc=0
  set +e
  (cd "$repo" && ID="$id" bash "$repo/scripts/ci/secret-scan.sh") >"$log" 2>&1
  rc=$?
  set -e
  return "$rc"
}

# --- secret scan ------------------------------------------------------------

# A scan that cannot run fails closed: a folder that is not a git checkout.
not_repo="$tmp/secret/not-a-repo"
mkdir -p "$not_repo/scripts/ci"
cp "$scan_script" "$not_repo/scripts/ci/secret-scan.sh"
log="$tmp/secret-not-a-repo.log"
if (cd "$not_repo" && GIT_CEILING_DIRECTORIES="$tmp" ID="$test_product_id" \
  bash "$not_repo/scripts/ci/secret-scan.sh") >"$log" 2>&1; then
  fail "the secret scan passed outside a git checkout"
fi
assert_not_contains "$log" "$test_product_id" "the failed scan printed the product id"
pass "the secret scan fails outside a git checkout"

# A shallow clone fails closed too: its history is incomplete.
repo=$(make_secret_repo shallow-source)
printf 'second\n' >"$repo/second.txt"
"${git_env[@]}" git -C "$repo" add second.txt
"${git_env[@]}" git -C "$repo" commit -q -m second
shallow_clone="$tmp/secret/shallow"
"${git_env[@]}" git clone -q --depth 1 "file://$repo" "$shallow_clone"
log="$tmp/secret-shallow.log"
if run_secret_scan "$shallow_clone" "$test_product_id" "$log"; then
  fail "the secret scan passed in a shallow clone"
fi
assert_not_contains "$log" "$test_product_id" "the shallow scan printed the product id"
pass "the secret scan fails in a shallow clone"

repo=$(make_secret_repo absent)
log="$tmp/secret-absent.log"
if ! run_secret_scan "$repo" "$test_product_id" "$log"; then
  fail "an absent product id failed the secret scan"
fi
assert_not_contains "$log" "$test_product_id" "the absent scan printed the product id"
pass "an absent product id passes the secret scan"

log="$tmp/secret-no-id.log"
if ! run_secret_scan "$repo" "" "$log"; then
  fail "an unavailable product id secret did not skip"
fi
assert_contains "$log" "product id secret not available; skipping" \
  "an unavailable product id secret did not print the skip message"
pass "an unavailable product id secret skips"

repo=$(make_secret_repo working-tree)
log="$tmp/secret-working-tree.log"
printf '%s\n' "$test_product_id" >"$repo/leak.txt"
"${git_env[@]}" git -C "$repo" add leak.txt
if run_secret_scan "$repo" "$test_product_id" "$log"; then
  fail "a product id in the working tree passed the secret scan"
fi
assert_not_contains "$log" "$test_product_id" "the working-tree scan printed the product id"
pass "a product id in the working tree fails the secret scan"

repo=$(make_secret_repo history)
log="$tmp/secret-history.log"
printf '%s\n' "$test_product_id" >"$repo/leak.txt"
"${git_env[@]}" git -C "$repo" add leak.txt
"${git_env[@]}" git -C "$repo" commit -q -m "add leak"
rm "$repo/leak.txt"
"${git_env[@]}" git -C "$repo" add -A
"${git_env[@]}" git -C "$repo" commit -q -m "remove leak"
if run_secret_scan "$repo" "$test_product_id" "$log"; then
  fail "a product id only in history passed the secret scan"
fi
assert_not_contains "$log" "$test_product_id" "the history scan printed the product id"
pass "a product id only in history fails the secret scan"

# --- --validate-only --------------------------------------------------------

run_validate() {
  local log=$1
  shift
  local rc=0
  set +e
  "$@" >"$log" 2>&1
  rc=$?
  set -e
  return "$rc"
}

log="$tmp/validate-valid.log"
if ! run_validate "$log" env FINGLASS_FINTS_PRODUCT_ID="$test_product_id" \
  bash "$package_script" --validate-only; then
  fail "a valid product id was rejected"
fi
assert_contains "$log" "product id: valid" "a valid product id did not print valid"
assert_not_contains "$log" "$test_product_id" "the valid check printed the product id"
pass "--validate-only accepts a valid product id without building"

log="$tmp/validate-invalid-apostrophe.log"
if run_validate "$log" env FINGLASS_FINTS_PRODUCT_ID="bad'id" \
  bash "$package_script" --validate-only; then
  fail "a product id with an apostrophe was accepted"
fi
assert_contains "$log" "product id: invalid" "an apostrophe id did not print invalid"
assert_not_contains "$log" "bad'id" "the apostrophe check printed the product id"
pass "--validate-only rejects bad'id"

log="$tmp/validate-invalid-space.log"
if run_validate "$log" env FINGLASS_FINTS_PRODUCT_ID="with space" \
  bash "$package_script" --validate-only; then
  fail "a product id with a space was accepted"
fi
assert_contains "$log" "product id: invalid" "a spaced id did not print invalid"
assert_not_contains "$log" "with space" "the spaced check printed the product id"
pass "--validate-only rejects a product id with a space"

too_long=$(printf 'A%.0s' {1..31})
log="$tmp/validate-too-long.log"
if run_validate "$log" env FINGLASS_FINTS_PRODUCT_ID="$too_long" \
  bash "$package_script" --validate-only; then
  fail "a 31-character product id was accepted"
fi
assert_contains "$log" "product id: invalid" "a 31-character id did not print invalid"
assert_not_contains "$log" "$too_long" "the too-long check printed the product id"
pass "--validate-only rejects a 31-character product id"

log="$tmp/validate-not-set.log"
if run_validate "$log" env -u FINGLASS_FINTS_PRODUCT_ID \
  bash "$package_script" --validate-only; then
  fail "an unset product id exited 0"
fi
assert_contains "$log" "product id: not set" "an unset product id did not print not set"
pass "--validate-only reports an unset product id and exits non-zero"

# The value only comes from the environment, never a positional argument.
log="$tmp/validate-argument.log"
if run_validate "$log" env -u FINGLASS_FINTS_PRODUCT_ID \
  bash "$package_script" --validate-only "$test_product_id"; then
  fail "a positional product id was accepted"
fi
assert_contains "$log" "product id: not set" "a positional product id was not ignored"
assert_not_contains "$log" "$test_product_id" "the argument check printed the product id"
pass "--validate-only ignores a positional product id"

# --- linker value -----------------------------------------------------------

log="$tmp/linker.log"
if ! FINGLASS_EXPECT_PRODUCT_ID_LINKED=1 \
  go test -ldflags "-X github.com/MaMissaoui/Finglass/internal/core/fintsproduct.productID=$test_product_id" \
  ./internal/core/fintsproduct -run TestLinkerValue -count=1 >"$log" 2>&1; then
  fail "the -ldflags -X value did not reach the fintsproduct package"
fi
assert_not_contains "$log" "$test_product_id" "the linker test printed the product id"
pass "-ldflags -X sets the product id"

# The self-check line and the About DTO with a linked id: present, and never the value.
log="$tmp/linker-surfaces.log"
if ! FINGLASS_EXPECT_PRODUCT_ID_LINKED=1 \
  go test -ldflags "-X github.com/MaMissaoui/Finglass/internal/core/fintsproduct.productID=$test_product_id" \
  ./cmd/finglass ./internal/bind -run 'TestSelfCheckNeverPrintsTheProductID|TestAboutDTONeverPrintsTheProductID' \
  -count=1 -v >"$log" 2>&1; then
  fail "the self-check or the About DTO exposed the linked product id"
fi
if grep -q -- "--- SKIP" "$log"; then
  fail "a linked product id test skipped instead of running"
fi
assert_not_contains "$log" "$test_product_id" "the surface tests printed the product id"
pass "the self-check and the About DTO never show a linked product id"

printf '\n%d product-id cases passed.\n' "$passed"
