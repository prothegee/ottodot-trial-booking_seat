#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Runs one backend process from source, in the foreground, against real
# containers. The code being edited is the code that is running, so a print
# statement shows up immediately and a debugger can attach.
#
# One process at a time, so the logs on screen belong to a single one:
#   backend/scripts/debug.sh            the api on 9000
#   backend/scripts/debug.sh worker     the worker on 9002, second terminal
#
# It starts what is not already up: postgres primary, postgres replica, redis,
# node exporter, Prometheus, and Grafana. Then it migrates, and seeds only into
# an empty database. Prometheus carries a second target at
# host.containers.internal, so the panels stay filled for a process run here.
#
# It will not start the containerised api or worker: each holds the port this
# process needs, so when one is up this refuses and says how to stop it.
#
# On exit it stops only what it started, and stopping is never a removal, so the
# next start carries the same database, queue, and history. --keep leaves even
# those running. Removing state is scripts/cleanup_dev.sh, and that one asks.
#
# Usage:
#   backend/scripts/debug.sh
#   backend/scripts/debug.sh worker
#   backend/scripts/debug.sh --keep
#   FAULT_INJECTION_ENABLED=true backend/scripts/debug.sh
#
# Return:
# - 0: the process ran until ctrl-c, or exited on its own without error
# - 1: the process itself failed
# - 2: a guard refused before anything started
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$backend_root/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"
source "$repository_root/scripts/lib/settings.sh"
source "$backend_root/scripts/lib/database.sh"

# The services this script may start, by compose service name and by container
# name. Every list is literal: nothing here is discovered from a pattern, so this
# script can only ever act on what it names.
DEBUG_DATA_SERVICES=("postgres-primary" "postgres-replica" "redis")
DEBUG_DATA_CONTAINERS=("ottodot-postgres-primary" "ottodot-postgres-replica" "ottodot-redis")

# cAdvisor is deliberately not in this list. It reports per container, the
# process being debugged is not in a container, and it is the one service allowed
# to fail, so starting it here could end the session before the process ever ran.
# The host wide panels of resources.json answer the same questions.
DEBUG_MONITORING_SERVICES=("node-exporter" "prometheus" "grafana")
DEBUG_MONITORING_CONTAINERS=("ottodot-node-exporter" "ottodot-prometheus" "ottodot-grafana")

# The two process containers, which this script never asks to start. It still has
# to name them, because Prometheus declares depends_on api and worker and compose
# raises them anyway. See correct_what_compose_raised below.
DEBUG_PROCESS_CONTAINERS=("ottodot-api" "ottodot-worker")

process_containers_before=()

readiness_attempts="${DEBUG_READINESS_ATTEMPTS:-60}"

process_name="api"
keep_containers="no"
container_runtime=""
started_containers=()
process_status=0
process_was_interrupted="no"

note_interrupt() {
    process_was_interrupted="yes"
}

refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit 2
}

parse_arguments() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            api | worker)
                process_name="$1"
                ;;
            --keep)
                keep_containers="yes"
                ;;
            *)
                refuse "unknown argument '$1', expected api, worker, or --keep"
                ;;
        esac

        shift
    done
}

# Local debugging only. The process is about to run with development defaults,
# including a throwaway signing key, so any other environment is a mistake worth
# stopping for rather than a configuration error to read later.
require_development() {
    if [ "${APP_ENV:-development}" != "development" ]; then
        refuse "APP_ENV is '$APP_ENV', this script runs a process for local debugging only"
    fi

    if ! command -v go >/dev/null 2>&1; then
        refuse "go is not installed, this script runs the process from source"
    fi
}

