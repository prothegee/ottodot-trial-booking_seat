#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests scripts/stack_restart.sh without stopping or starting anything.
#
# One case runs the script and stops in its argument guard. The rest read the
# source, because the property worth pinning is that this file delegates: a
# restart that grew its own stop or its own start could drift from what the two
# scripts underneath actually do.
#
# Usage:
#   scripts/stack_restart_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
subject="$repository_root/scripts/stack_restart.sh"

passed=0
failed=0

report_pass() {
    printf '  pass: %s\n' "$1"
    passed=$((passed + 1))
}

report_fail() {
    printf '  FAIL: %s\n' "$1" >&2
    failed=$((failed + 1))
}

# Finds a pattern in the script's code and never in its prose, so the header
# describing a behaviour cannot pass for the behaviour.
matches_in_code() {
    grep --line-number --extended-regexp "$1" "$subject" |
        grep --invert-match --extended-regexp '^[0-9]+:[[:space:]]*#'
}

run_delegation_cases() {
    printf 'delegation cases\n'

    if [ -z "$(matches_in_code 'scripts/stack_down.sh')" ]; then
        report_fail "it does not stop through stack_down.sh"
    else
        report_pass "it stops through stack_down.sh"
    fi

    if [ -z "$(matches_in_code 'scripts/stack_up.sh')" ]; then
        report_fail "it does not start through stack_up.sh"
    else
        report_pass "it starts through stack_up.sh"
    fi

    # A compose verb here would be a second definition of what a stop is, and the
    # two would drift the first time one of them changed.
    if [ -n "$(matches_in_code 'stack_compose')" ]; then
        report_fail "it drives compose itself instead of delegating"
    else
        report_pass "it drives no compose verb of its own"
    fi

    # Nothing in a restart removes anything, which is what keeps this file out of
    # the destructive class and out of a prompt.
    if [ -n "$(matches_in_code '(rm | rmi |--volumes|prune)')" ]; then
        report_fail "it removes something, which a restart must not"
    else
        report_pass "it removes nothing"
    fi
}

run_guard_cases() {
    local output
    local code

    printf 'guard cases\n'

    output="$("$subject" nonsense 2>&1 </dev/null)"
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "an unknown stack refuses before anything is stopped"
    else
        report_fail "an unknown stack exited $code, expected 2"
        printf '    output: %s\n' "$(printf '%s' "$output" | tr '\n' ' ')" >&2
    fi
}

main() {
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    run_delegation_cases
    run_guard_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
