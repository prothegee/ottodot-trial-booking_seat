#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests scripts/stack_status.sh. One case runs it and reads what it printed, the
# rest read the source.
#
# Running it is safe here in a way that running most subjects would not be: this
# one only ever reads, so a case that executes it costs a second and starts
# nothing. That is worth using, because the property most worth pinning is that
# every service in a compose file is reported on, and the honest way to check
# that is to look at the output rather than at the code that produced it.
#
# Usage:
#   scripts/stack_status_test.sh
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
subject="$repository_root/scripts/stack_status.sh"

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
    if grep --quiet --extended-regexp "$2" "$subject"; then
        report_pass "$1"

        return 0
    fi

    report_fail "$1"
}

expect_absent_from_code() {
    if grep --quiet --extended-regexp "$2" "$subject"; then
        report_fail "$1"

        return 0
    fi

    report_pass "$1"
}

# Every container name declared across both compose files.
compose_containers() {
    awk '/^[[:space:]]+container_name:/ { print $2 }' \
        "$repository_root/backend/compose.yml" "$repository_root/frontend/compose.yml"
}

run_guard_cases() {
    local code

    printf 'guard cases\n'

    "$subject" bogus >/dev/null 2>&1 </dev/null
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "an unknown stack name refuses"
    else
        report_fail "an unknown stack name refuses, expected exit 2, got $code"
    fi
}

# The reason this file exists. A status command that starts or removes something
# is not a status command, and the mistake would only be noticed by whoever lost
# a container to it.
run_read_only_cases() {
    printf 'read only cases\n'

    expect_absent_from_code "it never drives a compose verb" \
        'stack_compose'

    expect_absent_from_code "no runtime verb here changes anything" \
        '\$runtime" (rm|rmi|stop|start|restart|kill|prune|create|run|system)'

    expect_absent_from_code "it never removes a path" \
        'rm -(r|f)'

    expect_present_in_code "the runtime is only ever asked to list or inspect" \
        '\$runtime" (ps|inspect)'
}

run_coverage_cases() {
    local output
    local name
    local absent=0

    printf 'coverage cases\n'

    expect_present_in_code "the names are read from the compose files" \
        'container_name:'

    expect_absent_from_code "no service list is hard coded beside them" \
        '^[A-Z_]+_CONTAINERS='

    # The output has to name every service, whatever state it is in. A service
    # added to a compose file and missing here would be reported on by nobody,
    # and the summary line would still say everything is up.
    output="$("$subject" 2>&1 </dev/null)"

    while read -r name; do
        if ! printf '%s\n' "$output" | grep --quiet "$name"; then
            report_fail "$name is in a compose file and not in the output"
            absent=$((absent + 1))
        fi
    done < <(compose_containers)

    if [ "$absent" -eq 0 ]; then
        report_pass "every compose service appears in the output"
    fi

    case "$output" in
        *"what the test runs need"*) report_pass "it reports what the test runs need" ;;
        *) report_fail "the test run readings are missing from the output" ;;
    esac
}

# The api image carries the binary and nothing else. An exec into it fails and
# returns the same empty string as a setting that is off, so reading the setting
# that way reported one as the other.
run_fault_reading_cases() {
    printf 'fault reading cases\n'

    expect_present_in_code "the fault setting is read with inspect" \
        'inspect "\$API_CONTAINER"'

    expect_absent_from_code "it never execs a binary into the api image" \
        'exec "\$API_CONTAINER"'

    expect_present_in_code "a setting it could not read is unknown, not off" \
        'unknown, the container did not say'
}

run_self_cases() {
    local code

    # This ends in the guard at the top of this file, before a single case runs,
    # so it costs one process and cannot recurse.
    printf 'self cases\n'

    "${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "a flag passed to this file refuses"
    else
        report_fail "a flag passed to this file refuses, expected exit 2, got $code"
    fi
}

main() {
    run_guard_cases
    run_read_only_cases
    run_coverage_cases
    run_fault_reading_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
