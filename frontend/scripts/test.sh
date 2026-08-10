#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# The four tiers against the fake transport. Nothing needs to be running: no
# api, no database, no browser, no containers.
#
# Type checking runs first. A test suite that passes while the types are broken
# is reporting on a build nobody can ship.
#
# Usage:
#   frontend/scripts/test.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$frontend_root"

printf 'checking types\n'
npm run check

printf 'running the four tiers\n'
npm test
