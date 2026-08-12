#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the socket discovery in scripts/lib/stack.sh. Nothing here starts a
# container: the library is sourced, and the function under test is called with
# a temporary directory standing in for the runtime's own.
#
# The property worth pinning is that the socket is discovered rather than
# assumed. The compose default is root's socket, and a rootless machine that
# takes it gets "permission denied" from a service that then never starts.
#
# Usage:
#   scripts/lib/stack_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

library_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

source "$library_directory/stack.sh"

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

# A real socket, because the function tests for one. A regular file at the same
# path is what a stale mount leaves behind, and handing that to a container is
# the failure this check exists to keep out.
#
# Param:
# path - string (where to bind it)
create_socket() {
    python3 -c 'import socket, sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$1"
}

# Calls the function under test with a clean environment every time, so no case
# can be decided by what the case before it exported.
#
# Param:
# runtime_directory - string (the value XDG_RUNTIME_DIR takes for this call)
#
# Return:
# - the socket and the storage directory on one line, or "unset" for either
resolve_with() {
    local runtime_directory="$1"

    (
        unset CONTAINER_SOCKET CONTAINER_STORAGE_DIR
        export XDG_RUNTIME_DIR="$runtime_directory"

        stack_export_container_socket

        printf '%s %s\n' "${CONTAINER_SOCKET:-unset}" "${CONTAINER_STORAGE_DIR:-unset}"
    )
}

run_discovery_cases() {
    local rootless_socket="$work_directory/runtime/podman/podman.sock"
    local resolved

    printf 'discovery cases\n'

    mkdir --parents "$work_directory/runtime/podman" "$work_directory/empty"
    create_socket "$rootless_socket"

    resolved="$(resolve_with "$work_directory/runtime")"

    check_value "the rootless socket is found" "$rootless_socket" "${resolved%% *}"
    check_value "the store matches the socket" "${HOME}/.local/share/containers" "${resolved##* }"

    resolved="$(resolve_with "$work_directory/empty")"

    check_value "no socket leaves the compose default alone" "unset" "${resolved%% *}"
    check_value "no socket leaves the store alone" "unset" "${resolved##* }"
}

# A machine that already carries these values is telling this repository where
# its runtime lives, and discovery has nothing to add to that.
run_override_cases() {
    local resolved

    printf 'override cases\n'

    resolved="$(
        export CONTAINER_SOCKET=/somewhere/of/my/own.sock
        export CONTAINER_STORAGE_DIR=/somewhere/of/my/own
        export XDG_RUNTIME_DIR="$work_directory/runtime"

        stack_export_container_socket

        printf '%s %s\n' "$CONTAINER_SOCKET" "$CONTAINER_STORAGE_DIR"
    )"

    check_value "a socket already set is left alone" "/somewhere/of/my/own.sock" "${resolved%% *}"
    check_value "a store already set is left alone" "/somewhere/of/my/own" "${resolved##* }"
}

# A regular file where a socket belongs is not a socket, and mounting one gives
# the service a file it cannot speak to.
run_shape_cases() {
    local resolved

    printf 'shape cases\n'

    mkdir --parents "$work_directory/stale/podman"
    printf 'not a socket\n' >"$work_directory/stale/podman/podman.sock"

    resolved="$(resolve_with "$work_directory/stale")"

    check_value "a regular file is not taken for a socket" "unset" "${resolved%% *}"
}

main() {
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    work_directory="$(mktemp --directory)"

    run_discovery_cases
    run_override_cases
    run_shape_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
