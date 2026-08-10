#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Dev server on 9001, pointed at an api on 9000.
#
# The port is strict on purpose. A dev server that quietly moves to 9002 when
# 9001 is busy would sit on the worker metrics port, and the failure would show
# up later as a confusing scrape.
#
# Usage:
#   frontend/scripts/dev.sh
#   API_BASE_URL=http://127.0.0.1:9500 frontend/scripts/dev.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$frontend_root"

export API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:9000}"
export FRONTEND_PORT="${FRONTEND_PORT:-9001}"

printf 'dev server on %s, api at %s\n' "$FRONTEND_PORT" "$API_BASE_URL"

exec npm run dev
