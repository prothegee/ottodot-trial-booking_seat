#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the flags and the terminal guard of backend/scripts/test_all.sh, without
# starting a container or running a single test.
#
# Every case that runs the subject ends inside a guard. The guard cases point
# DATABASE_CONTAINER at a name nothing answers to, which is what a machine with
# no stack looks like from here, so the refusing path can be proved without
# taking anybody's stack down.
#
# The property worth pinning is which way the guard reads. It asks whether this
# run would start containers, not whether somebody typed a flag, so a pipe that
# finds a stack already up is allowed through and a pipe that would raise one is
# not.
#
# Usage:
#   backend/scripts/test_all_test.sh
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

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
subject="$backend_root/scripts/test_all.sh"

# A container name nothing answers to, so database_require_running fails the way
# it would on a machine that has never started the stack.
ABSENT_CONTAINER="ottodot-postgres-primary-absent"

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

expect_count_in_code() {
    local name="$1"
    local pattern="$2"
    local wanted="$3"

    local found

    found="$(grep --count --extended-regexp "$pattern" "$subject")"

    if [ "$found" = "$wanted" ]; then
        report_pass "$name"

        return 0
    fi

    report_fail "$name, expected $wanted, found $found"
}

# Runs the subject against a stack that is not there, so it can only refuse.
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

    output="$(APP_ENV=development DATABASE_CONTAINER="$ABSENT_CONTAINER" \
        "$subject" "$@" 2>&1 </dev/null)"
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
    expect_run "the refusal names the flag that is accepted" 2 "--run-integration" --not-a-flag
}

run_guard_cases() {
    printf 'guard cases\n'

    # No stack and no flag is the case the guard exists for: this run would have
    # to raise containers, and a pipe cannot be asked whether it may.
    expect_run "no stack and no flag refuses" 2 "stdin is not a terminal"
    expect_run "the refusal names the flag that answers it" 2 "--run-integration"
}

run_source_cases() {
    printf 'source cases\n'

    # The change this file exists for. A pipe that finds a stack up starts
    # nothing, so the guard lets it through rather than refusing on the flag.
    expect_present_in_code "the guard asks whether a stack is up" \
        '^    if stack_is_up; then$'

    expect_present_in_code "being up is answered by the database library" \
        '^    database_require_running >/dev/null 2>&1$'

    # One definition of up, used by the guard and by the step that would raise
    # one. Two readings could disagree, and the disagreement would be a stack
    # started by a run that was never allowed to start one.
    expect_count_in_code "the step that raises a stack reads it the same way" \
        '^    if stack_is_up; then$' 2

    expect_present_in_code "the flag still answers the guard on its own" \
        '^    if \[ "\$run_the_integration" = "yes" \]; then$'

    expect_present_in_code "a terminal still needs no flag" \
        '^    if \[ -t 0 \]; then$'

    # The refusal has to stay reachable. A guard that always returns 0 would
    # pass every case above and let a pipe raise containers.
    expect_present_in_code "the refusal is still there to reach" \
        'exit 2'

    expect_present_in_code "only a stack this run started is taken down" \
        '^    if \[ "\$started_the_stack" != "yes" \]; then$'
}

run_self_cases() {
    local code

    printf 'self cases\n'

    # This ends in the guard at the top of this file, before a single case runs,
    # so it costs one process and cannot recurse.
    "${BASH_SOURCE[0]}" --run-integration >/dev/null 2>&1 </dev/null
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "a flag passed to this file refuses"
    else
        report_fail "a flag passed to this file refuses, expected exit 2, got $code"
    fi
}

main() {
    run_flag_cases
    run_guard_cases
    run_source_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
