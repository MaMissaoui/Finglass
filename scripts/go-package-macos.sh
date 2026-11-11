#!/usr/bin/env bash
# Builds an ad-hoc-signed universal Finglass.app from the Go/Wails app.
# Writes only inside build/ and frontend/dist/ (node_modules is npm's own
# install target inside frontend/). The only network access is npm ci.
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
export MACOSX_DEPLOYMENT_TARGET=13.0
export CGO_CFLAGS="-mmacosx-version-min=13.0"
export CGO_LDFLAGS="-mmacosx-version-min=13.0"

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

# 6. Self-check and architecture check.
tmp_home="$(mktemp -d)"
trap 'rm -rf "$tmp_home"' EXIT

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

printf 'App: %s\n' "$app"
printf 'Architectures: %s\n' "$archs"
