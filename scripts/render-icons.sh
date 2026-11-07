#!/usr/bin/env bash
# Regenerates every icon file in assets/icons from the SVG sources in assets/brand.
# Needs rsvg-convert (librsvg) and ImageMagick 7 (`magick`); iconutil (macOS only) for the .icns.
set -euo pipefail

cd "$(dirname "$0")/.."
brand=assets/brand
out=assets/icons
mkdir -p "$out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

png() { rsvg-convert -w "$2" -h "$2" "$brand/$1" -o "$3"; }

# Plain PNGs: the simplified mark up to 48 px, the full icon above.
for s in 16 32 48; do png finglass-favicon.svg "$s" "$out/finglass-$s.png"; done
for s in 64 128 256 512 1024; do png finglass-icon.svg "$s" "$out/finglass-$s.png"; done

# Web favicons.
cp "$out/finglass-16.png" "$out/favicon-16x16.png"
cp "$out/finglass-32.png" "$out/favicon-32x32.png"
magick "$out/finglass-16.png" "$out/finglass-32.png" "$out/finglass-48.png" "$out/favicon.ico"

# Windows: one .ico with all the sizes Explorer and the taskbar ask for.
png finglass-favicon.svg 24 "$tmp/24.png"
magick "$out/finglass-16.png" "$tmp/24.png" "$out/finglass-32.png" "$out/finglass-48.png" \
  "$out/finglass-64.png" "$out/finglass-128.png" "$out/finglass-256.png" "$out/finglass.ico"

# macOS: the padded variant on Apple's icon grid.
if command -v iconutil >/dev/null; then
  set_dir="$tmp/finglass.iconset"
  mkdir -p "$set_dir"
  for s in 16 32 128 256 512; do
    png finglass-icon-macos.svg "$s" "$set_dir/icon_${s}x${s}.png"
    png finglass-icon-macos.svg $((s * 2)) "$set_dir/icon_${s}x${s}@2x.png"
  done
  iconutil -c icns "$set_dir" -o "$out/finglass.icns"
else
  echo "iconutil not found: finglass.icns left as it is" >&2
fi

echo "Icons written to $out"
