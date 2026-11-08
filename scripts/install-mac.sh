#!/usr/bin/env bash
# Installs Finglass.app into an Applications folder.
#
# By default it downloads the newest published release (pre-releases included) from GitHub,
# verifies its checksum and signature, and installs it. No Rust is needed for that.
# With --from-source it builds this checkout instead.
#
# Usage: scripts/install-mac.sh [options]
#
#   --install-dir <folder>  Where Finglass.app goes (default: /Applications).
#   --version <tag>         Release to install, e.g. v0.2.2 or 0.2.2 (default: newest).
#   --reinstall             Install even when the same version is already installed.
#   --from-source           Build this checkout instead of downloading a release.
#   --force                 Source builds only: install a dirty checkout or a HEAD that is
#                           neither a v* release tag nor origin/main.
#   --no-update             Source builds only: do not fast-forward a clean main from origin.
#   --check-only            Resolve and verify everything, then exit without installing.
#   -h, --help              Show the usage.
#
# A previous Finglass.app is moved to the Trash, never deleted. Quit Finglass first.
#
# Environment:
#   FINGLASS_INSTALL_TRASH  Folder used instead of ~/.Trash when replacing an existing app.
#                           Meant for CI and tests; leave it unset for a normal install.
#   FINGLASS_RELEASES_API   Release-list URL (default: the GitHub API). Meant for tests.
#
# macOS only. Release installs write only under --install-dir and a temporary folder; source
# builds also write under target/.

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/install-mac.sh [options]

Installs the newest published Finglass release into /Applications. Pre-releases count, and no
Rust is needed. --from-source builds this checkout instead.

  --install-dir <folder>  Where Finglass.app goes (default: /Applications).
  --version <tag>         Release to install, e.g. v0.2.2 or 0.2.2 (default: newest).
  --reinstall             Install even when the same version is already installed.
  --from-source           Build this checkout instead of downloading a release.
                          Needs Rust 1.92+ and cargo-packager 0.11.8.
  --force                 Source builds only: install a dirty checkout or a HEAD that is
                          neither a v* release tag nor origin/main.
  --no-update             Source builds only: do not fast-forward a clean main from origin.
  --check-only            Resolve and verify everything, then exit without installing.
  -h, --help              Show this help.

A previous Finglass.app is moved to the Trash, never deleted. Quit Finglass first.
EOF
}

repo_root=$(cd "$(dirname "$0")/.." && pwd)
install_dir=/Applications
version_arg=""
reinstall=0
from_source=0
force=0
no_update=0
check_only=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --install-dir)
      if [ "$#" -lt 2 ] || [ -z "$2" ]; then
        echo "error: --install-dir needs a folder; nothing was changed." >&2
        usage >&2
        exit 2
      fi
      install_dir=$2
      shift 2
      ;;
    --install-dir=*)
      install_dir=${1#*=}
      if [ -z "$install_dir" ]; then
        echo "error: --install-dir needs a folder; nothing was changed." >&2
        usage >&2
        exit 2
      fi
      shift
      ;;
    --version)
      if [ "$#" -lt 2 ] || [ -z "$2" ]; then
        echo "error: --version needs a tag like v0.2.2 or 0.2.2; nothing was changed." >&2
        usage >&2
        exit 2
      fi
      version_arg=$2
      shift 2
      ;;
    --version=*)
      version_arg=${1#*=}
      if [ -z "$version_arg" ]; then
        echo "error: --version needs a tag like v0.2.2 or 0.2.2; nothing was changed." >&2
        usage >&2
        exit 2
      fi
      shift
      ;;
    --reinstall)
      reinstall=1
      shift
      ;;
    --from-source)
      from_source=1
      shift
      ;;
    --force)
      force=1
      shift
      ;;
    --no-update)
      no_update=1
      shift
      ;;
    --check-only)
      check_only=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1; nothing was changed." >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ "$from_source" -eq 0 ] && { [ "$force" -eq 1 ] || [ "$no_update" -eq 1 ]; }; then
  echo "error: --force and --no-update only apply to --from-source; nothing was changed." >&2
  usage >&2
  exit 2
