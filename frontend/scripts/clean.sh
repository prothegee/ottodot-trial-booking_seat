#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Removes the build state of this stack: node_modules, .svelte-kit, and build.
#
# Nothing here holds data, so the worst case is a reinstall. It still prompts,
# because the safety convention is defined once for the whole repository and a
# script that opts out of it teaches the wrong habit.
#
# The prompt, the manifest, and the guards all come from scripts/lib/confirm.sh
# at the repository root rather than from a copy kept here.
#
# Usage:
#   APP_ENV=development frontend/scripts/clean.sh
#   APP_ENV=development frontend/scripts/clean.sh --dry-run
#
# Exit codes:
# - 0: done, or the dry run finished
# - 1: declined by the operator
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$frontend_root/.." && pwd)"

source "$repository_root/scripts/lib/confirm.sh"

removable=(
    "$frontend_root/node_modules"
    "$frontend_root/.svelte-kit"
    "$frontend_root/build"
)

main() {
    local path

    confirm_parse_flags "$@"

    for path in "${removable[@]}"; do
        confirm_target_path "$path"
    done

    confirm_proceed "remove the build state of the frontend stack"

    for path in "${removable[@]}"; do
        if [ -e "$path" ]; then
            printf 'removing %s\n' "$path"
            rm -rf "$path"
        fi
    done

    printf 'build state is gone, run npm install to start again\n'
}

main "$@"
