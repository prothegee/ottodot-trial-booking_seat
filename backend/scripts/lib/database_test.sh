#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests how backend/scripts/lib/database.sh picks the container runtime it talks
# to. Nothing here starts a container or touches a database: the library is
# sourced, and stand-in commands ahead of the real ones on PATH answer for the
# runtimes, so no case depends on what this machine has installed.
#
# The property worth pinning is that the runtime is the one holding the primary
# container. A machine carrying both podman and docker started the stack with
# whichever compose it found, and asking the other one answers "no such
# container" for every statement migrate.sh and seed.sh try to run.
#
# Usage:
#   backend/scripts/lib/database_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

library_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$library_directory/database.sh"

work_directory=""
passed=0
failed=0

cleanup() {
    if [ -n "$work_directory" ] && [ -d "$work_directory" ]; then
        rm -rf "$work_directory"
    fi
}

trap cleanup EXIT

report_pass() {
    printf '  pass: %s\n' "$1"
    passed=$((passed + 1))
}

report_fail() {
    printf '  FAIL: %s\n' "$1" >&2
    failed=$((failed + 1))
}

check_value() {
    local name="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" != "$expected" ]; then
        report_fail "$name: expected '$expected', got '$actual'"

        return 1
    fi

    report_pass "$name"

    return 0
}

check_contains() {
    local name="$1"
    local needle="$2"
    local haystack="$3"

    case "$haystack" in
        *"$needle"*)
            report_pass "$name"

            return 0
            ;;
    esac

    report_fail "$name: '$needle' is not in '$haystack'"

    return 1
}

# Writes a stand-in runtime that records what it was asked and then answers the
# way this case needs, so a test can say which runtime holds the container.
#
# Param:
# directory - string (the stand-in PATH entry to write into)
# name - string (podman or docker)
# verdict - string (0 when this runtime has the container, 1 when it does not)
create_runtime() {
    local directory="$1"
    local name="$2"
    local verdict="$3"

    mkdir --parents "$directory"

    # /bin/sh rather than the usual env line: PATH holds this directory alone
    # while a case runs, so an interpreter looked up on PATH would not be found.
    {
        printf '#!/bin/sh\n'
        printf 'printf "%%s %%s\\n" "%s" "$*" >>"%s/asked.log"\n' "$name" "$work_directory"
        printf 'exit %s\n' "$verdict"
    } >"$directory/$name"

    chmod 0755 "$directory/$name"
}

# Param:
# podman_verdict - string (0 when podman holds the container, 1 when it does not)
# docker_verdict - string (0 when docker holds it, 1 when it does not)
#
# Return:
# - the stand-in PATH entry holding both
create_machine() {
    local directory="$work_directory/machine-$1$2"

    create_runtime "$directory" podman "$1"
    create_runtime "$directory" docker "$2"

    printf '%s\n' "$directory"
}

# Return:
# - the runtime name, or "refused" when the resolver stopped
resolve_with() {
    local machine="$1"

    (
        unset CONTAINER_RUNTIME
        export PATH="$machine"

        database_runtime 2>/dev/null || printf 'refused\n'
    )
}

run_selection_cases() {
    printf 'selection cases\n'

    : >"$work_directory/asked.log"

    check_value "the runtime holding the container is chosen" \
        "docker" "$(resolve_with "$(create_machine 1 0)")"
    check_value "podman is chosen when podman holds it" \
        "podman" "$(resolve_with "$(create_machine 0 1)")"
    check_contains "the container is named in the question" \
        "container inspect ottodot-postgres-primary" "$(<"$work_directory/asked.log")"
}

# Neither runtime has the container when the stack is down, and that is the case
# every caller is written to report. It needs a runtime name to report it with.
run_absent_cases() {
    printf 'absent cases\n'

    check_value "neither holding it still names an installed one" \
        "podman" "$(resolve_with "$(create_machine 1 1)")"

    mkdir --parents "$work_directory/machine-bare"

    check_value "no runtime at all refuses" \
        "refused" "$(resolve_with "$work_directory/machine-bare")"

    check_contains "the refusal says what to install" "install podman or docker" "$(
        unset CONTAINER_RUNTIME
        export PATH="$work_directory/machine-bare"

        database_runtime 2>&1 >/dev/null
    )"
}

# A machine that already carries this value is telling the repository where its
# runtime lives, and no probe has anything to add to that.
run_override_cases() {
    printf 'override cases\n'

    check_value "a runtime already set is left alone" "/somewhere/of/my/own" "$(
        export CONTAINER_RUNTIME=/somewhere/of/my/own
        export PATH="$(create_machine 0 0)"

        database_runtime
    )"
}

# The container is not ready when no runtime can reach it, and the message has
# to say which container and what to run, or the reader is left guessing.
run_readiness_cases() {
    local refusal

    printf 'readiness cases\n'

    refusal="$(
        unset CONTAINER_RUNTIME
        export PATH="$(create_machine 1 1)"

        database_require_running 2>&1 >/dev/null
    )"

    check_contains "the refusal names the container" "ottodot-postgres-primary" "$refusal"
    check_contains "the refusal names the next step" "scripts/stack_up.sh" "$refusal"
}

main() {
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    work_directory="$(mktemp --directory)"

    run_selection_cases
    run_absent_cases
    run_override_cases
    run_readiness_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
