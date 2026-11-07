#!/usr/bin/env bash
# Builds Finglass.app from this checkout and installs it into an Applications folder.
#
# Usage: scripts/install-mac.sh [--install-dir <folder>] [--force]
#
#   --install-dir <folder>  Where Finglass.app goes (default: /Applications).
#   --force                 Install even when this checkout has uncommitted changes or its
#                           HEAD is neither a v* release tag nor the commit of origin/main.
#
# The source check runs before anything is built. The build uses scripts/package-macos.sh.
# A previous Finglass.app is moved to the Trash, never deleted. Quit Finglass first.
#
# Environment:
#   FINGLASS_INSTALL_TRASH  Folder used instead of ~/.Trash when replacing an existing app.
#                           Meant for CI; leave it unset for a normal install.
#
# macOS only. Writes the build under target/ and the app into --install-dir.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/install-mac.sh [--install-dir <folder>] [--force]

  --install-dir <folder>  Where Finglass.app goes (default: /Applications).
  --force                 Install even when the checkout is dirty or HEAD is not a
                          v* release tag or the fetched origin/main.

A previous Finglass.app is moved to the Trash, never deleted. Quit Finglass first.
EOF
}

repo_root=$(cd "$(dirname "$0")/.." && pwd)
install_dir=/Applications
force=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      if [ "$#" -lt 2 ] || [ -z "$2" ]; then
        echo "error: --install-dir needs a folder" >&2
        usage >&2
        exit 2
      fi
      install_dir=$2
      shift 2
      ;;
    --install-dir=*)
      install_dir=${1#*=}
      if [ -z "$install_dir" ]; then
        echo "error: --install-dir needs a folder" >&2
        usage >&2
        exit 2
      fi
      shift
      ;;
    --force)
      force=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ "$(uname -s)" != "Darwin" ]; then
  echo "error: install-mac.sh builds and installs a macOS app and only runs on macOS." >&2
  exit 1
fi

# A relative --install-dir is relative to the folder the script was started from.
case "$install_dir" in
  /*) ;;
  *) install_dir="$PWD/$install_dir" ;;
esac
# Drop trailing slashes so the running-app check compares the same spelling ps prints.
while [ "${#install_dir}" -gt 1 ] && [ "${install_dir%/}" != "$install_dir" ]; do
  install_dir=${install_dir%/}
done

# Build inputs whose untracked files matter (tracked changes anywhere count, like FinDesk).
is_build_input() {
  case "$1" in
    crates/* | assets/* | scripts/* | Cargo.toml | Cargo.lock | rust-toolchain*) return 0 ;;
    *) return 1 ;;
  esac
}

# Refuse a work-in-progress checkout before spending minutes on a build. Mirrors FinDesk's
# checkInstallSource: another checkout may still be built with --force.
check_source() {
  if ! git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Warning: not a git checkout (or git is not installed), so Finglass cannot tell whether this is a released build." >&2
    return 0
  fi

  local entry code path i joined
  local -a changed=()
  # -z: paths come unquoted and NUL-separated; a rename or copy is followed by its old path.
  while IFS= read -r -d '' entry; do
    [ -n "$entry" ] || continue
    code=${entry:0:2}
    path=${entry:3}
    case "$code" in
      R* | C*) IFS= read -r -d '' _ || true ;;
    esac
    # Untracked files only count inside the build inputs: a stray download in the repository
    # folder is never packaged, but a new file under crates/ or scripts/ would be.
    if [ "$code" = "??" ] && ! is_build_input "$path"; then
      continue
    fi
    changed+=("$path")
  done < <(git -C "$repo_root" status --porcelain=v1 -z)

  if [ "${#changed[@]}" -gt 0 ]; then
    joined=""
    for ((i = 0; i < ${#changed[@]} && i < 5; i++)); do
      if [ "$i" -gt 0 ]; then
        joined+=", "
      fi
      joined+="${changed[i]}"
    done
    if [ "$force" -eq 1 ]; then
      echo "Warning: this checkout has uncommitted changes (${joined}); installing anyway because --force is set." >&2
    else
      echo "This checkout has uncommitted changes (${joined}). Install from a clean checkout, or pass --force to install it anyway." >&2
      exit 1
    fi
  fi

  local head_sha head_short branch tag origin_main source
  head_sha=$(git -C "$repo_root" rev-parse HEAD)
  head_short=$(git -C "$repo_root" rev-parse --short HEAD)
  branch=$(git -C "$repo_root" rev-parse --abbrev-ref HEAD)
  if [ "$branch" = "HEAD" ]; then
    branch="detached HEAD"
  fi
  tag=$(git -C "$repo_root" describe --tags --exact-match --match 'v*' HEAD 2>/dev/null || true)
  origin_main=$(git -C "$repo_root" rev-parse --verify --quiet refs/remotes/origin/main 2>/dev/null || true)

  if [ -n "$tag" ]; then
    source="release $tag"
  elif [ -n "$origin_main" ] && [ "$head_sha" = "$origin_main" ]; then
    source="origin/main@$head_short"
  elif [ "$force" -eq 1 ]; then
    echo "Warning: HEAD ($head_short on $branch) is not a release or main; installing anyway because --force is set." >&2
    source="$branch@$head_short"
  else
    echo "HEAD ($head_short on $branch) is not a release or main. Check out a v* release tag or main, or pass --force to install it anyway." >&2
    exit 1
  fi

  echo "Installing Finglass from $source"
}

# Read the bundle version without trusting a malformed Info.plist.
bundle_version() {
  local app=$1 plist version=""
  plist="$app/Contents/Info.plist"
  if [ -f "$plist" ]; then
    version=$(defaults read "$plist" CFBundleShortVersionString 2>/dev/null || true)
    case "$version" in
      "File Doesn't Exist"*) version="" ;;
    esac
    if [ -z "$version" ]; then
      version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$plist" 2>/dev/null || true)
    fi
  fi
  printf '%s\n' "${version:-unknown}"
}

check_source

echo "Building Finglass; the first build takes a few minutes."
"$repo_root/scripts/package-macos.sh"

built="$repo_root/target/packager/Finglass.app"
if [ ! -d "$built" ]; then
  echo "error: expected $built after packaging" >&2
  exit 1
fi

target="$install_dir/Finglass.app"
trash_dir=${FINGLASS_INSTALL_TRASH:-"$HOME/.Trash"}

# Replacing the bundle under a running app would crash it or leave it half old, half new.
running_pid=""
while read -r pid command; do
  case "$command" in
    "$target"/*)
      running_pid=$pid
      break
      ;;
  esac
done < <(ps -axo pid=,command=)
if [ -n "$running_pid" ]; then
  echo "Finglass is running from $target. Quit it, then install again." >&2
  exit 1
fi

if [ -e "$target" ] || [ -L "$target" ]; then
  previous=$(bundle_version "$target")
  stamp=$(date '+%Y-%m-%d %H.%M.%S')
  mkdir -p "$trash_dir"
  trashed="$trash_dir/Finglass $previous (replaced $stamp).app"
  # mv, not rm: it also handles an install folder on another volume, and keeps the old app.
  mv "$target" "$trashed"
  echo "Previous Finglass $previous moved to the Trash as \"$(basename "$trashed")\""
fi

mkdir -p "$install_dir"
# ditto keeps the bundle's signature, permissions and extended attributes intact.
ditto "$built" "$target"
codesign --verify --deep --strict "$target"
echo "Installed Finglass $(bundle_version "$target") to $target"
