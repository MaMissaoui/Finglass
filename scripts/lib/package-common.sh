#!/usr/bin/env bash
# Shared steps for the Windows and Linux packagers, mirrored from
# scripts/go-package-macos.sh: version validation, the build cache layout, the
# frontend build and the bindings staleness check. Sourced, never executed.
#
# The macOS script keeps its own product-id flow because only a release tag may
# link the FinTS product id. The Windows and Linux builds are artifacts, never
# release assets, so they never pass it.

package_validate_version() {
  local version=$1
  if [[ ! "$version" =~ ^[0-9A-Za-z._-]+$ ]]; then
    echo "invalid version: $version" >&2
    return 1
  fi
}

# package_native_path <path> prints the path in the form native tools expect.
# On Windows (Git Bash), environment variables are not converted for Go or npm,
# so a POSIX $PWD path must become a drive path.
package_native_path() {
  local path=$1
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) cygpath -w "$path" ;;
    *) printf '%s\n' "$path" ;;
  esac
}

package_setup() {
  local root=$1
  PACKAGE_BUILD="$root/build"
  local cache="$PACKAGE_BUILD/go/.cache"

  mkdir -p "$PACKAGE_BUILD/go" "$cache/go" "$cache/tmp" "$cache/npm"

  export GOPROXY=off
  export GOTOOLCHAIN=local
  export GOTELEMETRY=off

  # Keep tool caches and temporary files inside build/go/. CI can point the Go
  # build cache at a restored folder so it survives the build.
  local go_cache="${FINGLASS_PACKAGE_GOCACHE:-$cache/go}"
  local native_go_cache native_gotmpdir native_npm_cache
  native_go_cache="$(package_native_path "$go_cache")"
  native_gotmpdir="$(package_native_path "$cache/tmp")"
  native_npm_cache="$(package_native_path "$cache/npm")"
  export GOCACHE="$native_go_cache"
  export GOTMPDIR="$native_gotmpdir"
  export npm_config_cache="$native_npm_cache"

  # The default folder above already exists. A CI-provided cache is restored
  # by the caller; create it only when it is a POSIX path this shell can make.
  if [[ -n "${FINGLASS_PACKAGE_GOCACHE:-}" && "$FINGLASS_PACKAGE_GOCACHE" == /* ]]; then
    mkdir -p "$FINGLASS_PACKAGE_GOCACHE"
  fi
}

package_build_frontend() {
  local root=$1
  (
    cd "$root/frontend" || exit 1
    npm ci --no-audit --no-fund
    npm run build
  )
}

package_check_bindings() {
  local root=$1
  (cd "$root" && go tool wails3 generate bindings -ts ./cmd/finglass)
  if ! (cd "$root" && git diff --exit-code -- frontend/bindings); then
    echo "frontend/bindings is stale; commit the regenerated bindings" >&2
    return 1
  fi
}

# package_assert_self_check <output> <absent|present>
package_assert_self_check() {
  local output=$1
  local expected_product=$2

  printf '%s\n' "$output"
  if [[ "$output" != OK:* ]]; then
    echo "self-check failed" >&2
    return 1
  fi

  case "$expected_product" in
    absent)
      if [[ "$output" != *"FinTS product id: absent"* ]]; then
        echo "self-check did not report the FinTS product id as absent" >&2
        return 1
      fi
      ;;
    present)
      if [[ "$output" != *"FinTS product id: present"* ]]; then
        echo "self-check did not report the FinTS product id as present" >&2
        return 1
      fi
      ;;
    *)
      echo "unknown product id expectation: $expected_product" >&2
      return 1
      ;;
  esac
}
