#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: library
#
# The cross-stack helper. Sourced, never executed.
#
# Each stack owns its own compose file and its own container directory, so that
# either one can be lifted out and deployed on its own. Nothing inside a stack
# reaches into the other. This library is what lets the root scripts act on both
# at once, and the root scripts are the only place that is allowed to.
#
# Usage:
#   source "<repository root>/scripts/lib/stack.sh"
#   stack_compose backend up --detach
#
# Note:
# - COMPOSE_COMMAND overrides the search when a machine needs something else,
#   for example "nerdctl compose"
# ---------------------------------------------------------------------------- #

STACK_NAMES=("backend" "frontend")

# Bind mount directories, per stack. The frontend holds no state, so it has
# none. Paths are relative to the repository root.
#
# Each one sits inside the directory of the service that writes it, next to that
# service's own configuration, rather than in a shared tree. One service's state
# is then independent of every other: it can be removed on its own, and no single
# path deletion can take all five.
STACK_DATA_DIRECTORIES=(
    "backend/containers/postgres-primary/.data"
    "backend/containers/postgres-replica/.data"
    "backend/containers/redis/.data"
    "backend/containers/prometheus/.data"
    "backend/containers/grafana/.data"
)

stack_repository_root() {
    cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
}

# The container runtime a root script talks to directly, for the few things
# compose has no answer for: waiting on a health check inside a container, or
# removing one by name.
#
# Podman is preferred when both are installed, because a rootless runtime is the
# safer of the two to hand a removal command to.
#
# Return:
# - the command name on standard output
# - exit 1 with a message when neither is installed
stack_container_runtime() {
    if [ -n "${CONTAINER_RUNTIME:-}" ]; then
        printf '%s\n' "$CONTAINER_RUNTIME"

        return 0
    fi

    if command -v podman >/dev/null 2>&1; then
        printf 'podman\n'

        return 0
    fi

    if command -v docker >/dev/null 2>&1; then
        printf 'docker\n'

        return 0
    fi

    printf 'no container runtime found, install podman or docker\n' >&2

    return 1
}

stack_resolve_compose() {
    if [ -n "${COMPOSE_COMMAND:-}" ]; then
        read -r -a stack_compose_command <<<"$COMPOSE_COMMAND"

        return 0
    fi

    if command -v podman-compose >/dev/null 2>&1; then
        stack_compose_command=(podman-compose)

        return 0
    fi

    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        stack_compose_command=(docker compose)

        return 0
    fi

    if command -v docker-compose >/dev/null 2>&1; then
        stack_compose_command=(docker-compose)

        return 0
    fi

    printf 'no compose command found, install podman-compose or docker compose\n' >&2

    return 1
}

# Points cAdvisor at a runtime socket this user can open, and at the image store
# under it.
#
# The compose default is the system socket, which belongs to root. A rootless
# podman answers on its own socket instead, so the default is refused with
# "statfs /run/podman/podman.sock: permission denied" and cAdvisor never starts.
# The first candidate that exists and is readable here is the right one, and a
# value already in the environment is left alone.
#
# The socket the machine offers also says which store holds the images, so the
# two are decided together rather than left to disagree.
stack_export_container_socket() {
    local candidate
    local socket
    local store

    if [ -n "${CONTAINER_SOCKET:-}" ]; then
        return 0
    fi

    for candidate in \
        "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/podman/podman.sock|${HOME}/.local/share/containers" \
        "/run/podman/podman.sock|/var/lib/containers" \
        "/var/run/docker.sock|/var/lib/docker"; do
        socket="${candidate%%|*}"
        store="${candidate##*|}"

        if [ ! -S "$socket" ] || [ ! -r "$socket" ]; then
            continue
        fi

        CONTAINER_SOCKET="$socket"
        export CONTAINER_SOCKET

        if [ -z "${CONTAINER_STORAGE_DIR:-}" ]; then
            CONTAINER_STORAGE_DIR="$store"
            export CONTAINER_STORAGE_DIR
        fi

        return 0
    done

    return 0
}

# Runs a compose command against one stack's own file.
#
# Param:
# stack - string (backend or frontend)
# ... - the compose arguments, for example: up --detach
stack_compose() {
    local stack="$1"
    local root

    shift

    root="$(stack_repository_root)"
    stack_resolve_compose || return 1
    stack_export_container_socket

    "${stack_compose_command[@]}" --file "$root/$stack/compose.yml" "$@"
}

# Turns the stack argument every cross-stack script accepts into a list of
# names, one per line.
#
# Param:
# requested - string (all, backend, or frontend. Defaults to all)
#
# Return:
# - the selected names on standard output
# - exit 1 with a message when the name is not one of the three
stack_selection() {
    local requested="${1:-all}"

    case "$requested" in
        all)
            printf '%s\n' "${STACK_NAMES[@]}"
            ;;
        backend | frontend)
            printf '%s\n' "$requested"
            ;;
        *)
            printf 'unknown stack %q, expected all, backend, or frontend\n' "$requested" >&2

            return 1
            ;;
    esac
}

# Creates the bind mount directories before compose does.
#
# The mode is deliberately open. Each container creates its real data directory
# inside one of these and locks that one down as its own user, which is what
# keeps the same compose file working under rootful Docker and rootless Podman.
# Nothing but local development state ever lands here.
stack_prepare_data_directories() {
    local stack="$1"
    local root
    local path

    root="$(stack_repository_root)"

    for path in "${STACK_DATA_DIRECTORIES[@]}"; do
        case "$path" in
            "$stack"/*) ;;
            *) continue ;;
        esac

        mkdir -p "$root/$path"
        chmod 0777 "$root/$path"
    done
}
