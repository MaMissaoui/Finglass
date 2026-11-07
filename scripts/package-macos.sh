#!/usr/bin/env bash
# Builds Finglass.app and the DMG with cargo-packager, then ad-hoc signs and verifies the app.
# Writes only under target/. Never installs anything and never runs the app.
set -euo pipefail

cd "$(dirname "$0")/.."

out=target/packager
app="$out/Finglass.app"
packager_version=0.11.8

if ! cargo packager --version >/dev/null 2>&1; then
  echo "cargo-packager not found; install it with: cargo install cargo-packager --version $packager_version --locked" >&2
  exit 1
fi

# The [package.metadata.packager] table lives in crates/finglass/Cargo.toml. --release makes
# cargo-packager look in target/release, which the configured hook builds.
# cargo-packager caches its create-dmg helper under $HOME/Library/Caches; point HOME at target for
# this command so the whole run stays inside the repository, while keeping the real Cargo and
# rustup homes so the toolchain and registry are used as usual.
cache_home="$PWD/target/.packager-home"
mkdir -p "$cache_home"
real_cargo_home="${CARGO_HOME:-$HOME/.cargo}"
real_rustup_home="${RUSTUP_HOME:-$HOME/.rustup}"
HOME="$cache_home" CARGO_HOME="$real_cargo_home" RUSTUP_HOME="$real_rustup_home" \
  cargo packager --release --packages finglass

if [ ! -d "$app" ]; then
  echo "expected $app after packaging" >&2
  exit 1
fi

# The config signs with the ad-hoc identity "-", so this is normally a no-op. It also covers a
# tool run that produced an unsigned bundle.
if ! codesign --verify --deep --strict "$app" >/dev/null 2>&1; then
  codesign --force --deep --sign - "$app"
fi
codesign --verify --deep --strict "$app"

dmg=$(find "$out" -maxdepth 1 -type f -name 'Finglass_*.dmg' -print -quit)
if [ -z "$dmg" ]; then
  echo "no Finglass DMG in $out" >&2
  exit 1
fi

echo "App: $app"
echo "DMG: $dmg"