fi
if [ "$from_source" -eq 1 ] && [ -n "$version_arg" ]; then
  echo "error: --version only applies to release installs, not --from-source; nothing was changed." >&2
  usage >&2
  exit 2
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "error: install-mac.sh installs a macOS app and only runs on macOS; nothing was changed." >&2
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

target="$install_dir/Finglass.app"
trash_dir=${FINGLASS_INSTALL_TRASH:-"$HOME/.Trash"}

tmp_dir=""
mount_point=""
staging=""
moved_old=0
trashed=""

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if [ -n "$staging" ] && [ -e "$staging" ]; then
    rm -rf "$staging" || true
  fi
  # Never leave the user with no app: put the old one back if a failure interrupted the replace.
  if [ "$moved_old" -eq 1 ] && [ ! -e "$target" ] && [ -n "$trashed" ] && [ -e "$trashed" ]; then
    mv "$trashed" "$target" 2>/dev/null || true
  fi
  if [ -n "$mount_point" ]; then
    hdiutil detach "$mount_point" >/dev/null 2>&1 || true
  fi
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

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

# Build inputs whose untracked files matter (tracked changes anywhere count, like FinDesk).
is_build_input() {
  case "$1" in
    crates/* | assets/* | scripts/* | Cargo.toml | Cargo.lock | rust-toolchain*) return 0 ;;
    *) return 1 ;;
  esac
}

# Fast-forward a clean main from origin, without ever resetting, stashing or merging commits.
# Skipped for tags, detached HEADs, other branches, dirty trees and --no-update.
maybe_update_main() {
  if [ "$no_update" -eq 1 ]; then
    return 0
  fi
  if ! git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi
  local branch
  branch=$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)
  if [ "$branch" != "main" ]; then
    return 0
  fi
  if [ -n "$(git -C "$repo_root" status --porcelain=v1 2>/dev/null)" ]; then
    return 0
  fi
  if ! git -C "$repo_root" remote get-url origin >/dev/null 2>&1; then
    return 0
  fi

  local old_sha old_short new_short origin_main
  old_sha=$(git -C "$repo_root" rev-parse HEAD)
  if ! git -C "$repo_root" fetch --quiet origin main 2>/dev/null; then
    echo "Warning: could not fetch origin/main (offline?); deciding from the local refs." >&2
    return 0
  fi
  origin_main=$(git -C "$repo_root" rev-parse --verify --quiet refs/remotes/origin/main 2>/dev/null || true)
  if [ -z "$origin_main" ] || [ "$old_sha" = "$origin_main" ]; then
    return 0
  fi
  if git -C "$repo_root" merge-base --is-ancestor "$old_sha" "$origin_main"; then
    old_short=$(git -C "$repo_root" rev-parse --short "$old_sha")
    if git -C "$repo_root" merge --ff-only --quiet origin/main; then
      new_short=$(git -C "$repo_root" rev-parse --short HEAD)
      echo "Updated main from $old_short to $new_short."
    else
      echo "Warning: could not fast-forward main to origin/main; using the local checkout." >&2
    fi
  fi
}

source_description=""

# Refuse a work-in-progress checkout before spending minutes on a build. Mirrors FinDesk's
# checkInstallSource: another checkout may still be built with --force.
check_source() {
  maybe_update_main

  if ! git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Warning: not a git checkout (or git is not installed), so Finglass cannot tell whether this is a released build." >&2
    source_description="this checkout"
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
      echo "This checkout has uncommitted changes (${joined}). Install from a clean checkout, or pass --force to install it anyway. Nothing was installed." >&2
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
    echo "HEAD ($head_short on $branch) is not a release or main. Check out a v* release tag or main, or pass --force to install it anyway. Nothing was installed." >&2
    echo "Run \`git switch main && git pull --ff-only\`, or install the latest release without \`--from-source\`." >&2
    exit 1
  fi

  source_description=$source
}

# curl options for every download. macOS 11 ships curl 7.64, which lacks --retry-all-errors (7.71+),
# and an unknown option would fail every download, so it is added only when this curl knows it.
curl_flags=(--fail --location --silent --show-error --retry 4 --retry-delay 3 --connect-timeout 15)
if { curl --help all 2>/dev/null || curl --help 2>/dev/null; } | grep -q -- '--retry-all-errors'; then
  curl_flags+=(--retry-all-errors)
fi

# Whether dotted version $1 is newer than $2 (numeric, three parts; anything else is never newer).
version_newer() {
  local a=$1 b=$2 i x y
  local -a left right
  [[ "$a" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && "$b" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  IFS=. read -r -a left <<<"$a"
  IFS=. read -r -a right <<<"$b"
  for i in 0 1 2; do
    x=$((10#${left[i]}))
    y=$((10#${right[i]}))
    if [ "$x" -ne "$y" ]; then
      [ "$x" -gt "$y" ]
      return
    fi
  done
  return 1
}

# arch name used in release assets.
release_arch() {
  case "$(uname -m)" in
    arm64) printf '%s\n' aarch64 ;;
    x86_64) printf '%s\n' x86_64 ;;
    *) return 1 ;;
  esac
}

# Fetch the release list and pick the release and assets. Sets release_tag, release_version,
# dmg_url and sums_url. The JSON is parsed with JavaScript for Automation, which ships with macOS.
resolve_release() {
  local api=$1 wanted=$2 arch=$3 machine=$4
  local releases_file="$tmp_dir/releases.json" script="$tmp_dir/resolve-release.js" line

  if ! curl "${curl_flags[@]}" "$api" --output "$releases_file"; then
    echo "error: could not read the release list from $api; nothing was changed." >&2
    return 1
  fi

  cat > "$script" <<'JAVASCRIPT'
ObjC.import('Foundation');

function readText(path) {
  var file = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null);
  if (!file) {
    throw new Error('cannot read ' + path);
  }
  return file.js;
}

function fail(message) {
  return 'error\t' + message;
}

function numbers(tag) {
  return tag.replace(/^v/, '').split('.').map(function (part) {
    return parseInt(part, 10);
  });
}

function isNewer(a, b) {
  var left = numbers(a);
  var right = numbers(b);
  for (var i = 0; i < 3; i += 1) {
    if (left[i] !== right[i]) {
      return left[i] > right[i];
    }
  }
  return false;
}

function run(argv) {
  var jsonPath = argv[0];
  var wanted = argv[1] || '';
  var arch = argv[2];
  var machine = argv[3];
  var releases;

  try {
    releases = JSON.parse(readText(jsonPath));
  } catch (error) {
    return fail('could not parse the release list: ' + error.message);
  }
  if (!Array.isArray(releases)) {
    return fail('the release list is not a JSON array');
  }

  var published = releases.filter(function (release) {
    return release && !release.draft && typeof release.tag_name === 'string' &&
      /^v\d+\.\d+\.\d+$/.test(release.tag_name);
  });

  var release;
  if (wanted) {
    release = published.find(function (candidate) {
      return candidate.tag_name === wanted;
    });
    if (!release) {
      return fail('release ' + wanted + ' is not among the published releases');
    }
  } else {
    if (published.length === 0) {
      return fail('no published v<major>.<minor>.<patch> release was found');
    }
    release = published.reduce(function (newest, candidate) {
      return isNewer(candidate.tag_name, newest.tag_name) ? candidate : newest;
    });
  }

  var tag = release.tag_name;
  var version = tag.slice(1);
  var assetName = 'Finglass_' + version + '_' + arch + '.dmg';
  var assets = Array.isArray(release.assets) ? release.assets : [];
  var dmg = assets.find(function (asset) {
    return asset && asset.name === assetName && asset.browser_download_url;
  });
  if (!dmg) {
    return fail('release ' + tag + ' has no ' + assetName + ' asset for ' + machine + ' Macs');
  }
  var sums = assets.find(function (asset) {
    return asset && asset.name === 'SHA256SUMS.txt' && asset.browser_download_url;
  });
  if (!sums) {
    return fail('release ' + tag + ' has no SHA256SUMS.txt asset');
  }

  return [tag, version, dmg.browser_download_url, sums.browser_download_url].join('\t');
}
JAVASCRIPT

  if ! line=$(osascript -l JavaScript "$script" "$releases_file" "$wanted" "$arch" "$machine"); then
    echo "error: could not resolve a release; nothing was changed." >&2
    return 1
  fi
  case "$line" in
    error$'\t'*)
      echo "error: ${line#error$'\t'} — nothing was changed." >&2
      return 1
      ;;
    *$'\t'*$'\t'*$'\t'*) ;;
    *)
      echo "error: could not resolve a release; nothing was changed." >&2
      return 1
      ;;
  esac

  IFS=$'\t' read -r release_tag release_version dmg_url sums_url <<<"$line"
}

# Copy the app into a staging bundle, verify it, and swap it into place atomically. The old app
# goes to the Trash and is put back if the swap fails, so the user is never left without an app.
install_app() {
  local src=$1
  local previous stamp running_pid pid command

  mkdir -p "$install_dir"
  staging="$install_dir/.Finglass.app.installing-$$"
  rm -rf "$staging"

  # ditto keeps the bundle's signature, permissions and extended attributes intact.
  if ! ditto "$src" "$staging"; then
    echo "error: could not copy the app into $install_dir; nothing was changed." >&2
    exit 1
  fi
  if xattr "$staging" 2>/dev/null | grep -qx 'com.apple.quarantine'; then
    xattr -d com.apple.quarantine "$staging" 2>/dev/null || true
  fi
  if ! codesign --verify --deep --strict "$staging"; then
    echo "error: the copied app failed signature verification; nothing was changed." >&2
    exit 1
  fi

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
    echo "Finglass is running from $target. Quit it, then install again. Nothing was changed." >&2
    exit 1
  fi

  if [ -e "$target" ] || [ -L "$target" ]; then
    previous=$(bundle_version "$target")
    stamp=$(date '+%Y-%m-%d %H.%M.%S')
    mkdir -p "$trash_dir"
    trashed="$trash_dir/Finglass $previous (replaced $stamp).app"
    # Mark this before the move so an interrupted move still restores the old app.
    moved_old=1
    # mv, not rm: it also handles an install folder on another volume, and keeps the old app.
    if ! mv "$target" "$trashed"; then
      echo "error: could not move the old app to the Trash; nothing was changed." >&2
      exit 1
    fi
    echo "Previous Finglass $previous moved to the Trash as \"$(basename "$trashed")\""
  fi

  if ! mv "$staging" "$target"; then
    if [ "$moved_old" -eq 1 ] && [ ! -e "$target" ] && [ -e "$trashed" ]; then
      mv "$trashed" "$target"
      moved_old=0
      echo "error: could not install the new app; the previous app was put back." >&2
    else
      echo "error: could not install the new app; the install folder was not changed." >&2
    fi
    exit 1
  fi
  staging=""
  moved_old=0

  echo "Installed Finglass $(bundle_version "$target") to $target"
}

if [ "$from_source" -eq 1 ]; then
  check_source
  if [ "$check_only" -eq 1 ]; then
    echo "Source check passed ($source_description); nothing was installed."
    exit 0
  fi

  echo "Installing Finglass from $source_description"
  echo "Building Finglass; the first build takes a few minutes."
  if ! "$repo_root/scripts/package-macos.sh"; then
    echo "error: building Finglass failed; nothing was installed." >&2
    exit 1
  fi

  built="$repo_root/target/packager/Finglass.app"
  if [ ! -d "$built" ]; then
    echo "error: expected $built after packaging; nothing was installed." >&2
    exit 1
  fi
  install_app "$built"
  exit 0
fi

arch=$(release_arch) || {
  echo "error: unsupported Mac architecture $(uname -m); nothing was changed." >&2
  exit 1
}
machine=$(uname -m)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/finglass-install.XXXXXX")

wanted_tag=""
if [ -n "$version_arg" ]; then
  case "$version_arg" in
    v*) wanted_tag=$version_arg ;;
    [0-9]*) wanted_tag="v$version_arg" ;;
    *)
      echo "error: --version expects a tag like v0.2.2 or 0.2.2; nothing was changed." >&2
      exit 2
      ;;
  esac
fi

releases_api=${FINGLASS_RELEASES_API:-"https://api.github.com/repos/MaMissaoui/Finglass/releases?per_page=20"}
release_tag=""
release_version=""
dmg_url=""
sums_url=""
resolve_release "$releases_api" "$wanted_tag" "$arch" "$machine"

if [ "$reinstall" -eq 0 ] && [ "$check_only" -eq 0 ] && [ -d "$target" ]; then
  installed_version=$(bundle_version "$target")
  if [ "$installed_version" = "$release_version" ]; then
    echo "Finglass $release_version is already installed."
    exit 0
  fi
  # A newer build (e.g. from --from-source) is never replaced by an older "latest" release unasked.
  if [ -z "$version_arg" ] && version_newer "$installed_version" "$release_version"; then
    echo "Finglass $installed_version is installed, which is newer than the latest release $release_version; keeping it. Pass --version v$release_version to install that release anyway. Nothing was changed."
    exit 0
  fi
fi

dmg_name="Finglass_${release_version}_${arch}.dmg"
dmg_path="$tmp_dir/$dmg_name"
sums_path="$tmp_dir/SHA256SUMS.txt"

if [ -t 1 ]; then
  if ! curl "${curl_flags[@]}" --progress-bar "$dmg_url" --output "$dmg_path"; then
    echo "error: could not download $dmg_name; nothing was changed." >&2
    exit 1
  fi
elif ! curl "${curl_flags[@]}" "$dmg_url" --output "$dmg_path"; then
  echo "error: could not download $dmg_name; nothing was changed." >&2
  exit 1
fi
if ! curl "${curl_flags[@]}" "$sums_url" --output "$sums_path"; then
  echo "error: could not download SHA256SUMS.txt; nothing was changed." >&2
  exit 1
fi

actual_sum=$(shasum -a 256 "$dmg_path" | awk '{print $1}')
expected_sum=$(awk -v name="$dmg_name" '{
  file = $2;
  sub(/^\*/, "", file);
  if (file == name) {
    print $1;
    exit;
  }
}' "$sums_path")
if [ -z "$expected_sum" ]; then
  echo "error: $dmg_name is not listed in SHA256SUMS.txt — nothing was changed." >&2
  exit 1
