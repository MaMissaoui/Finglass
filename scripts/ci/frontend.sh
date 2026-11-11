#!/usr/bin/env bash
# The frontend checks of CI's "Go" job: install, svelte-check, Vitest, build, then the Playwright screenshots in
# WebKit against the mock build. ci.yml and scripts/ci-local.sh both run this file.
# Screenshots go to $FINGLASS_SCREENSHOTS, or frontend/test-results/screenshots/.
set -euo pipefail
cd "$(dirname "$0")/../../frontend"

step() { printf '\n== %s\n' "$*"; }

step "npm ci"
npm ci --no-audit --no-fund

step "svelte-check"
npm run check

step "vitest"
npm test

step "vite build"
npm run build

step "playwright webkit"
npx playwright install webkit

step "screenshots"
npm run screenshots
