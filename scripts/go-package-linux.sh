#!/usr/bin/env bash
# Builds the Linux DEB (GTK4/WebKitGTK 6.0) from the Go/Wails app. Runs on
# ubuntu-latest. Built but never released: no FinTS product id is read or
# linked. Writes only inside build/ and frontend/dist/ (node_modules is npm's
# own install target inside frontend/). The only network access is npm ci.
#
# Output: build/go/finglass_<debian-version>_amd64.deb
set -euo pipefail

cd "$(dirname "$0")/.."
repo_root=$PWD
# shellcheck source=lib/package-common.sh
# shellcheck disable=SC1091
. "$repo_root/scripts/lib/package-common.sh"

version="${1:-0.0.0-dev}"
package_validate_version "$version"

# These builds are artifacts, never release assets: never a product id.
unset FINGLASS_FINTS_PRODUCT_ID

package_setup "$repo_root"
build="$PACKAGE_BUILD"

tmp_home=""
cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if [[ -n "$tmp_home" ]]; then
    rm -rf "$tmp_home" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# 1. Frontend.
package_build_frontend "$repo_root"

# 2. Regenerate the bindings and fail when the checked-in files are stale.
package_check_bindings "$repo_root"

# 3. Linux binary. No fintsproduct.productID ldflag, ever.
outdir="$build/go/linux"
mkdir -p "$outdir"
ldflags="-s -w -X github.com/MaMissaoui/Finglass/internal/bind.Version=$version"
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=1
go build -trimpath -ldflags "$ldflags" -o "$outdir/finglass" ./cmd/finglass

# 4. Self-check. It exits before Wails starts, so no display is needed.
tmp_home="$(mktemp -d "${TMPDIR:-/tmp}/finglass-self-check.XXXXXX")"
if ! check_output="$(FINGLASS_HOME="$tmp_home" "$outdir/finglass" --self-check 2>&1)"; then
  echo "self-check failed:" >&2
  printf '%s\n' "$check_output" >&2
  exit 1
fi
package_assert_self_check "$check_output" absent

# 5. Package root. Debian versions sort prereleases before the release, so a
# hyphen in the version becomes a tilde (0.0.0-ci.5 -> 0.0.0~ci.5).
deb_version="${version//-/~}"
deb_root="$build/go/deb"
pkg="$deb_root/finglass"
rm -rf "$deb_root"
install -d -m 0755 \
  "$pkg/DEBIAN" \
  "$pkg/usr/bin" \
  "$pkg/usr/share/applications" \
  "$pkg/usr/share/doc/finglass"

install -m 0755 "$outdir/finglass" "$pkg/usr/bin/finglass"
install -m 0644 "$repo_root/packaging/linux/finglass.desktop" \
  "$pkg/usr/share/applications/io.github.mamissaoui.finglass.desktop"

for size in 16 32 48 64 128 256 512; do
  install -d -m 0755 "$pkg/usr/share/icons/hicolor/${size}x${size}/apps"
  install -m 0644 "$repo_root/assets/icons/finglass-${size}.png" \
    "$pkg/usr/share/icons/hicolor/${size}x${size}/apps/io.github.mamissaoui.finglass.png"
done

{
  printf 'Finglass is licensed under the MIT License.\n\n'
  cat "$repo_root/LICENSE"
  printf '\nThird-party notices\n===================\n\n'
  cat "$repo_root/packaging/THIRD-PARTY-NOTICES.txt"
} >"$pkg/usr/share/doc/finglass/copyright"
chmod 0644 "$pkg/usr/share/doc/finglass/copyright"

installed_size="$(du -sk "$pkg" | cut -f1)"
sed \
  -e "s/__VERSION__/$deb_version/g" \
  -e "s/__INSTALLED_SIZE__/$installed_size/g" \
  "$repo_root/packaging/linux/control" >"$pkg/DEBIAN/control"
chmod 0644 "$pkg/DEBIAN/control"

# 6. Build the DEB. --root-owner-group makes every entry root:root without
# needing a chown; the file modes above are preserved.
deb="$build/go/finglass_${deb_version}_amd64.deb"
rm -f "$deb"
dpkg-deb --build --root-owner-group "$pkg" "$deb"

contents="$(dpkg-deb --contents "$deb")"
if [[ "$contents" != *"./usr/bin/finglass"* ]]; then
  echo "$deb does not contain ./usr/bin/finglass" >&2
  exit 1
fi

if command -v lintian >/dev/null 2>&1; then
  # Informational only: lintian's policy opinions do not gate the artifact.
  lintian "$deb" || true
else
  echo "lintian is not installed; skipping the informational check"
fi

printf 'Binary: %s\n' "$outdir/finglass"
printf 'DEB: %s\n' "$deb"
