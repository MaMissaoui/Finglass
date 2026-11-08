#!/usr/bin/env bash
# Tests scripts/install-mac.sh without network access or a real /Applications folder.
# Builds fixture apps, DMGs and release JSON under mktemp -d, and throwaway git repositories
# for the --from-source checks. Runs on macOS only.
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd)
install_script="$repo_root/scripts/install-mac.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/finglass-install-test.XXXXXX")

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
  if [ -n "${log:-}" ] && [ -f "$log" ]; then
    printf '%s\n' '--- installer output ---' >&2
    sed 's/^/    /' "$log" >&2 || true
    printf '%s\n' '--- end installer output ---' >&2
  fi
  exit 1
}

assert_eq() {
  [ "$1" = "$2" ] || fail "${3}: expected '$2', got '$1'"
}

assert_grep() {
  grep -q -- "$1" "$2" || fail "${3}: missing '$1' in $2"
}

assert_no_app() {
  [ ! -e "$1/Finglass.app" ] || fail "expected no Finglass.app in $1"
}

assert_trash_empty() {
  [ -z "$(ls -A "$1")" ] || fail "expected the trash folder $1 to stay empty"
}

app_version() {
  defaults read "$1/Contents/Info.plist" CFBundleShortVersionString
}

make_app() {
  local app=$1 version=$2
  rm -rf "$app"
  mkdir -p "$app/Contents/MacOS"
  cat >"$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>dev.finglass.installer-test</string>
  <key>CFBundleName</key><string>Finglass</string>
  <key>CFBundleDisplayName</key><string>Finglass</string>
  <key>CFBundleExecutable</key><string>finglass</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
</dict>
</plist>
EOF
  cat >"$app/Contents/MacOS/finglass" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "$app/Contents/MacOS/finglass"
  codesign --force --deep --sign - "$app" >/dev/null 2>&1 ||
    fail "could not ad-hoc sign the fixture app $app"
  codesign --verify --deep --strict "$app" >/dev/null 2>&1 ||
    fail "the fixture app $app does not verify"
}

make_dmg() {
  local src=$1 dmg=$2
  rm -f "$dmg"
  hdiutil create -quiet -srcfolder "$src" -format UDZO -volname Finglass "$dmg" >/dev/null ||
    fail "could not create the fixture DMG $dmg"
}

fixtures="$tmp/fixtures"
mkdir -p "$fixtures"

case "$(uname -m)" in
  arm64) host_arch=aarch64 other_arch=x86_64 ;;
  x86_64) host_arch=x86_64 other_arch=aarch64 ;;
  *) fail "unsupported test architecture $(uname -m)" ;;
esac

