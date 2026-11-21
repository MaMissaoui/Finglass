#!/usr/bin/env bash
# Generates packaging/THIRD-PARTY-NOTICES.txt from the ported NOTICE files, the Go modules compiled into
# ./cmd/finglass and the JavaScript packages bundled into the frontend. With --check, writes to a temporary file
# and exits 1 if the committed file differs. The output is deterministic: sorted, no dates, no absolute paths.
set -euo pipefail
export LC_ALL=C

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/packaging/THIRD-PARTY-NOTICES.txt"

check=0
if [[ "${1:-}" == "--check" ]]; then
  check=1
  shift
fi
if [[ $# -ne 0 ]]; then
  echo "usage: $0 [--check]" >&2
  exit 2
fi

tmp="$(mktemp "${TMPDIR:-/tmp}/finglass-third-party-notices.XXXXXX")"
missing_go="$(mktemp "${TMPDIR:-/tmp}/finglass-missing-go.XXXXXX")"
missing_js="$(mktemp "${TMPDIR:-/tmp}/finglass-missing-js.XXXXXX")"
cleanup() {
  rm -f "$tmp" "$missing_go" "$missing_js"
}
trap cleanup EXIT

# emit_file prints a file and makes sure the next output starts on a new line.
emit_file() {
  local file="$1"
  cat "$file"
  if [[ -n "$(tail -c 1 "$file")" ]]; then
    printf '\n'
  fi
}

section() {
  printf '%s\n%s\n\n' "$1" "$(printf '%s' "$1" | tr '[:print:]' '-')"
}

# license_files prints the regular files in a directory whose name starts with LICENSE, LICENCE, COPYING or
# NOTICE (case-insensitive), sorted by name.
license_files() {
  local dir="$1" file base lower
  [[ -d "$dir" ]] || return 0
  while IFS= read -r file; do
    base="${file##*/}"
    lower="$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')"
    case "$lower" in
      license*|licence*|copying*|notice*) printf '%s\n' "$file" ;;
    esac
  done < <(find "$dir" -maxdepth 1 -type f | sort)
}

# print_entry writes one module/package entry and records it when it has no licence file.
print_entry() {
  local label="$1" version="$2" dir="$3" missing_file="$4"
  local found=0 file
  printf '== %s %s ==\n' "$label" "$version"
  while IFS= read -r file; do
    [[ -n "$file" ]] || continue
    found=1
    printf '\n-- %s --\n\n' "${file##*/}"
    emit_file "$file"
    printf '\n'
  done < <(license_files "$dir")
  if [[ "$found" -eq 0 ]]; then
    printf '(no licence file found in the module)\n\n'
    printf '%s %s\n' "$label" "$version" >>"$missing_file"
  fi
}

{
  printf 'Finglass includes the following third-party software. Their licence texts follow.\n\n'

  section "Ported code"
  for notice in \
    internal/fints/NOTICE \
    internal/core/documents/pdftext/NOTICE \
    internal/core/decimal/NOTICE; do
    printf '%s\n\n' "$notice"
    emit_file "$root/$notice"
    printf '\n'
  done

  section "Go modules compiled into the app"
  # The target is pinned to the macOS app, so the list is the same on every host.
  go_module_lines="$(
    cd "$root"
    GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go list -deps -f '{{with .Module}}{{if not .Main}}{{with .Replace}}{{.Path}} {{.Version}}{{else}}{{.Path}} {{.Version}}{{end}} {{.Dir}}{{end}}{{end}}' ./cmd/finglass |
      sed '/^[[:space:]]*$/d' | sort -u
  )"
  if printf '%s\n' "$go_module_lines" | awk 'NF < 3 { found = 1 } END { exit !found }'; then
    (cd "$root" && go mod download)
    go_module_lines="$(
      cd "$root"
      GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go list -deps -f '{{with .Module}}{{if not .Main}}{{with .Replace}}{{.Path}} {{.Version}}{{else}}{{.Path}} {{.Version}}{{end}} {{.Dir}}{{end}}{{end}}' ./cmd/finglass |
        sed '/^[[:space:]]*$/d' | sort -u
    )"
  fi
  while read -r path version dir; do
    [[ -n "${path:-}" ]] || continue
    print_entry "$path" "$version" "$dir" "$missing_go"
  done <<<"$go_module_lines"

  section "JavaScript packages bundled into the window"
  if [[ ! -d "$root/frontend/node_modules" ]]; then
    echo "frontend/node_modules is missing; run: (cd frontend && npm ci)" >&2
    exit 1
  fi

  node - "$root/frontend" "$missing_js" <<'NODE'
const fs = require("fs");
const path = require("path");

const frontendDir = process.argv[2];
const missingPath = process.argv[3];
const licensePattern = /^(license|licence|copying|notice)/i;

function compare(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}

