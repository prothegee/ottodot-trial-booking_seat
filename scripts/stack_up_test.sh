#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests scripts/stack_up.sh without starting anything. One case runs the script
# and stops in its argument guard, the rest read the source.
#
# The property worth pinning is the one that broke a run: cAdvisor is allowed to
# fail, compose reports the whole call failed when it does, and under set -e that
# ended the script before the frontend was ever started. A test that started
# containers to prove this would take a minute and a runtime that refuses the
# socket, so the shape of the handling is read instead.
#
# Usage:
#   scripts/stack_up_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
subject="$repository_root/scripts/stack_up.sh"
compose_file="$repository_root/backend/compose.yml"

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

# The names the script waits on, read from the script itself rather than copied
# here, so this file cannot drift into agreeing with a list that no longer
# exists.
required_containers() {
    awk '/^backend_required_containers=\(/ { inside = 1; next }
         inside && /^\)/ { exit }
         inside { gsub(/[[:space:]]/, ""); if ($0 != "") print }' "$subject"
}

# Every container the backend compose file names.
compose_containers() {
    awk '/^[[:space:]]+container_name:/ { print $2 }' "$compose_file"
}

run_optional_service_cases() {
    local required
    local name
    local absent=0

    printf 'optional service cases\n'

    required="$(required_containers)"

    if printf '%s\n' "$required" | grep --quiet '^ottodot-cadvisor$'; then
        report_fail "cadvisor is required, so its failure still ends the run"
    else
        report_pass "cadvisor is not required"
    fi

    # Everything else in the compose file has to be waited on. A service added
    # there and forgotten here would be reported as started without anyone
    # having looked.
    while read -r name; do
        case "$name" in
            ottodot-cadvisor) continue ;;
        esac

        if ! printf '%s\n' "$required" | grep --quiet "^$name\$"; then
            report_fail "$name is in the compose file and not in the required list"
            absent=$((absent + 1))
        fi
    done < <(compose_containers)

    if [ "$absent" -eq 0 ]; then
        report_pass "every other backend service is required"
    fi

    if grep --quiet 'if stack_compose backend up --detach --build; then' "$subject"; then
        report_pass "a failed compose call is inspected rather than fatal"
    else
        report_fail "a failed compose call ends the script before the frontend starts"
    fi

    if grep --quiet 'backend did not start' "$subject"; then
        report_pass "a service that is required and missing is named"
    else
        report_fail "a missing required service is not reported"
    fi
}

run_guard_cases() {
    local output
    local code

    printf 'guard cases\n'

    output="$("$subject" nonsense 2>&1 </dev/null)"
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "an unknown stack refuses before anything is started"
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

    run_optional_service_cases
    run_guard_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