# make_release <tag> <arch> <app version> [bad-checksum]
make_release() {
  local tag=$1 arch=$2 app_version_value=$3 mode=${4:-ok}
  local version=${tag#v}
  local dir="$fixtures/$tag"
  local src="$dir/src"
  local dmg="$dir/Finglass_${version}_${arch}.dmg"
  rm -rf "$dir"
  mkdir -p "$src"
  make_app "$src/Finglass.app" "$app_version_value"
  make_dmg "$src" "$dmg"
  if [ "$mode" = bad-checksum ]; then
    printf '%s  %s\n' \
      '0000000000000000000000000000000000000000000000000000000000000000' \
      "Finglass_${version}_${arch}.dmg" >"$dir/SHA256SUMS.txt"
  else
    (cd "$dir" && shasum -a 256 "Finglass_${version}_${arch}.dmg" >SHA256SUMS.txt)
  fi
}

make_release v0.1.0 "$other_arch" 0.1.0
make_release v0.1.1 "$host_arch" 0.1.1 bad-checksum
make_release v0.1.2 "$host_arch" 9.9.9
make_release v0.2.1 "$host_arch" 0.2.1
make_release v0.2.2 "$host_arch" 0.2.2

releases_json="$tmp/releases.json"

emit_release() {
  local tag=$1 arch=$2 version=${1#v}
  printf '{"tag_name":"%s","draft":false,"prerelease":true,"assets":[' "$tag"
  printf '{"name":"Finglass_%s_%s.dmg","browser_download_url":"file://%s/%s/Finglass_%s_%s.dmg"},' \
    "$version" "$arch" "$fixtures" "$tag" "$version" "$arch"
  printf '{"name":"SHA256SUMS.txt","browser_download_url":"file://%s/%s/SHA256SUMS.txt"}' \
    "$fixtures" "$tag"
  printf ']}'
}

{
  # A draft with a higher tag must be ignored; the releases are deliberately not in tag order.
  printf '[{"tag_name":"v9.9.9","draft":true,"prerelease":true,"assets":[]},'
  emit_release v0.2.2 "$host_arch"
  printf ','
  emit_release v0.1.0 "$other_arch"
  printf ','
  emit_release v0.2.1 "$host_arch"
  printf ','
  emit_release v0.1.1 "$host_arch"
  printf ','
  emit_release v0.1.2 "$host_arch"
  printf ']\n'
} >"$releases_json"

run_install() {
  local log_file=$1 trash_dir=$2
  shift 2
  local rc=0
  set +e
  FINGLASS_RELEASES_API="file://$releases_json" \
    FINGLASS_INSTALL_TRASH="$trash_dir" \
    "$install_script" "$@" >"$log_file" 2>&1
  rc=$?
  set -e
  return "$rc"
}

new_case() {
  local name=$1
  apps="$tmp/cases/$name/apps"
  trash="$tmp/cases/$name/trash"
  log="$tmp/cases/$name/install.log"
  mkdir -p "$apps" "$trash"
}

count_replaced_apps() {
  local dir=$1 candidate count=0
  for candidate in "$dir"/Finglass\ *\ \(replaced\ *\).app; do
    [ -e "$candidate" ] || continue
    count=$((count + 1))
    printf '%s\n' "$candidate"
  done | wc -l | tr -d ' '
}

# --- release installs -------------------------------------------------------

new_case fresh
if ! run_install "$log" "$trash" --install-dir "$apps"; then
  fail "fresh install failed"
fi
assert_grep 'Installed Finglass 0.2.2 to' "$log" "fresh install message"
assert_eq "$(app_version "$apps/Finglass.app")" "0.2.2" "fresh install version"
assert_trash_empty "$trash"
pass "fresh release install picks the newest published release"

new_case same
if ! run_install "$log" "$trash" --install-dir "$apps"; then
  fail "first install for the already-installed case failed"
fi
if ! run_install "$log" "$trash" --install-dir "$apps"; then
  fail "second install of the same version failed"
fi
assert_grep 'Finglass 0.2.2 is already installed.' "$log" "already-installed message"
assert_eq "$(app_version "$apps/Finglass.app")" "0.2.2" "version after already-installed"
assert_trash_empty "$trash"
pass "same version again says already installed and moves nothing"

new_case newer
if ! run_install "$log" "$trash" --install-dir "$apps" --version v0.2.1; then
  fail "installing the older release for the newer-version case failed"
fi
assert_eq "$(app_version "$apps/Finglass.app")" "0.2.1" "older install version"
if ! run_install "$log" "$trash" --install-dir "$apps"; then
  fail "upgrade install failed"
fi
assert_eq "$(app_version "$apps/Finglass.app")" "0.2.2" "upgraded version"
assert_eq "$(count_replaced_apps "$trash")" "1" "replaced apps in the test trash"
trashed_app=""
for candidate in "$trash"/Finglass\ *\ \(replaced\ *\).app; do
  [ -e "$candidate" ] || continue
  trashed_app=$candidate
done
[ -n "$trashed_app" ] || fail "expected a replaced app in $trash"
assert_eq "$(app_version "$trashed_app")" "0.2.1" "version of the app moved to the trash"
pass "newer release moves the old app to the test trash"

new_case version
if ! run_install "$log" "$trash" --install-dir "$apps" --version 0.2.1; then
  fail "installing an explicit older tag failed"
fi
assert_eq "$(app_version "$apps/Finglass.app")" "0.2.1" "explicit older tag version"
assert_grep 'Installed Finglass 0.2.1 to' "$log" "explicit older tag message"
pass "--version picks the named tag without the leading v"

new_case checksum
printf 'keep\n' >"$apps/keep.txt"
if run_install "$log" "$trash" --install-dir "$apps" --version v0.1.1; then
  fail "a checksum mismatch unexpectedly installed"
fi
assert_grep 'checksum mismatch' "$log" "checksum mismatch message"
assert_grep 'nothing was changed' "$log" "checksum mismatch change message"
assert_no_app "$apps"
assert_eq "$(cat "$apps/keep.txt")" "keep" "checksum case left the install folder alone"
assert_trash_empty "$trash"
pass "checksum mismatch exits 1 and leaves the install folder unchanged"

new_case no-downgrade
make_app "$apps/Finglass.app" 9.0.0
if ! run_install "$log" "$trash" --install-dir "$apps"; then
  fail "the no-downgrade case did not exit 0"
fi
assert_grep 'newer than the latest release 0.2.2' "$log" "no-downgrade message"
assert_eq "$(app_version "$apps/Finglass.app")" "9.0.0" "a newer installed app is kept"
assert_trash_empty "$trash"
pass "a newer installed app is not replaced by an older latest release"

new_case missing-arch
if run_install "$log" "$trash" --install-dir "$apps" --version v0.1.0; then
  fail "a release without this Mac's asset unexpectedly installed"
fi
assert_grep 'v0.1.0' "$log" "missing-arch message names the release"
assert_grep 'Macs' "$log" "missing-arch message names the architecture"
assert_grep 'nothing was changed' "$log" "missing-arch change message"
assert_no_app "$apps"
pass "missing architecture asset exits 1"

new_case wrong-version
if run_install "$log" "$trash" --install-dir "$apps" --version v0.1.2; then
  fail "a DMG with the wrong app version unexpectedly installed"
fi
assert_grep '9.9.9' "$log" "wrong-version message names the app version"
assert_grep '0.1.2' "$log" "wrong-version message names the release version"
assert_grep 'nothing was changed' "$log" "wrong-version change message"
assert_no_app "$apps"
assert_trash_empty "$trash"
pass "a DMG whose app version differs from the tag exits 1"

new_case check-only
if ! run_install "$log" "$trash" --install-dir "$apps" --check-only; then
  fail "--check-only failed"
fi
assert_grep 'Check passed' "$log" "check-only message"
assert_grep 'nothing was installed' "$log" "check-only change message"
assert_no_app "$apps"
assert_trash_empty "$trash"
pass "--check-only verifies everything and installs nothing"

# --- source checks ----------------------------------------------------------

source_root="$tmp/source"
mkdir -p "$source_root"
git_env=(env GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_TERMINAL_PROMPT=0)

setup_source_repo() {
  local name=$1
  local base="$source_root/$name"
  local bare="$base/origin.git"
  local seed="$base/seed"
  local clone="$base/clone"
  mkdir -p "$base"
  "${git_env[@]}" git init --bare -q "$bare"
  "${git_env[@]}" git init -q -b main "$seed"
  "${git_env[@]}" git -C "$seed" config user.name "Installer Test"
  "${git_env[@]}" git -C "$seed" config user.email "installer@example.invalid"
  mkdir -p "$seed/scripts"
  cp "$install_script" "$seed/scripts/install-mac.sh"
  cp "$repo_root/scripts/package-macos.sh" "$seed/scripts/package-macos.sh"
  printf 'initial\n' >"$seed/README.txt"
  "${git_env[@]}" git -C "$seed" add .
  "${git_env[@]}" git -C "$seed" commit -q -m initial
  "${git_env[@]}" git -C "$seed" remote add origin "$bare"
  "${git_env[@]}" git -C "$seed" push -q -u origin main
  "${git_env[@]}" git clone -q "$bare" "$clone"
  "${git_env[@]}" git -C "$clone" config user.name "Installer Test"
  "${git_env[@]}" git -C "$clone" config user.email "installer@example.invalid"
}

source_clone() {
  printf '%s\n' "$source_root/$1/clone"
}

push_seed_file() {
  local name=$1 file=$2
  local seed="$source_root/$name/seed"
  printf '%s\n' "$file" >"$seed/$file"
  "${git_env[@]}" git -C "$seed" add "$file"
  "${git_env[@]}" git -C "$seed" commit -q -m "remote $file"
  "${git_env[@]}" git -C "$seed" push -q origin main
}

commit_clone_file() {
  local name=$1 file=$2
  local clone
  clone=$(source_clone "$name")
  printf '%s\n' "$file" >"$clone/$file"
  "${git_env[@]}" git -C "$clone" add "$file"
  "${git_env[@]}" git -C "$clone" commit -q -m "local $file"
}

run_source_check() {
  local log_file=$1 clone=$2
  shift 2
  local rc=0
  set +e
  "${git_env[@]}" "$clone/scripts/install-mac.sh" --from-source --check-only "$@" \
    >"$log_file" 2>&1
  rc=$?
  set -e
  return "$rc"
}

new_source_case() {
  local name=$1
  log="$tmp/source/$name/install.log"
  mkdir -p "$tmp/source/$name"
}

new_source_case behind
setup_source_repo behind
push_seed_file behind updated.txt
clone=$(source_clone behind)
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if ! run_source_check "$log" "$clone"; then
  fail "clean main behind origin was refused"
fi
assert_grep 'Updated main from' "$log" "fast-forward message"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" \
  "$("${git_env[@]}" git -C "$clone" rev-parse origin/main)" "fast-forwarded HEAD"
[ -f "$clone/updated.txt" ] || fail "fast-forward did not bring in the new file"
[ "$head_before" != "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" ] ||
  fail "fast-forward did not move HEAD"
pass "clean main behind origin fast-forwards from origin/main"

new_source_case diverged
setup_source_repo diverged
commit_clone_file diverged local.txt
push_seed_file diverged remote.txt
clone=$(source_clone diverged)
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if run_source_check "$log" "$clone"; then
  fail "a diverged main was accepted"
fi
assert_grep 'not a release or main' "$log" "diverged refusal"
assert_grep 'git switch main && git pull --ff-only' "$log" "diverged refusal hint"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" "$head_before" \
  "diverged HEAD after refusal"
[ ! -f "$clone/remote.txt" ] || fail "diverged repository was merged"
pass "diverged main is refused and left unchanged"

new_source_case dirty
setup_source_repo dirty
push_seed_file dirty updated.txt
clone=$(source_clone dirty)
printf 'dirty\n' >"$clone/scripts/dirty.txt"
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if run_source_check "$log" "$clone"; then
  fail "a dirty checkout was accepted"
fi
assert_grep 'uncommitted changes' "$log" "dirty refusal"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" "$head_before" \
  "dirty HEAD after refusal"
[ ! -f "$clone/updated.txt" ] || fail "dirty repository was updated"
pass "dirty tree is refused and left unchanged"

new_source_case tagged
setup_source_repo tagged
clone=$(source_clone tagged)
"${git_env[@]}" git -C "$clone" tag v1.0.0
push_seed_file tagged remote.txt
"${git_env[@]}" git -C "$clone" checkout -q v1.0.0
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if ! run_source_check "$log" "$clone"; then
  fail "a checked-out release tag was refused"
fi
assert_grep 'release v1.0.0' "$log" "tagged acceptance message"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" "$head_before" \
  "tagged HEAD after check"
[ ! -f "$clone/remote.txt" ] || fail "tagged repository was updated"
pass "a detached release tag is accepted and not updated"

new_source_case no-update
setup_source_repo no-update
push_seed_file no-update updated.txt
clone=$(source_clone no-update)
"${git_env[@]}" git -C "$clone" fetch -q origin
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if run_source_check "$log" "$clone" --no-update; then
  fail "--no-update still updated or accepted a behind checkout"
fi
assert_grep 'not a release or main' "$log" "no-update refusal"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" "$head_before" \
  "--no-update HEAD after refusal"
[ ! -f "$clone/updated.txt" ] || fail "--no-update updated the checkout"
pass "--no-update refuses a behind main and leaves it unchanged"

new_source_case unreachable
setup_source_repo unreachable
clone=$(source_clone unreachable)
"${git_env[@]}" git -C "$clone" remote set-url origin "$source_root/unreachable/missing.git"
head_before=$("${git_env[@]}" git -C "$clone" rev-parse HEAD)
if ! run_source_check "$log" "$clone"; then
  fail "an unreachable origin did not fall back to the local refs"
fi
assert_grep 'Warning: could not fetch origin/main' "$log" "unreachable-origin warning"
assert_eq "$("${git_env[@]}" git -C "$clone" rev-parse HEAD)" "$head_before" \
  "unreachable-origin HEAD after check"
pass "unreachable origin warns and decides from the local refs"

printf '\n%d installer cases passed.\n' "$passed"
