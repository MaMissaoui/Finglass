#!/usr/bin/env bash
# Launches the packaged app once from its DMG, with a fresh temporary
# FINGLASS_HOME, and checks that it stays alive and writes only its instance
# lock. This is the non-tag part of package.yml's launch step, usable from CI's
# Package job.
#
# Usage: scripts/ci/launch-check.sh [dmg]
# The DMG defaults to build/go/Finglass_0.4.0-dev_universal.dmg.
set -euo pipefail
cd "$(dirname "$0")/../.."

dmg="${1:-build/go/Finglass_0.4.0-dev_universal.dmg}"
if [ ! -f "$dmg" ]; then
  echo "no DMG at $dmg" >&2
  exit 1
fi

mount_point=$(mktemp -d)
copy_dir=$(mktemp -d)
home=$(mktemp -d)

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  hdiutil detach "$mount_point" >/dev/null 2>&1 || true
  rm -rf "$mount_point" "$copy_dir" "$home"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

hdiutil attach -nobrowse -readonly -noautoopen -mountpoint "$mount_point" "$dmg" >/dev/null
echo "Mounted $dmg at $mount_point"

ditto "$mount_point/Finglass.app" "$copy_dir/Finglass.app"

log="$copy_dir/finglass-launch.log"
FINGLASS_HOME="$home" "$copy_dir/Finglass.app/Contents/MacOS/Finglass" >"$log" 2>&1 &
pid=$!

sleep 10
if ! kill -0 "$pid" 2>/dev/null; then
  echo "the app exited within 10 seconds" >&2
  cat "$log" 2>/dev/null || true
  wait "$pid" || true
  exit 1
fi
echo "The app is still running after 10 seconds; stopping it."
kill "$pid"
wait "$pid" || true

if [ ! -d "$home" ]; then
  echo "FINGLASS_HOME was not created" >&2
  exit 1
fi
# Until the user creates or unlocks a profile, the unlock window only reads
# this folder; the instance lock creates finglass.lock and nothing else.
entries=$(ls -A "$home")
if [ "$entries" != "finglass.lock" ]; then
  echo "expected only finglass.lock in FINGLASS_HOME:" >&2
  ls -la "$home" >&2
  exit 1
fi
echo "Launch check passed; FINGLASS_HOME holds only the instance lock."
