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

# The runtime that has the primary container, not merely one that is installed.
#
# A machine carrying both podman and docker started the stack with whichever
# compose it found, and the other one has never seen those containers. Asking it
# gets "no such container" from every call, which reads as a database that is
# not running rather than as the wrong question.
#
# Return:
# - the command name on standard output
# - exit 1 with a message when neither runtime is installed
database_runtime() {
    local candidate

    if [ -n "${CONTAINER_RUNTIME:-}" ]; then
        printf '%s\n' "$CONTAINER_RUNTIME"

        return 0
    fi

    for candidate in podman docker; do
        if command -v "$candidate" >/dev/null 2>&1 &&
            "$candidate" container inspect "$DATABASE_CONTAINER" >/dev/null 2>&1; then
            printf '%s\n' "$candidate"

            return 0
        fi
    done

    # Neither has it, so the container is not there and every caller is about to
    # say so. Name one that exists, so that message comes from a real command.
    for candidate in podman docker; do
        if command -v "$candidate" >/dev/null 2>&1; then
            printf '%s\n' "$candidate"

            return 0
        fi
    done

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
