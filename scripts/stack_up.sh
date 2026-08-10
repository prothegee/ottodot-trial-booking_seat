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
#
# Next steps it does not take for you:
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"

readiness_attempts="${STACK_READINESS_ATTEMPTS:-60}"

container_runtime() {
    if command -v podman >/dev/null 2>&1; then
        printf 'podman\n'

        return 0
    fi

    printf 'docker\n'
}

wait_for_primary() {
    local runtime="$1"
    local attempt=1

    while [ "$attempt" -le "$readiness_attempts" ]; do
        if "$runtime" exec ottodot-postgres-primary pg_isready --quiet >/dev/null 2>&1; then
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

start_backend() {
    local runtime="$1"

    printf '\nbackend: preparing bind mount directories under backend/.data/\n'
    stack_prepare_data_directories backend

    printf 'backend: starting\n'
    stack_compose backend up --detach

    wait_for_primary "$runtime"
    wait_for_replica "$runtime"

    printf '  postgres primary  127.0.0.1:5432\n'
    printf '  postgres replica  127.0.0.1:5433\n'
    printf '  redis             127.0.0.1:6379\n'
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
    runtime="$(container_runtime)"

    while read -r stack; do
        case "$stack" in
            backend) start_backend "$runtime" ;;
            frontend) start_frontend ;;
        esac
    done <<<"$selection"
}

main "$@"
