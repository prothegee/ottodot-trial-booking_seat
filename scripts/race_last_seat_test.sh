#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the guards and the cleanup of scripts/race_last_seat.sh, without racing
# anything. One case runs the script and stops in its flag guard, the rest read
# the source.
#
# The property worth pinning is the one that could quietly cost somebody their
# data. --fresh-class makes a real class in the real table, so the run has to
# delete it again, and every delete it issues has to be scoped to that one
# class. A delete that lost its where clause would empty the seeded rows on a
# machine somebody was demonstrating from, and it would do it on the way out
# where nobody is reading.
#
# Racing for real is what the script itself is, so nothing here does it. The
# reproducing path is proven by running it, which is what
# scripts/test_integration.sh does on every pull request.
#
# Usage:
#   scripts/race_last_seat_test.sh
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
subject="$repository_root/scripts/race_last_seat.sh"

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

run_flag_cases() {
    local output
    local code

    printf 'flag cases\n'

    output="$("$subject" --not-a-flag 2>&1 </dev/null)"
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "an unknown flag refuses"
    else
        report_fail "an unknown flag refuses, expected exit 2, got $code"
    fi

    # The refusal has to name the flag that does exist. A refusal that only says
    # no leaves the reader to go and find the source.
    case "$output" in
        *--fresh-class*) report_pass "the refusal names the flag that is accepted" ;;
        *) report_fail "the refusal does not name --fresh-class: $output" ;;
    esac

    # The leading dash is bracketed so grep reads a pattern rather than a flag.
    expect_present_in_code "--fresh-class is the only flag taken" \
        '[-]-fresh-class\)'
}

run_offer_cases() {
    printf 'offer cases\n'

    # The seeded seat is raced when it is free, and the offer is only ever made
    # when it is not. An unconditional offer would quietly stop testing the
    # thing the brief asks about.
    expect_present_in_code "a free seeded seat is raced without asking" \
        'if \[ "\$confirmed" = "0" \]; then'

    expect_present_in_code "the prompt defaults to No" \
        'race a throwaway class instead\? \[y/N\]'

    expect_present_in_code "only y accepts the offer" \
        'if \[ "\$reply" != "y" \]; then'

    # Nothing is attached to answer in a workflow, so the run has to end rather
    # than hang on a read nobody will ever type into.
    expect_present_in_code "no terminal refuses instead of asking" \
        'if \[ ! -t 0 \]; then'

    expect_present_in_code "the declined path names the way back" \
        'seed_reset\.sh'
}

run_cleanup_cases() {
    local deletes
    local unscoped

    printf 'cleanup cases'

    printf '\n'

    expect_present_in_code "the throwaway class is dropped on the way out" \
        'drop_the_fresh_class'

    expect_present_in_code "the drop runs from the exit trap" \
        'trap cleanup EXIT'

    # The call, indented inside cleanup, not the definition at the left margin.
    expect_present_in_code "the cleanup calls the drop" \
        '^[[:space:]]+drop_the_fresh_class$'

    # A run that raced the seeded class has no throwaway class, and a drop that
    # carried on regardless would be deleting with an empty identifier.
    expect_present_in_code "a run without a throwaway class drops nothing" \
        'if \[ -z "\$fresh_class_id" \]; then'

    # The one that matters. Every delete has to name the class this run made,
    # either directly or through the bookings that belong to it.
    deletes="$(grep --count --extended-regexp '^[[:space:]]*delete from' "$subject")"
    unscoped="$(grep --extended-regexp --after-context=2 '^[[:space:]]*delete from' "$subject" |
        grep --count --extended-regexp 'fresh_class_id')"

    if [ "$deletes" -gt 0 ] && [ "$unscoped" -ge "$deletes" ]; then
        report_pass "all $deletes deletes name the throwaway class"
    else
        report_fail "$deletes delete(s) found, only $unscoped scoped to fresh_class_id"
    fi

    expect_absent_from_code "nothing here truncates or drops a table" \
        '(truncate|drop table|delete from [a-z_]+;)'

    # The seeded identifiers are read, never deleted. A delete naming one of
    # them would be destroying the dataset the rest of the tests share.
    expect_absent_from_code "no delete names a seeded identifier" \
        'delete from .*0192a000'
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
    run_offer_cases
    run_cleanup_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