container_is_running() {
    [ "$("$container_runtime" inspect --format '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]
}

# The containerised twin of the process about to run holds its port, so both
# cannot exist at once.
require_the_port_is_free() {
    local twin="ottodot-$process_name"

    if container_is_running "$twin"; then
        printf 'refused: the container %s is running and holds the port this process needs\n' "$twin" >&2
        printf 'stop it with: %s stop %s\n' "$container_runtime" "$twin" >&2

        exit 2
    fi
}

# Reports which of the named containers are not running, one per line. Both
# starters below read this, so what counts as already up is decided in one place.
containers_not_running() {
    local container

    for container in "$@"; do
        if ! container_is_running "$container"; then
            printf '%s\n' "$container"
        fi
    done
}

# A running container is not the same as a usable one. A container that survived
# scripts/cleanup_dev.sh keeps serving from files that were deleted underneath
# it: it reports itself as running and fails every query.
#
# Ending it lets the compose call below start it again on a directory that
# exists. A good one answers, so it is left alone.
discard_a_database_that_cannot_serve() {
    if ! container_is_running ottodot-postgres-primary || primary_is_ready; then
        return 0
    fi

    printf 'ottodot-postgres-primary is running and cannot answer, its data was removed underneath it\n'
    printf 'ending it, so it is started again on a directory that exists\n'

    "$container_runtime" stop ottodot-postgres-primary >/dev/null 2>&1 || true
}

# Starts the data services and remembers which ones were down beforehand, since
# those are the only ones this script is allowed to stop again.
start_missing_data_services() {
    local missing=()

    discard_a_database_that_cannot_serve

    mapfile -t missing < <(containers_not_running "${DEBUG_DATA_CONTAINERS[@]}")

    if [ "${#missing[@]}" -eq 0 ]; then
        printf 'postgres primary, postgres replica, and redis are already running\n'

        return 0
    fi

    started_containers+=("${missing[@]}")

    printf 'starting: %s\n' "${missing[*]}"

    stack_prepare_data_directories backend
    stack_compose backend up --detach "${DEBUG_DATA_SERVICES[@]}"
}

# Starts the monitoring layer, so the dashboards are live against the process
# running from source and not only against a containerised one.
#
# --no-deps is what makes this safe to call here. Prometheus declares depends_on
# api and worker, so without the flag compose would start the very container this
# script already refused to run beside, and the port would be taken twice.
start_missing_monitoring_services() {
    local missing=()

    mapfile -t missing < <(containers_not_running "${DEBUG_MONITORING_CONTAINERS[@]}")

    if [ "${#missing[@]}" -eq 0 ]; then
        printf 'node exporter, prometheus, and grafana are already running\n'

        return 0
    fi

    started_containers+=("${missing[@]}")

    printf 'starting: %s\n' "${missing[*]}"

    stack_compose backend up --detach --no-deps "${DEBUG_MONITORING_SERVICES[@]}"
}

# Reports which of the named containers are running, one per line.
containers_running_among() {
    local container

    for container in "$@"; do
        if container_is_running "$container"; then
            printf '%s\n' "$container"
        fi
    done
}

was_running_before() {
    local container

    for container in "${process_containers_before[@]}"; do
        if [ "$container" = "$1" ]; then
            return 0
        fi
    done

    return 1
}

# --no-deps is asked for above and is not enough. podman-compose 1.6.0 accepts
# the flag and starts the dependencies anyway, so starting Prometheus raises the
# api and the worker, taking the port this run needs.
#
# The correction reads what the runtime reports, not what compose promised. The
# twin is stopped again, the other is kept and recorded so the same ctrl-c ends
# it.
correct_what_compose_raised() {
    local twin="ottodot-$process_name"
    local container

    for container in "${DEBUG_PROCESS_CONTAINERS[@]}"; do
        if ! container_is_running "$container" || was_running_before "$container"; then
            continue
        fi

        if [ "$container" = "$twin" ]; then
            printf 'compose raised %s despite --no-deps, stopping it again to free the port\n' "$container"
            "$container_runtime" stop "$container" >/dev/null 2>&1 || true

            continue
        fi

        printf 'compose raised %s, it does not hold a port this run needs, keeping it\n' "$container"
        started_containers+=("$container")
    done
}

# A real query, over tcp, rather than pg_isready. Two servers answer pg_isready
# and cannot serve a request:
#
# - the temporary one the image runs while it builds a new database, which
#   listens on the unix socket alone
# - one whose data directory was deleted underneath it
#
# A server that cannot read its own catalogue cannot answer a query. The password
# is handed in because tcp asks for one and the socket does not.
primary_is_ready() {
    "$container_runtime" exec --env PGPASSWORD="${POSTGRES_PASSWORD:-ottodot_development}" \
        ottodot-postgres-primary psql --quiet --tuples-only --no-align \
        --host 127.0.0.1 --username "${POSTGRES_USER:-ottodot}" --dbname "${POSTGRES_DB:-ottodot}" \
        --command 'select 1' >/dev/null 2>&1
}

replica_is_streaming() {
    local state

    state="$("$container_runtime" exec --env PGPASSWORD="${POSTGRES_PASSWORD:-ottodot_development}" \
        ottodot-postgres-replica psql --tuples-only --no-align \
        --host 127.0.0.1 --username "${POSTGRES_USER:-ottodot}" --dbname "${POSTGRES_DB:-ottodot}" \
        --command 'select pg_is_in_recovery()' 2>/dev/null || true)"

    [ "$state" = "t" ]
}

redis_answers() {
    local reply

    reply="$("$container_runtime" exec ottodot-redis redis-cli ping 2>/dev/null | tr -d '\r' || true)"

    [ "$reply" = "PONG" ]
}

wait_for_data_services() {
    local attempt=1

    while [ "$attempt" -le "$readiness_attempts" ]; do
        if primary_is_ready && replica_is_streaming && redis_answers; then
            printf 'postgres primary, postgres replica, and redis are ready\n'

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf 'the data services did not become ready within %s seconds\n' "$readiness_attempts" >&2

    return 1
}

# The gate above proves a server answered, not that the container is still there
# a moment later. A previous run of this script can still be stopping its
# containers while this one starts, so the stop lands after the check.
#
# Reading them once more turns that into a restart rather than a runtime error
# the migration cannot explain.
settle_the_data_services() {
    local missing=()

    mapfile -t missing < <(containers_not_running "${DEBUG_DATA_CONTAINERS[@]}")

    if [ "${#missing[@]}" -eq 0 ]; then
        return 0
    fi

    printf 'these answered and then went away, starting them again: %s\n' "${missing[*]}"

    started_containers+=("${missing[@]}")

    stack_compose backend up --detach "${DEBUG_DATA_SERVICES[@]}"

    wait_for_data_services
}

# Migrations always, because applying nothing is what an up to date schema
# looks like. Seed only into an empty database, because seed.sh refuses over
# existing rows and a refusal here would read like a failure.
prepare_the_database() {
    local existing_parents

    "$backend_root/scripts/migrate.sh"

    existing_parents="$(database_scalar 'select count(*) from parents')"

    if [ "$existing_parents" = "0" ]; then
        # The flag rather than the prompt: this script has already been run
        # deliberately against a development stack, and stopping here to ask
        # about the demo accounts would break the one command a reviewer runs.
        "$backend_root/scripts/seed.sh" --generate-demo-users

        return 0
    fi

    printf 'data is already there, %s parent(s), leaving it alone\n' "$existing_parents"
}

# Every address points at a published port on this machine, because the process
# is running here and not on the container network. The credentials come from
# the same variables compose reads, so overriding one moves both together.
#
# These are the fallbacks and not the settings. config.json wins over every one
# of them, so a value here is what the process uses only when the file leaves it
# out. The database scripts above read the same variables, which is the reason
# they are exported rather than written into the file.
export_process_settings() {
    local user="${POSTGRES_USER:-ottodot}"
    local password="${POSTGRES_PASSWORD:-ottodot_development}"
    local name="${POSTGRES_DB:-ottodot}"

    export APP_ENV="${APP_ENV:-development}"
    export DATABASE_PRIMARY_URL="${DATABASE_PRIMARY_URL:-postgres://$user:$password@127.0.0.1:5432/$name?sslmode=disable}"
    export DATABASE_REPLICA_URL="${DATABASE_REPLICA_URL:-postgres://$user:$password@127.0.0.1:5433/$name?sslmode=disable}"
    export REDIS_ADDRESS="${REDIS_ADDRESS:-127.0.0.1:6379}"
    export ALLOWED_ORIGINS="${ALLOWED_ORIGINS:-http://127.0.0.1:9001,http://localhost:9001}"
    export COOKIE_SECURE="${COOKIE_SECURE:-false}"
    export FAULT_INJECTION_ENABLED="${FAULT_INJECTION_ENABLED:-false}"
}

announce() {
    printf '\nrunning cmd/%s from source, ctrl-c to stop\n' "$process_name"

    case "$process_name" in
        api)
            printf '  api               127.0.0.1:9000\n'
            printf '  faults            %s\n' "$FAULT_INJECTION_ENABLED"
            ;;
        worker)
            printf '  worker metrics    127.0.0.1:9002\n'
            ;;
    esac

    printf '  primary           127.0.0.1:5432\n'
    printf '  replica           127.0.0.1:5433\n'
    printf '  redis             127.0.0.1:6379\n'

    printf '  prometheus        127.0.0.1:9003\n'
    printf '  grafana           127.0.0.1:9004\n'

    # Named because it is the one row that stays blank here. cAdvisor reports per
    # container and this process is not one, so the container panels have nothing
    # to say about it, while resources.json answers the same questions host wide.
    printf '\n  no per container panels, this process is not a container\n\n'
}

