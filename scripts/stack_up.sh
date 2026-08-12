#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Brings a stack up and waits until it is usable. It only creates and starts
# things, so it never prompts.
#
# Each stack has its own compose file and can be started on its own. This script
# exists to start both in one command, which is the kind of cross-stack work
# only the root scripts directory is allowed to do.
#
# Usage:
#   scripts/stack_up.sh              (both stacks)
#   scripts/stack_up.sh backend      (databases and cache only)
#   scripts/stack_up.sh frontend     (the served bundle only)
#
# Note:
# - the frontend container and frontend/scripts/dev.sh both want port 9001, so
#   run one or the other, never both
# - it stamps the images it builds with this checkout's commit, so a running
#   service can say which source it came from. Set BUILD_COMMIT or BUILD_TIME
#   yourself to hand in different values
#
# Next steps it does not take for you:
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"

readiness_attempts="${STACK_READINESS_ATTEMPTS:-60}"

# The backend services that have to be running for the stack to be usable.
#
# cAdvisor is deliberately absent. It reads a container runtime socket a machine
# may not offer, and without it the panels degrade from per container to host
# wide rather than going dark.
backend_required_containers=(
    ottodot-postgres-primary
    ottodot-postgres-replica
    ottodot-redis
    ottodot-api
    ottodot-worker
    ottodot-prometheus
    ottodot-grafana
    ottodot-node-exporter
)

container_is_running() {
    [ "$("$1" inspect --format '{{.State.Running}}' "$2" 2>/dev/null)" = "true" ]
}

# A real query, over tcp, rather than pg_isready.
#
# Two states answer pg_isready and cannot serve a request, and both of them end
# with the migration this script tells you to run next reporting something
# impossible about the database:
#
#	on a first ever start the image builds the database with a temporary server
#	of its own, which listens on the unix socket alone
#
#	after a cleanup that removed the data directory but not the container, the
#	server keeps running on files that are no longer there
#
# Selecting one value costs nothing and rules out both, because a server that
# cannot read its own catalogue cannot answer it.
wait_for_primary() {
    local runtime="$1"
    local attempt=1

    while [ "$attempt" -le "$readiness_attempts" ]; do
        if "$runtime" exec --env PGPASSWORD="${POSTGRES_PASSWORD:-ottodot_development}" \
            ottodot-postgres-primary psql --quiet --tuples-only --no-align \
            --host 127.0.0.1 --username "${POSTGRES_USER:-ottodot}" --dbname "${POSTGRES_DB:-ottodot}" \
            --command 'select 1' >/dev/null 2>&1; then
            printf 'primary is accepting connections\n'

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf 'primary did not become ready within %s seconds\n' "$readiness_attempts" >&2

    return 1
}

wait_for_replica() {
    local runtime="$1"
    local attempt=1
    local state=""

    while [ "$attempt" -le "$readiness_attempts" ]; do
        state="$("$runtime" exec ottodot-postgres-replica psql --tuples-only --no-align \
            --username "${POSTGRES_USER:-ottodot}" --dbname "${POSTGRES_DB:-ottodot}" \
            --command 'select pg_is_in_recovery()' 2>/dev/null || true)"

        if [ "$state" = "t" ]; then
            printf 'replica is streaming from the primary\n'

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf 'replica did not reach standby state within %s seconds\n' "$readiness_attempts" >&2

    return 1
}

wait_for_frontend() {
    local attempt=1

    while [ "$attempt" -le "$readiness_attempts" ]; do
        if curl --silent --fail --output /dev/null http://127.0.0.1:9001/ 2>/dev/null; then
            printf 'frontend is serving on 9001\n'

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf 'frontend did not answer on 9001 within %s seconds\n' "$readiness_attempts" >&2

    return 1
}

stamp_build_identity() {
    # An image is built from a copy of the source without the repository, so a
    # container has no way to work out which commit it came from. This is the
    # last place that still knows, so it writes both values down for the builds
    # that follow. A value already in the environment is left alone, which is how
    # a pipeline hands in its own.
    if [ -z "${BUILD_COMMIT:-}" ]; then
        BUILD_COMMIT="$(git -C "$repository_root" rev-parse --short=7 HEAD 2>/dev/null || true)"
        export BUILD_COMMIT
    fi

    if [ -z "${BUILD_TIME:-}" ]; then
        BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        export BUILD_TIME
    fi

    printf 'stamping images with commit %s at %s\n' "${BUILD_COMMIT:-unknown}" "$BUILD_TIME"
}

# Starts the backend containers, and lets the one optional service refuse.
#
# compose reports the whole call failed when a single container will not start,
# and under set -e that ended this script where it stood: cAdvisor, which is
# allowed to fail, took the frontend down with it before it was ever started.
#
# compose's exit code is therefore not the question. Whether the services the
# stack needs are running is, so that is what is asked.
start_the_backend_containers() {
    local runtime="$1"
    local name
    local missing=()

    # --build, like the frontend. Without it a source change is invisible: the
    # image already exists, compose starts it, and the stack comes up healthy
    # running the code from before the edit.
    if stack_compose backend up --detach --build; then
        return 0
    fi

    for name in "${backend_required_containers[@]}"; do
        if ! container_is_running "$runtime" "$name"; then
            missing+=("$name")
        fi
    done

    if [ "${#missing[@]}" -gt 0 ]; then
        printf 'backend did not start: %s\n' "${missing[*]}" >&2

        return 1
    fi

    printf 'cadvisor did not start, every other backend service did\n' >&2
    printf 'per container panels stay blank until it does, see backend/how-to.md\n' >&2

    return 0
}

start_backend() {
    local runtime="$1"

    printf '\nbackend: preparing each service bind mount under backend/containers/\n'
    stack_prepare_data_directories backend

    printf 'backend: starting, this builds the api and the worker on the first run\n'
    start_the_backend_containers "$runtime"

    wait_for_primary "$runtime"
    wait_for_replica "$runtime"

    # Every service this call starts, not only the three it waited for. A list
    # shorter than the stack reads as though the rest did not come up.
    printf '  postgres primary  127.0.0.1:5432\n'
    printf '  postgres replica  127.0.0.1:5433\n'
    printf '  redis             127.0.0.1:6379\n'
    printf '  api               127.0.0.1:9000\n'
    printf '  worker metrics    127.0.0.1:9002\n'
    printf '  prometheus        127.0.0.1:9003\n'
    printf '  grafana           127.0.0.1:9004 (sign in: admin / admin)\n'
    printf '  node exporter     127.0.0.1:9005\n'
    printf '  cadvisor          127.0.0.1:9006 (allowed to fail, see backend/how-to.md)\n'
    printf 'apply the schema with backend/scripts/migrate.sh\n'
}

start_frontend() {
    printf '\nfrontend: starting, this builds the bundle on the first run\n'
    stack_compose frontend up --detach --build

    wait_for_frontend

    printf '  frontend          127.0.0.1:9001\n'
}

main() {
    local selection
    local runtime
    local stack

    selection="$(stack_selection "${1:-all}")" || exit 2
    runtime="$(stack_container_runtime)" || exit 2

    stamp_build_identity

    while read -r stack; do
        case "$stack" in
            backend) start_backend "$runtime" ;;
            frontend) start_frontend ;;
        esac
    done <<<"$selection"
}

main "$@"
