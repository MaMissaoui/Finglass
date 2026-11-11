#!/usr/bin/env bash
# Builds and self-checks the Go Finglass.app (universal, ad-hoc signed). ci.yml and scripts/ci-local.sh both run
# this file. The packaging script itself runs with GOPROXY=off, so the modules (including the wails3 tool's)
# are downloaded first.
set -euo pipefail
cd "$(dirname "$0")/../.."

printf '\n== go mod download\n'
go mod download

printf '\n== package Finglass.app\n'
bash scripts/go-package-macos.sh "${1:-0.4.0-dev}"