fi
if [ "$actual_sum" != "$expected_sum" ]; then
  echo "error: checksum mismatch — nothing was changed ($dmg_name)." >&2
  exit 1
fi

mount_point="$tmp_dir/mnt"
mkdir -p "$mount_point"
if ! hdiutil attach -nobrowse -readonly -noautoopen -mountpoint "$mount_point" "$dmg_path" \
  >/dev/null 2>"$tmp_dir/hdiutil-attach.log"; then
  cat "$tmp_dir/hdiutil-attach.log" >&2 || true
  echo "error: could not mount $dmg_name; nothing was changed." >&2
  exit 1
fi

mounted_app="$mount_point/Finglass.app"
if [ ! -d "$mounted_app" ]; then
  echo "error: $dmg_name does not contain Finglass.app; nothing was changed." >&2
  exit 1
fi
mounted_version=$(bundle_version "$mounted_app")
if [ "$mounted_version" != "$release_version" ]; then
  echo "error: $dmg_name contains Finglass $mounted_version, but the release is $release_version; nothing was changed." >&2
  exit 1
fi
if ! codesign --verify --deep --strict "$mounted_app"; then
  echo "error: the app in $dmg_name failed signature verification; nothing was changed." >&2
  exit 1
fi

if [ "$check_only" -eq 1 ]; then
  echo "Check passed: Finglass $release_version from $dmg_name is ready to install; nothing was installed."
  exit 0
fi

install_app "$mounted_app"
