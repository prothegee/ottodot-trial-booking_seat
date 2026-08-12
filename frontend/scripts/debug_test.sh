#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Checks the guards of debug.sh, and reads its source for the promises its
# header makes.
#
# The dev server is never started here. Every case either stops at a guard that
# fires before the handoff, or reads the file.
#
# Usage:
#   frontend/scripts/debug_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

# Every case here sets its own conditions, so a flag typed at this file would run
# everything and print the same report, which reads as though the flag did
# something.
if [ "$#" -gt 0 ]; then
    printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

    exit 2
fi

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

subject="$frontend_root/scripts/debug.sh"
output_file="$(mktemp)"

passed=0
failed=0

cleanup() {
    rm -f "$output_file"
}

trap cleanup EXIT

record_pass() {
    printf '  pass: %s\n' "$1"
    passed=$((passed + 1))
}

record_fail() {
    printf '  FAIL: %s: %s\n' "$1" "$2" >&2
    failed=$((failed + 1))
}

run_script() {
    "$subject" "$@" >"$output_file" 2>&1 </dev/null

    printf '%s\n' "$?"
}

expect_refusal() {
    local name="$1"
    local expected_text="$2"
    local actual_code="$3"

    if [ "$actual_code" != "2" ]; then
        record_fail "$name" "expected exit 2, got $actual_code"

        return 1
    fi

    if ! grep --quiet --fixed-strings "$expected_text" "$output_file"; then
        record_fail "$name" "the message did not mention '$expected_text': $(tr '\n' ' ' <"$output_file")"

        return 1
    fi

    record_pass "$name"
}

# Reads the source with comment lines dropped, so a word this script promises
# never to use cannot pass just because the header explains why it is absent.
matches_in_code() {
    local pattern="$1"

    grep --line-number --extended-regexp "$pattern" "$subject" |
        grep --invert-match --extended-regexp '^[0-9]+:[[:space:]]*#'
}

expect_absent_from_code() {
    local name="$1"
    local pattern="$2"
    local found

    found="$(matches_in_code "$pattern")"

    if [ -n "$found" ]; then
        record_fail "$name" "found: $(printf '%s' "$found" | tr '\n' ' ')"

        return 1
    fi

    record_pass "$name"
}

expect_present_in_code() {
    local name="$1"
    local pattern="$2"

    if [ -z "$(matches_in_code "$pattern")" ]; then
        record_fail "$name" "no line matched '$pattern'"

        return 1
    fi

    record_pass "$name"
}

# Finds where main calls something. The call is the only line that is exactly
# the name at one indent, since a definition ends in a brace.
call_line_of() {
    grep --line-number --extended-regexp "^    $1\$" "$subject" | cut --delimiter=: --fields=1
}

expect_call_order() {
    local name="$1"
    local first
    local second

    first="$(call_line_of "$2")"
    second="$(call_line_of "$3")"

    if [ -z "$first" ] || [ -z "$second" ]; then
        record_fail "$name" "one of the calls is missing, found '$first' and '$second'"

        return 1
    fi

    if [ "$first" -ge "$second" ]; then
        record_fail "$name" "$2 is on line $first, $3 on line $second"

        return 1
    fi

    record_pass "$name"
}

printf 'debug.sh guards\n'

expect_refusal "an argument is refused, this script takes none" \
    "unknown argument '--keep'" \
    "$(run_script --keep)"

printf 'debug.sh source\n'

# The port check has to come before anything slow. A refusal that arrives after
# an npm install has already cost the minute it was meant to save.
expect_call_order "the port check runs before the dependency install" \
    require_the_port_is_free require_dependencies

expect_absent_from_code "it never touches a compose file" \
    'compose'

# Asking a runtime whether one container is running is all this script needs. A
# start would mean it can bring something up, a stop would mean it can take
# something down, and it promises neither.
expect_absent_from_code "the runtime is only ever asked to inspect" \
    '"\$runtime" (start|run|create|stop|kill|exec|rm|rmi)'

expect_absent_from_code "it removes nothing" \
    '(^|[^[:alnum:]_-])(rm|rmi)([^[:alnum:]_-]|$)'

# One definition of how the server starts. A vite or npm run dev here would be
# a second one, and the two would drift.
expect_absent_from_code "it does not define its own way to start the server" \
    'vite|npm run dev'

expect_present_in_code "it hands off to dev.sh" \
    'exec "\$frontend_root/scripts/dev.sh"'

# Monitoring is reported and never started. It belongs to the backend stack, and
# a script in this directory reaching across to start it is exactly the coupling
# the two stacks are split to avoid.
expect_present_in_code "it reports on monitoring before handing off" \
    '^    report_on_monitoring$'

# stack_compose is the only way anything in this repository brings a service up,
# so its absence is the whole proof. Naming stack_up.sh inside a printf is the
# point of the report and has to stay legal, which is why the check is on the
# call and not on the string.
expect_absent_from_code "it never starts the monitoring it reports on" \
    'stack_compose'

# The call below ends in the guard at the top of this file, before a single case
# runs, so it costs one process and cannot recurse.
"${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
self_code="$?"

if [ "$self_code" = "2" ]; then
    record_pass "a flag passed to this file refuses"
else
    record_fail "a flag passed to this file refuses" "expected exit 2, got $self_code"
fi

printf '\n%s passed, %s failed\n' "$passed" "$failed"

if [ "$failed" -gt 0 ]; then
    exit 1
fi
