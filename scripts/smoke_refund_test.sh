#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the flags, the guards, and the account rule of scripts/smoke_refund.sh,
# without writing a single row.
#
# Every case that runs the script ends inside a guard, before it reaches a
# database. That is possible because the order in the script is flags first,
# then the environment, then the stack, so a run with APP_ENV unset can prove
# the whole of the flag handling and nothing else.
#
# Two properties are worth pinning here, and both could quietly cost somebody
# something. Only demo accounts may be offered, so a real address can never be
# the one a refund is written against. And the decrease has to have a floor, so
# a number nobody is owed can never be closed.
#
# Usage:
#   scripts/smoke_refund_test.sh
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
subject="$repository_root/scripts/smoke_refund.sh"

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

    expect_run "an unknown flag refuses" 2 "--increase" --not-a-flag
    expect_run "no flag at all refuses" 2 "--decrease"
    expect_run "both actions at once refuse" 2 "not both" --increase --decrease
    expect_run "a zero refuses" 2 "1 or more" --increase 0
    expect_run "a zero refuses on the other action too" 2 "1 or more" --decrease 0

    # A negative number never reaches the count check. It does not match the
    # digits test, so it is read as a flag, and no flag is spelled that way.
    expect_run "a negative number refuses" 2 "unknown flag" --increase -3

    # The one case that proves the parsing accepted its input: it gets all the
    # way to the environment guard, which is the next thing in the script.
    expect_run "a valid run reaches the environment guard" 2 "APP_ENV" --increase 5
    expect_run "a number is optional" 2 "APP_ENV" --decrease

    expect_present_in_code "the number defaults to 1" \
        '^step=1$'

    expect_present_in_code "--dry-run and --yes are left to confirm.sh" \
        '[-]-dry-run\|--yes\)'
}

run_account_cases() {
    printf 'account cases\n'

    # The rule the user asked for. The list a number is chosen from is built by
    # one query, and that query has to carry the domain filter.
    expect_present_in_code "the demo domain is a named constant" \
        '^DEMO_EMAIL_DOMAIN='

    expect_present_in_code "the offered list is filtered to it" \
        "parents.email like '%@\\\$DEMO_EMAIL_DOMAIN'"

    expect_present_in_code "the admin account is not offered" \
        "parents.role = 'parent'"

    # A number outside the list is refused rather than clamped into it, because
    # clamping would write a refund against somebody nobody chose.
    expect_present_in_code "a number outside the list refuses" \
        'is not one of the numbers offered'

    expect_present_in_code "a parent with no child refuses" \
        'has no child on the account'
}

run_guard_cases() {
    printf 'guard cases\n'

    expect_present_in_code "the environment guard is the shared one" \
        'confirm_require_environment'

    expect_present_in_code "the manifest and prompt are the shared ones" \
        'confirm_proceed'

    expect_present_in_code "the target carries the project prefix" \
        'confirm_target_name "ottodot-postgres-primary"'

    # Nothing may be written before the operator has answered. Both writers are
    # called from run_the_change, and ask_first is called above them.
    expect_present_in_code "the prompt comes before the writing" \
        '^    ask_first "\$before"$'

    expect_absent_from_code "nothing here deletes, truncates, or drops" \
        '(delete from|truncate|drop table)'
}

run_floor_cases() {
    printf 'floor cases\n'

    # The decrease has to stop at zero rather than run past it. Two things make
    # that true: nothing owed ends the run, and the update is limited to what
    # was asked for out of what that parent has.
    expect_present_in_code "nothing owed ends the run before the prompt" \
        'is owed nothing, so there is nothing to close'

    expect_present_in_code "the close is limited to the number asked for" \
        '^            limit \$step$'

    expect_present_in_code "the close only picks bookings already owed" \
        "where status = 'refund_required'"

    expect_present_in_code "the close only picks that parent's bookings" \
        "and student_id in \\(select id from students where parent_id = '\\\$parent_id'\\)"

    # Newest first is what makes an increase undoable, and what keeps a refund
    # left by a real lost race last in line.
    expect_present_in_code "the newest are closed first" \
        '^            order by created_at desc$'

    # An increase past the api's own cap would stop being visible on the panel,
    # so it is refused with the reason rather than written and left puzzling.
    expect_present_in_code "an increase past the gauge ceiling refuses" \
        '^GAUGE_CEILING=200$'
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
    run_account_cases
    run_guard_cases
    run_floor_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
