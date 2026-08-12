#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: library
#
# One way to reach the database, shared by migrate.sh, seed.sh, and db_reset.sh.
# Sourced, never executed.
#
# Every statement runs through psql inside the primary container. There is no
# second path that talks to the database from the host, because two paths drift
# and only one of them ever gets tested. It also means no reviewer has to
# install a Postgres client to run this project.
#
# Usage:
#   source "<repository root>/backend/scripts/lib/database.sh"
#   database_require_running
#   database_statement "select count(*) from parents"
# ---------------------------------------------------------------------------- #

DATABASE_CONTAINER="${DATABASE_CONTAINER:-ottodot-postgres-primary}"
DATABASE_USER="${POSTGRES_USER:-ottodot}"
DATABASE_NAME="${POSTGRES_DB:-ottodot}"

database_runtime() {
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

database_require_running() {
    local runtime

    runtime="$(database_runtime)" || return 1

    if ! "$runtime" exec "$DATABASE_CONTAINER" pg_isready --quiet >/dev/null 2>&1; then
        printf 'the primary container %s is not ready, run scripts/stack_up.sh first\n' "$DATABASE_CONTAINER" >&2

        return 1
    fi
}

# Runs psql with the flags every caller wants: stop on the first error, and no
# banner. Extra flags and the statement come from the caller.
database_psql() {
    local runtime

    runtime="$(database_runtime)" || return 1

    "$runtime" exec --interactive "$DATABASE_CONTAINER" \
        psql --set ON_ERROR_STOP=1 --quiet \
        --username "$DATABASE_USER" --dbname "$DATABASE_NAME" "$@"
}

database_statement() {
    database_psql --command "$1"
}

# Returns a single value with no padding and no header, for a count or a test.
database_scalar() {
    database_psql --tuples-only --no-align --command "$1"
}

database_script() {
    database_psql <"$1"
}
