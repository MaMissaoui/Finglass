#!/usr/bin/env bash
# Builds the per-user Windows installer (NSIS) from the Go/Wails app. Runs on
# windows-latest in Git Bash. Built but never released: no FinTS product id is
# read or linked. The build writes only inside build/ and frontend/dist/
# (node_modules is npm's own install target inside frontend/). The only network
# access is npm ci, plus Chocolatey only when NSIS is missing from the runner.
#
# Output: build/go/Finglass_<version>_windows_amd64_setup.exe
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

syso="$repo_root/cmd/finglass/finglass_windows_amd64.syso"
tmp_home=""

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  # The .syso is a generated build input, never a checked-in file.
  rm -f "$syso" || true
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

# 3. Icon and application manifest as a Go resource. wails3 requires a manifest
# and accepts the icon; both end up in the EXE. The file is removed on exit.
rm -f "$syso"
resource_status="embedded"
if ! go tool wails3 generate syso \
  -arch amd64 \
  -icon "$(package_native_path "$repo_root/assets/icons/finglass.ico")" \
  -manifest "$(package_native_path "$repo_root/packaging/windows/finglass.exe.manifest")" \
  -out "$(package_native_path "$syso")"; then
  resource_status="skipped (wails3 generate syso failed)"
  rm -f "$syso"
  echo "WARNING: building without the Windows icon and manifest resource." >&2
fi

# 4. Windows GUI binary. No fintsproduct.productID ldflag, ever.
outdir="$build/go/windows"
mkdir -p "$outdir"
ldflags="-s -w -H windowsgui -X github.com/MaMissaoui/Finglass/internal/bind.Version=$version"
export GOOS=windows
export GOARCH=amd64
export CGO_ENABLED=1
go build -trimpath -ldflags "$ldflags" -o "$(package_native_path "$outdir/Finglass.exe")" ./cmd/finglass

# 5. Self-check. It exits before Wails starts, so no display is needed. A
# windowsgui binary does not reliably attach to the console, so redirect the
# output to a file and read it back.
tmp_home="$(mktemp -d "${TMPDIR:-/tmp}/finglass-self-check.XXXXXX")"
check_log="$outdir/self-check.log"
if ! FINGLASS_HOME="$(package_native_path "$tmp_home")" "$outdir/Finglass.exe" --self-check >"$check_log" 2>&1; then
  echo "self-check failed:" >&2
  cat "$check_log" >&2 || true
  exit 1
fi
check_output="$(cat "$check_log")"
package_assert_self_check "$check_output" absent

# 6. NSIS. It ships with windows-latest; install it only when it is missing.
if ! command -v makensis >/dev/null 2>&1; then
  if ! command -v choco >/dev/null 2>&1; then
    echo "makensis is missing and Chocolatey is not available" >&2
    exit 1
  fi
  echo "makensis is missing; installing NSIS with Chocolatey" >&2
  choco install nsis -y --no-progress
  export PATH="$PATH:/c/Program Files (x86)/NSIS:/c/Program Files/NSIS"
fi
if ! command -v makensis >/dev/null 2>&1; then
  echo "makensis is still not on PATH after installing NSIS" >&2
  exit 1
fi

installer="$build/go/Finglass_${version}_windows_amd64_setup.exe"
makensis -V2 \
  -DVERSION="$version" \
  -DSRCDIR="$(package_native_path "$outdir")" \
  -DNOTICES="$(package_native_path "$repo_root/packaging/THIRD-PARTY-NOTICES.txt")" \
  -DICON="$(package_native_path "$repo_root/assets/icons/finglass.ico")" \
  -DOUTFILE="$(package_native_path "$installer")" \
  "$(package_native_path "$repo_root/packaging/windows/finglass.nsi")"

if [[ ! -f "$installer" ]]; then
  echo "makensis did not write $installer" >&2
  exit 1
fi

printf 'EXE: %s\n' "$outdir/Finglass.exe"
printf 'Installer: %s\n' "$installer"
printf 'Windows icon/manifest: %s\n' "$resource_status"
