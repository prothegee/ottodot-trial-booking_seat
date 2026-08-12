#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Checks that clean.sh is wired to the shared confirmation path.
#
# Only the paths that keep the build state run here. The confirming path is
# never exercised, because a test that proves deletion works by deleting
# node_modules costs a reinstall every run. What confirm.sh does after a y is
# covered by scripts/lib/confirm_test.sh, against a throwaway file.
#
# Usage:
#   frontend/scripts/clean_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

clean_script="$frontend_root/scripts/clean.sh"
output_file="$(mktemp)"

passed=0
failed=0

cleanup() {
    rm -f "$output_file"
}

trap cleanup EXIT

state_is_intact() {
    [ -d "$frontend_root/node_modules" ]
}

check() {
    local name="$1"
    local expected_code="$2"
    local actual_code="$3"

    if [ "$actual_code" != "$expected_code" ]; then
        printf '  FAIL: %s: expected exit %s, got %s\n' "$name" "$expected_code" "$actual_code" >&2
        printf '    output: %s\n' "$(tr '\n' ' ' <"$output_file")" >&2
        failed=$((failed + 1))

        return 1
    fi

    if ! state_is_intact; then
        printf '  FAIL: %s: node_modules was removed, this case must touch nothing\n' "$name" >&2
        failed=$((failed + 1))

        return 1
    fi

    printf '  pass: %s\n' "$name"
    passed=$((passed + 1))

    return 0
}

run_piped() {
    "$clean_script" "$@" >"$output_file" 2>&1 </dev/null

    printf '%s\n' "$?"
}

run_interactive() {
    local answer="$1"

    printf '%s\n' "$answer" | script --quiet --return \
        --command "$clean_script" /dev/null >"$output_file" 2>&1

    printf '%s\n' "$?"
}

main() {
    local self_code

    # Every case sets its own conditions, so a flag typed at this file would run
    # everything and print the same report, which reads as though the flag did
    # something.
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    if ! state_is_intact; then
        printf 'skipped: node_modules is not present, run npm install first\n'

        return 0
    fi

    printf 'clean.sh guard cases\n'

    check "APP_ENV unset refuses" 2 "$(unset APP_ENV; run_piped)"
    check "a pipe without --yes refuses" 2 "$(APP_ENV=development run_piped)"
    check "--dry-run touches nothing" 0 "$(APP_ENV=development run_piped --dry-run)"

    if grep --quiet "node_modules" "$output_file"; then
        printf '  pass: the manifest names node_modules before asking\n'
        passed=$((passed + 1))
    else
        printf '  FAIL: the manifest did not name node_modules\n' >&2
        failed=$((failed + 1))
    fi

    if command -v script >/dev/null 2>&1; then
        check "an empty answer declines" 1 "$(APP_ENV=development run_interactive '')"
        check "n declines" 1 "$(APP_ENV=development run_interactive 'n')"
    else
        printf 'prompt cases skipped: the "script" command is needed to attach a pseudo terminal\n'
    fi

    printf 'self cases\n'

    # The call below ends in the guard at the top of main, before a single case
    # runs, so it costs one process and cannot recurse.
    "${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
    self_code="$?"

    if [ "$self_code" = "2" ]; then
        printf '  pass: a flag passed to this file refuses\n'
        passed=$((passed + 1))
    else
        printf '  FAIL: a flag passed to this file refuses: expected exit 2, got %s\n' "$self_code" >&2
        failed=$((failed + 1))
    fi

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
