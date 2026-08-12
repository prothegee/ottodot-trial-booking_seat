#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Every frontend test in one command: the types and the four tiers, the static
# build, and the guards on this stack's own scripts.
#
# Nothing needs to be running. There is no api to reach, no database, and no
# browser, because every tier here answers from a fake transport.
#
# The build is included because a bundle that will not build is a failure a
# reviewer meets before any test does, and svelte-check does not catch every one
# of them. It writes build/ and nothing else, which is gitignored.
#
# Every step runs even after one fails, and each failure is named again at the
# end. A runner that stops at the first one turns a full picture into several
# rounds of the same command.
#
# Usage:
#   frontend/scripts/test_all.sh
#
# Note:
# - The dependencies have to be installed already. This file refuses rather than
#   installing them, because fetching a package tree is not what somebody
#   running tests asked for
#
# Return:
# - 0: every step passed
# - 1: at least one step failed, each one named on the way out
# - 2: a flag was passed, or the dependencies are not installed
# ---------------------------------------------------------------------------- #

set -uo pipefail

# Every step here is fixed, so a flag typed at this file would run all of them
# and print the same report, which reads as though the flag did something.
if [ "$#" -gt 0 ]; then
    printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

    exit 2
fi

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Without this the first step fails on a missing binary, and the message names
# svelte-check rather than the one thing that has to happen on a fresh clone.
if [ ! -d "$frontend_root/node_modules" ]; then
    printf 'refused: the dependencies are not installed, run: cd %s && npm install\n' \
        "$frontend_root" >&2

    exit 2
fi

passed=0
failed=0
failed_steps=()

run_step() {
    local name="$1"

    shift

    printf '\n--> %s\n' "$name"

    if "$@"; then
        passed=$((passed + 1))

        return 0
    fi

    failed=$((failed + 1))
    failed_steps+=("$name")

    return 1
}

report() {
    local step_name

    printf '\n%d passed, %d failed\n' "$passed" "$failed"

    if [ "$failed" -eq 0 ]; then
        return 0
    fi

    printf 'failed:\n' >&2

    for step_name in "${failed_steps[@]}"; do
        printf '  - %s\n' "$step_name" >&2
    done

    return 1
}

main() {
    run_step "types and the four tiers" "$frontend_root/scripts/test.sh"
    run_step "the static build" "$frontend_root/scripts/build.sh"
    run_step "the guards on debug.sh" "$frontend_root/scripts/debug_test.sh"
    run_step "the guards on clean.sh" "$frontend_root/scripts/clean_test.sh"

    report
}

main "$@"
