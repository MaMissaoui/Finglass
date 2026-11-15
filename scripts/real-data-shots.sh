#!/usr/bin/env bash
# Export a Finglass backup and screenshot every page with that real data.
#
#   scripts/real-data-shots.sh <backup.zip> <folder outside the repo>
#
# The exporter restores the backup inside a temporary home. The screenshots in
# <folder> hold real account data: keep them out of the repository.
set -euo pipefail

usage() {
  echo "usage: $0 <backup.zip> <folder outside the repo>" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage

resolve_path() {
  case "$1" in
  /*) printf '%s\n' "$1" ;;
  *) printf '%s/%s\n' "$PWD" "$1" ;;
  esac
}

backup="$(resolve_path "$1")"
out_dir="$(resolve_path "$2")"
root="$(cd "$(dirname "$0")/.." && pwd)"
frontend="$root/frontend"

cd "$root"

echo "Exporting $backup"
go run ./cmd/finglass-shots --backup "$backup" --out "$out_dir"

if [ ! -d "$frontend/node_modules/@playwright/test" ]; then
  echo "Installing frontend dependencies"
  (cd "$frontend" && npm ci --no-audit --no-fund)
fi

echo "Building the mock frontend"
(cd "$frontend" && npx vite build --mode mock --outDir dist-mock --emptyOutDir)

server_log="$(mktemp "${TMPDIR:-/tmp}/finglass-shots-server.XXXXXX")"
server_pid=""
cleanup() {
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -f "$server_log"
}
trap cleanup EXIT

node "$root/scripts/serve-real-data-shots.mjs" \
  "$out_dir/snapshot.json" "$frontend/dist-mock" >"$server_log" 2>&1 &
server_pid=$!

port=""
for _ in $(seq 1 100); do
  if grep -q '^PORT=' "$server_log" 2>/dev/null; then
    port="$(sed -n 's/^PORT=//p' "$server_log" | head -n 1)"
    break
  fi
  sleep 0.1
done
if [ -z "$port" ]; then
  echo "The local preview server did not start:" >&2
  cat "$server_log" >&2
  exit 1
fi

echo "Serving the snapshot on 127.0.0.1:$port"
node "$frontend/tests/real-data-shots.mjs" "http://127.0.0.1:$port" "$out_dir/png"

# The screenshots hold real account data; keep them owner-only like the JSON.
find "$out_dir/png" -type d -exec chmod 700 {} +
find "$out_dir/png" -type f -exec chmod 600 {} +

cat <<EOF

Screenshots written to:
  $out_dir/png

They contain real account data. Keep that folder outside the repository and
delete it when you are done.
EOF
