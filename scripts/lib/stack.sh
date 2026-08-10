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
STACK_DATA_DIRECTORIES=(
    "backend/.data/postgres-primary"
    "backend/.data/postgres-replica"
    "backend/.data/redis"
    "backend/.data/prometheus"
    "backend/.data/grafana"
)

stack_repository_root() {
    cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
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
