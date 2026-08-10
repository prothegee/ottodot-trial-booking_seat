#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Static build with the version and the commit injected.
#
# The commit is read from git when nothing overrides it, so a local build still
# says which code it came from. A reviewer watching a recording has to be able
# to tell which build is on screen.
#
# Usage:
#   frontend/scripts/build.sh
#   BUILD_VERSION=1.4.0 API_BASE_URL=https://api.example.test frontend/scripts/build.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$frontend_root"

resolve_commit() {
    if [ -n "${BUILD_COMMIT:-}" ]; then
        printf '%s\n' "$BUILD_COMMIT"

        return 0
    fi

    git rev-parse --short=7 HEAD 2>/dev/null || printf 'unknown\n'
}

export API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:9000}"
export BUILD_VERSION="${BUILD_VERSION:-dev}"
BUILD_COMMIT="$(resolve_commit)"
export BUILD_COMMIT

printf 'building version %s, commit %s, api %s\n' "$BUILD_VERSION" "$BUILD_COMMIT" "$API_BASE_URL"

npm run build

printf 'static bundle is in %s/build\n' "$frontend_root"