# Runs on every exit, including ctrl-c and a refusal. It stops by name, one
# container at a time, and only names this run put in the list.
#
# stop and never rm. The container keeps its identity and its bind mounts, so the
# database, the queue, and the Prometheus history are all still there on the next
# start. Removing anything is scripts/cleanup_dev.sh, and that one asks first.
stop_started_containers() {
    local container

    if [ "${#started_containers[@]}" -eq 0 ]; then
        return 0
    fi

    if [ "$keep_containers" = "yes" ]; then
        printf '\nleaving running, --keep was passed: %s\n' "${started_containers[*]}"

        return 0
    fi

    printf '\nstopping what this run started, nothing is removed: %s\n' "${started_containers[*]}"

    for container in "${started_containers[@]}"; do
        "$container_runtime" stop "$container" >/dev/null 2>&1 || true
    done

    printf 'the containers still exist, and the data directory of each one was left alone\n'
}

main() {
    parse_arguments "$@"
    require_development

    container_runtime="$(stack_container_runtime)" || exit 2

    require_the_port_is_free

    trap stop_started_containers EXIT

    # Before anything is started. A missing settings file is the first thing a
    # fresh clone runs into, and finding out after the containers are up costs a
    # cleanup nobody asked for.
    settings_ensure "$backend_root/config.json" "$backend_root/config.json.template" || exit 2

    start_missing_data_services
    wait_for_data_services
    settle_the_data_services
    prepare_the_database

    # After the database, because Prometheus keeps scraping whether or not the
    # process is up and there is nothing to gain by starting it any earlier.
    #
    # The snapshot is taken first so the correction can tell a container compose
    # raised from one that was already somebody else's.
    mapfile -t process_containers_before < <(containers_running_among "${DEBUG_PROCESS_CONTAINERS[@]}")

    start_missing_monitoring_services
    correct_what_compose_raised

    export_process_settings
    announce

    cd "$backend_root"

    # Installed here and not earlier. Until this point an interrupt should end
    # the script the default way, so a ctrl-c while the databases are still
    # coming up stops rather than carrying on to a process nobody waited for.
    #
    # go run reports the same exit 1 whether the program crashed or was
    # interrupted, so this flag is what tells the two apart.
    trap note_interrupt INT

    # -buildvcs=true is what makes this process able to name its own commit.
    # `go build` records the revision on its own, `go run` does not record it
    # unless asked, and without the record the third source in the build identity
    # chain has nothing to answer with and /version says "unknown".
    go run -buildvcs=true "./cmd/$process_name" || process_status="$?"

    if [ "$process_was_interrupted" = "yes" ]; then
        return 0
    fi

    return "$process_status"
}

main "$@"
