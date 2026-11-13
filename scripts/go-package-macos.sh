#!/usr/bin/env bash
# Builds an ad-hoc-signed universal Finglass.app and a universal DMG from the
# Go/Wails app. Writes only inside build/ and frontend/dist/ (node_modules is
# npm's own install target inside frontend/). The only network access is npm ci.
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:-0.4.0-dev}"
if [[ ! "$version" =~ ^[0-9A-Za-z._-]+$ ]]; then
  echo "invalid version: $version" >&2
  exit 1
fi

root="$PWD"
build="$root/build"
cache="$build/go/.cache"

mkdir -p "$build/go" "$cache/tmp" "$cache/npm" "$cache/go"

# Keep tool caches and temporary files inside build/go/.
export GOCACHE="$cache/go"
export GOTMPDIR="$cache/tmp"
export GOPROXY=off
export GOTOOLCHAIN=local
export GOTELEMETRY=off
export npm_config_cache="$cache/npm"

# 1. Frontend.
(
  cd frontend
  npm ci --no-audit --no-fund
  npm run build
)

# 2. Regenerate the bindings and fail when the checked-in files are stale.
go tool wails3 generate bindings -ts ./cmd/finglass
if ! git diff --exit-code -- frontend/bindings; then
  echo "frontend/bindings is stale; commit the regenerated bindings" >&2
  exit 1
fi

# 3. Universal Go binary.
ldflags="-s -w -X github.com/MaMissaoui/Finglass/internal/bind.Version=$version"
export GOOS=darwin
export CGO_ENABLED=1
# 13.0, not the Wails template's 12.0: Go 1.27's runtime objects are built for macOS 13 (the linker warns otherwise).
deployment_target=13.0
export MACOSX_DEPLOYMENT_TARGET="$deployment_target"
export CGO_CFLAGS="-mmacosx-version-min=$deployment_target"
export CGO_LDFLAGS="-mmacosx-version-min=$deployment_target"

for arch in arm64 amd64; do
  GOARCH="$arch" go build -trimpath -ldflags "$ldflags" \
    -o "$build/go/finglass-$arch" ./cmd/finglass
done

lipo -create \
  "$build/go/finglass-arm64" \
  "$build/go/finglass-amd64" \
  -output "$build/go/Finglass"

# 4. Hand-made app bundle.
app="$build/go/Finglass.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

install -m 0755 "$build/go/Finglass" "$app/Contents/MacOS/Finglass"
install -m 0644 assets/icons/finglass.icns "$app/Contents/Resources/finglass.icns"
sed "s/__VERSION__/$version/g" packaging/macos/Info.plist > "$app/Contents/Info.plist"

# 5. Ad-hoc sign and verify.
codesign --force --deep -s - "$app"
codesign --verify --deep --strict "$app"

# The bundle's minimum macOS version must match the deployment target above, so
# the two cannot drift apart.
minimum="$(plutil -extract LSMinimumSystemVersion raw -o - "$app/Contents/Info.plist")"
if [[ "$minimum" != "$deployment_target" ]]; then
  echo "LSMinimumSystemVersion is $minimum, expected $deployment_target" >&2
  exit 1
fi

# 6. Self-check and architecture check.
tmp_home=""
dmg_stage=""
dmg_mount=""
dmg_attached=0

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if [[ "$dmg_attached" -eq 1 && -n "$dmg_mount" ]]; then
    hdiutil detach "$dmg_mount" >/dev/null 2>&1 || true
  fi
  if [[ -n "$dmg_stage" ]]; then
    rm -rf "$dmg_stage" || true
  fi
  if [[ -n "$dmg_mount" ]]; then
    rm -rf "$dmg_mount" || true
  fi
  if [[ -n "$tmp_home" ]]; then
    rm -rf "$tmp_home" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

tmp_home="$(mktemp -d "${TMPDIR:-/tmp}/finglass-self-check.XXXXXX")"

check_output="$(FINGLASS_HOME="$tmp_home" "$app/Contents/MacOS/Finglass" --self-check)"
printf '%s\n' "$check_output"
if [[ "$check_output" != OK:* ]]; then
  echo "self-check failed" >&2
  exit 1
fi

archs="$(lipo -archs "$app/Contents/MacOS/Finglass")"
if [[ "$archs" != *arm64* || "$archs" != *x86_64* ]]; then
  echo "expected a universal binary, got: $archs" >&2
  exit 1
fi

# 7. DMG with the app and the usual /Applications shortcut.
dmg="$build/go/Finglass_${version}_universal.dmg"
dmg_stage="$(mktemp -d "$build/go/.dmg-stage.XXXXXX")"
dmg_mount="$(mktemp -d "$build/go/.dmg-mount.XXXXXX")"

ditto "$app" "$dmg_stage/Finglass.app"
ln -s /Applications "$dmg_stage/Applications"

rm -f "$dmg"
hdiutil create -volname Finglass -fs HFS+ -format UDZO -srcfolder "$dmg_stage" "$dmg"

# 8. Mount the result read-only and verify what a user would get.
if ! hdiutil attach -nobrowse -readonly -noautoopen -mountpoint "$dmg_mount" "$dmg" >/dev/null; then
  echo "could not mount $dmg" >&2
  exit 1
fi
dmg_attached=1

mounted_app="$dmg_mount/Finglass.app"
if [[ ! -d "$mounted_app" ]]; then
  echo "$dmg does not contain Finglass.app" >&2
  exit 1
fi
codesign --verify --deep --strict "$mounted_app"

mounted_version="$(plutil -extract CFBundleShortVersionString raw -o - "$mounted_app/Contents/Info.plist")"
if [[ "$mounted_version" != "$version" ]]; then
  echo "$dmg contains Finglass $mounted_version, expected $version" >&2
  exit 1
fi

hdiutil detach "$dmg_mount" >/dev/null
dmg_attached=0

printf 'App: %s\n' "$app"
printf 'DMG: %s\n' "$dmg"
printf 'Architectures: %s\n' "$archs"