function resolvePackage(name, fromDir) {
  let dir = fromDir;
  while (true) {
    const candidate = path.join(dir, "node_modules", name);
    const manifest = path.join(candidate, "package.json");
    if (fs.existsSync(manifest)) {
      return candidate;
    }
    const parent = path.dirname(dir);
    if (parent === dir) {
      throw new Error(`cannot resolve JavaScript package ${name} from ${fromDir}`);
    }
    dir = parent;
  }
}

function licenseFiles(dir) {
  return fs
    .readdirSync(dir)
    .filter((name) => licensePattern.test(name))
    .filter((name) => {
      try {
        return fs.statSync(path.join(dir, name)).isFile();
      } catch {
        return false;
      }
    })
    .sort(compare);
}

function mitText(author) {
  return [
    "MIT License",
    "",
    `Copyright (c) ${author}`,
    "",
    "Permission is hereby granted, free of charge, to any person obtaining a copy",
    'of this software and associated documentation files (the "Software"), to deal',
    "in the Software without restriction, including without limitation the rights",
    "to use, copy, modify, merge, publish, distribute, sublicense, and/or sell",
    "copies of the Software, and to permit persons to whom the Software is",
    "furnished to do so, subject to the following conditions:",
    "",
    "The above copyright notice and this permission notice shall be included in all",
    "copies or substantial portions of the Software.",
    "",
    'THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR',
    "IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,",
    "FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE",
    "AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER",
    "LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,",
    "OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE",
    "SOFTWARE.",
    "",
    "",
  ].join("\n");
}

const roots = ["@wailsio/runtime", "svelte"].map((name) => resolvePackage(name, frontendDir));
const pending = roots.slice();
const seen = new Set();
const packages = [];

while (pending.length > 0) {
  const dir = pending.pop();
  let realDir;
  try {
    realDir = fs.realpathSync(dir);
  } catch {
    realDir = dir;
  }
  if (seen.has(realDir)) {
    continue;
  }
  seen.add(realDir);

  const manifest = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
  packages.push({ name: manifest.name, version: manifest.version, dir, license: manifest.license, author: manifest.author });
  for (const dependency of Object.keys(manifest.dependencies || {}).sort(compare)) {
    pending.push(resolvePackage(dependency, dir));
  }
}

packages.sort((a, b) => compare(a.name, b.name) || compare(a.version, b.version));

const missing = [];
for (const pkg of packages) {
  const files = licenseFiles(pkg.dir);
  process.stdout.write(`== ${pkg.name} ${pkg.version} ==\n`);
  if (files.length === 0) {
    const author = typeof pkg.author === "string" ? pkg.author : (pkg.author && pkg.author.name) || "";
    if (pkg.license === "MIT" && author !== "") {
      // The package ships no licence file but declares MIT in package.json:
      // print the standard MIT text with the declared author.
      process.stdout.write(`(no licence file in the package; package.json declares MIT, author ${author})\n\n`);
      process.stdout.write(mitText(author));
      continue;
    }
    process.stdout.write("(no licence file found in the module)\n\n");
    missing.push(`${pkg.name} ${pkg.version}`);
    continue;
  }
  for (const name of files) {
    process.stdout.write(`\n-- ${name} --\n\n`);
    const text = fs.readFileSync(path.join(pkg.dir, name), "utf8");
    process.stdout.write(text);
    if (!text.endsWith("\n")) {
      process.stdout.write("\n");
    }
    process.stdout.write("\n");
  }
}

if (missing.length > 0) {
  fs.writeFileSync(missingPath, `${missing.join("\n")}\n`);
}
NODE

  section "Fonts and icons"
  printf 'IBM Plex Sans (SIL Open Font License 1.1)\n\n'
  emit_file "$root/frontend/public/fonts/OFL.txt"
  printf '\nMaterial icons - Apache-2.0\nSource: https://fonts.google.com/icons\n'
  material_dir="$root/frontend/node_modules/@material-design-icons/svg"
  material_license=""
  if [[ -d "$material_dir" ]]; then
    while IFS= read -r file; do
      [[ -n "$file" ]] || continue
      material_license="$file"
      break
    done < <(license_files "$material_dir")
  fi
  if [[ -n "$material_license" ]]; then
    printf '\n%s\n\n' "${material_license##*/}"
    emit_file "$material_license"
    printf '\n'
  fi
} >"$tmp"

if [[ -s "$missing_go" ]]; then
  echo "warning: Go modules with no licence file:" >&2
  sed 's/^/  /' "$missing_go" >&2
fi
if [[ -s "$missing_js" ]]; then
  echo "warning: JavaScript packages with no licence file:" >&2
  sed 's/^/  /' "$missing_js" >&2
fi

if [[ "$check" -eq 1 ]]; then
  if [[ ! -f "$out" ]] || ! cmp -s "$tmp" "$out"; then
    echo "packaging/THIRD-PARTY-NOTICES.txt is out of date; run scripts/third-party-notices.sh" >&2
    exit 1
  fi
  exit 0
fi

chmod 0644 "$tmp"
mv "$tmp" "$out"
