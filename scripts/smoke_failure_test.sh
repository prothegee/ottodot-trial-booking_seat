#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the flags, the guards, and the booking selection of
# scripts/smoke_failure.sh, without arming a fault or writing a single row.
#
# Every case that runs the script ends inside a guard, before it reaches any
# stack. The order in the script is flags first, then the environment, so a run
# with APP_ENV unset proves the whole of the flag handling and nothing else.
#
# The selection is what the rest of the cases are about. This script pays a
# booking it did not create on a second run, and the seeded child it uses is one
# other scripts also write rows for. Picking the newest of those rows instead
# of the one holding a seat is how a green suite turns red for a reason that has
# nothing to do with what the script is testing.
#
# Usage:
#   scripts/smoke_failure_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

if [ "$#" -gt 0 ]; then
    printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

    exit 2
fi

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
subject="$repository_root/scripts/smoke_failure.sh"

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

expect_present_in_code() {
    local name="$1"
    local pattern="$2"

    if grep --quiet --extended-regexp "$pattern" "$subject"; then
        report_pass "$name"

        return 0
    fi

    report_fail "$name"
}

expect_absent_from_code() {
    local name="$1"
    local pattern="$2"

    if grep --quiet --extended-regexp "$pattern" "$subject"; then
        report_fail "$name"

        return 0
    fi

    report_pass "$name"
}

# Runs the script with APP_ENV removed, so nothing it does can reach a stack.
#
# Param:
# name - string (what this case is called in the output)
# expected - string (the exit code the run should end with)
# needle - string (text the output has to carry, empty for none)
# ... - the flags to pass
expect_run() {
    local name="$1"
    local expected="$2"
    local needle="$3"
    shift 3

    local output
    local code

    output="$(env --unset=APP_ENV "$subject" "$@" 2>&1 </dev/null)"
    code="$?"

    if [ "$code" != "$expected" ]; then
        report_fail "$name, expected exit $expected, got $code: $output"

        return 0
    fi

    if [ -n "$needle" ]; then
        case "$output" in
            *"$needle"*) ;;
            *)
                report_fail "$name, the message does not say '$needle': $output"

                return 0
                ;;
        esac
    fi

    report_pass "$name"
}

run_flag_cases() {
    printf 'flag cases\n'

    expect_run "an unknown flag refuses" 2 "unknown flag" --not-a-flag
    expect_run "the refusal names the two flags taken" 2 "--dry-run and --yes" --not-a-flag

    # The one case that proves the parsing accepted its input: it gets all the
    # way to the environment guard, which is the next thing in the script.
    expect_run "a run with no flags reaches the environment guard" 2 "APP_ENV"
    expect_run "--yes reaches it too, rather than skipping it" 2 "APP_ENV" --yes
}

run_selection_cases() {
    printf 'selection cases\n'

    # The whole point of the reuse path. already_booked is only ever answered
    # while a pending_payment or a confirmed booking exists, so the row this
    # reads has to be named by status rather than by being the most recent.
    expect_present_in_code "the reused booking has to be holding a seat" \
        "and status = 'pending_payment'"

    expect_present_in_code "it is that child's booking, on that class" \
        "where student_id = '\\\$SMOKE_STUDENT_ID' and class_id = '\\\$SMOKE_CLASS_ID'"

    expect_present_in_code "the newest of those is taken" \
        '^        order by created_at desc$'

    expect_present_in_code "only one row is read" \
        '^        limit 1$'

    # Nothing to reuse is a refusal with a reason, not four assertions failing
    # further down against a booking that was never payable.
    expect_present_in_code "nothing to reuse refuses with the reason" \
        'no booking is in pending_payment'

    # The reuse branch is only entered on the api's own already_booked. Any
    # other 409 stops the run rather than reading as an earlier run's leftover.
    expect_present_in_code "only already_booked is read as a leftover" \
        'already_booked'
}

run_fault_cases() {
    printf 'fault cases\n'

    expect_present_in_code "the fault fires once and expires on its own" \
        '"count":1,"ttl_seconds":120'

    # A stack left broken by a script that died is the worst outcome here, so
    # the disarm is on the exit trap rather than at the end of the happy path.
    expect_present_in_code "the disarm is on the exit trap" \
        '^trap cleanup EXIT$'

    expect_present_in_code "the disarm asks the api to clear the point" \
        'api_request DELETE "\$jar_admin" /dev/faults'

    # A fault surface that is off answers 404, and guessing why is what this
    # avoids: the script says which setting the api actually got.
    expect_present_in_code "a fault surface that is off is named, not guessed" \
        'the fault surface is off'
}

run_guard_cases() {
    printf 'guard cases\n'

    expect_present_in_code "the environment guard is the shared one" \
        'confirm_require_environment'

    expect_present_in_code "the manifest and prompt are the shared ones" \
        'confirm_proceed'

    expect_present_in_code "both targets carry the project prefix" \
        'confirm_target_name "ottodot-api"'

    expect_present_in_code "the database it writes to is named as well" \
        'confirm_target_name "ottodot-postgres-primary"'

    expect_absent_from_code "nothing here deletes, truncates, or drops" \
        '(delete from|truncate|drop table)'
}

run_self_cases() {
    local code

    printf 'self cases\n'

    # This ends in the guard at the top of this file, before a single case runs,
    # so it costs one process and cannot recurse.
    "${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "a flag passed to this file refuses"
    else
        report_fail "a flag passed to this file refuses, expected exit 2, got $code"
    fi
}

main() {
    run_flag_cases
    run_selection_cases
    run_fault_cases
    run_guard_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
