#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the two things scripts/lib/stack.sh works out about a machine: which
# runtime socket to hand a container, and which runtime to talk to. Nothing here
# starts a container. The library is sourced, and each function under test is
# called with a temporary directory standing in for the machine's own.
#
# The socket has to be discovered rather than assumed. The compose default is
# root's socket, and a rootless machine that takes it gets "permission denied"
# from a service that then never starts.
#
# The runtime has to be the one compose used. A machine with both podman and
# docker installed but no podman-compose starts the stack with docker compose,
# and a podman exec then cannot see a single one of those containers.
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

check_differs() {
    local name="$1"
    local rejected="$2"
    local actual="$3"

    if [ "$actual" = "$rejected" ]; then
        report_fail "$name: got '$actual'"

        return 1
    fi

    report_pass "$name"

    return 0
}

# The strongest claim a miss case can make on any machine: whatever was chosen,
# it did not come out of the directory this test handed in.
check_outside_work_directory() {
    local name="$1"
    local actual="$2"

    case "$actual" in
        "$work_directory"/*)
            report_fail "$name: took '$actual' from the temporary directory"

            return 1
            ;;
    esac

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

    # The other two candidates are absolute machine paths, and a machine that
    # really offers one discovers it: a continuous integration runner carries
    # /var/run/docker.sock, so "unset" is not what a miss looks like everywhere.
    resolved="$(resolve_with "$work_directory/empty")"

    check_outside_work_directory "an empty runtime directory yields no socket" "${resolved%% *}"
    check_differs "no rootless socket means no rootless store" "${HOME}/.local/share/containers" "${resolved##* }"
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

    check_outside_work_directory "a regular file is not taken for a socket" "${resolved%% *}"
}

# Calls the runtime resolver with a stand-in command ahead of the real ones on
# PATH, so no case is decided by what this machine happens to have installed.
#
# Param:
# compose_command - string (the value COMPOSE_COMMAND takes for this call)
#
# Return:
# - the runtime name, or "refused" when the resolver stopped
resolve_runtime_with() {
    local compose_command="$1"

    (
        unset CONTAINER_RUNTIME
        export COMPOSE_COMMAND="$compose_command"
        export PATH="$work_directory/bin:$PATH"

        stack_container_runtime 2>/dev/null || printf 'refused\n'
    )
}

# Whichever compose starts the containers is the only runtime that can see them
# afterwards, so the name is read back out of the compose command.
run_runtime_cases() {
    printf 'runtime cases\n'

    mkdir --parents "$work_directory/bin"
    printf '#!/usr/bin/env bash\nexit 0\n' >"$work_directory/bin/ottodot-runtime"
    chmod 0755 "$work_directory/bin/ottodot-runtime"

    check_value "a compose suffix names the runtime" \
        "ottodot-runtime" "$(resolve_runtime_with 'ottodot-runtime-compose')"
    check_value "a two word compose names it as well" \
        "ottodot-runtime" "$(resolve_runtime_with 'ottodot-runtime compose')"
    check_value "a runtime that is not installed refuses" \
        "refused" "$(resolve_runtime_with 'ottodot-absent-compose')"

    check_value "a runtime already set is left alone" "/somewhere/of/my/own" "$(
        export CONTAINER_RUNTIME=/somewhere/of/my/own
        export COMPOSE_COMMAND='ottodot-runtime-compose'

        stack_container_runtime
    )"
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
    run_runtime_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"
