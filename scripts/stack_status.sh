#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# What is running, and whether it is in a state the test runs can use.
#
# It reads and prints. Nothing here starts, stops, builds, or removes anything,
# so it is safe at any moment, including while something else is still starting.
#
# The container names come from each stack's own compose file rather than from a
# list kept here, so a service added there appears here without this file being
# touched. A list copied into a status command is the one nobody remembers to
# update, and a status command that omits a service reports on health it never
# looked at.
#
# The second half is why this exists rather than `docker ps`. Every container can
# be up while the tests still cannot run: an unmigrated database fails the read
# routing proof, and an api started without the fault surface cannot run test 16.
# Both are read here, so one command answers what is missing.
#
# Usage:
#   scripts/stack_status.sh           (both stacks)
#   scripts/stack_status.sh backend
#   scripts/stack_status.sh frontend
#
# Note:
# - cAdvisor is optional and its absence never changes the exit code. It needs a
#   runtime socket this user can open, and not every machine offers one
#
# Return:
# - 0: every required container is up
# - 1: at least one required container is not up
# - 2: an unknown stack name
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"
source "$repository_root/backend/scripts/lib/database.sh"

# Not required, and said once here rather than at each place that skips it.
OPTIONAL_CONTAINER="ottodot-cadvisor"

# The api, read for the one setting that decides whether test 16 has anything to
# break, and the primary, which is the only container asked about its contents.
API_CONTAINER="ottodot-api"
PRIMARY_CONTAINER="ottodot-postgres-primary"

missing=0

# One call to the runtime for the whole picture rather than one per container.
# It saves eight processes, and it also means every line below describes the
# same moment rather than nine moments in a row.
runtime_snapshot=""

read_the_runtime() {
    local runtime

    runtime="$(stack_container_runtime)" || return 1

    runtime_snapshot="$("$runtime" ps --all --format '{{.Names}}|{{.Status}}' 2>/dev/null)"
}

# The container names one stack's compose file declares, in file order.
declared_containers() {
    local stack="$1"

    awk '/^[[:space:]]+container_name:/ { print $2 }' "$repository_root/$stack/compose.yml"
}

# The runtime's own words for a container, empty when it was never created.
status_of() {
    printf '%s\n' "$runtime_snapshot" | awk -F'|' -v want="$1" '$1 == want { print $2 }'
}

container_state() {
    local status

    status="$(status_of "$1")"

    if [ -z "$status" ]; then
        printf 'absent\n'

        return 0
    fi

    case "$status" in
        Up*) printf 'up\n' ;;
        *) printf 'down\n' ;;
    esac
}

# The runtime's health wording is kept rather than reworded, so a line here can
# be matched against `docker ps` without a translation in between.
container_health() {
    case "$(status_of "$1")" in
        *"(healthy)"*) printf 'healthy\n' ;;
        *"(unhealthy)"*) printf 'unhealthy\n' ;;
        *"(starting)"*) printf 'starting\n' ;;
    esac
}

print_one_container() {
    local name="$1"
    local state
    local note

    state="$(container_state "$name")"
    note="$(container_health "$name")"

    if [ "$name" = "$OPTIONAL_CONTAINER" ] && [ "$state" != "up" ]; then
        note="optional, never required"
    elif [ "$state" != "up" ]; then
        missing=$((missing + 1))
    fi

    if [ -z "$note" ]; then
        printf '  %-26s %s\n' "$name" "$state"

        return 0
    fi

    printf '  %-26s %-8s %s\n' "$name" "$state" "$note"
}

print_one_stack() {
    local stack="$1"
    local name
    local defined=0
    local running=0

    printf '\n%s\n' "$stack"

    while read -r name; do
        defined=$((defined + 1))

        if [ "$(container_state "$name")" = "up" ]; then
            running=$((running + 1))
        fi

        print_one_container "$name"
    done < <(declared_containers "$stack")

    printf '  %s of %s up\n' "$running" "$defined"
}

print_the_database_readings() {
    local schema
    local parents

    schema="$(database_scalar \
        "select count(*) from information_schema.tables where table_name = 'trial_classes'" 2>/dev/null)"

    if [ "$schema" != "1" ]; then
        printf '  %-24s %s\n' "the schema" "not applied, run backend/scripts/migrate.sh"
        printf '  %-24s %s\n' "the seed" "waiting on the schema"

        return 0
    fi

    printf '  %-24s %s\n' "the schema" "applied"

    parents="$(database_scalar "select count(*) from parents" 2>/dev/null)"

    if [ "${parents:-0}" = "0" ]; then
        printf '  %-24s %s\n' "the seed" "empty, run backend/scripts/seed.sh"

        return 0
    fi

    printf '  %-24s %s\n' "the seed" "$parents parent(s)"
}

# Read from the container's recorded configuration rather than by asking the api,
# which only answers that question to a signed in admin. A status command should
# not need an account to say what is switched on.
#
# It is read with inspect rather than by running printenv inside the container.
# That image carries the binary and nothing else, so there is no printenv in it
# to run, and an exec that fails returns the same empty string as a setting that
# is off. Reporting one as the other is the sort of wrong answer a status command
# has no business giving.
print_the_fault_reading() {
    local runtime
    local setting

    if [ "$(container_state "$API_CONTAINER")" != "up" ]; then
        printf '  %-24s %s\n' "the fault surface" "unknown, the api is not up"

        return 0
    fi

    runtime="$(stack_container_runtime)" || return 0

    setting="$("$runtime" inspect "$API_CONTAINER" \
        --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null |
        awk -F= '$1 == "FAULT_INJECTION_ENABLED" { print $2 }')"

    if [ "$setting" = "true" ]; then
        printf '  %-24s %s\n' "the fault surface" "on, test 16 can run"

        return 0
    fi

    if [ -z "$setting" ]; then
        printf '  %-24s %s\n' "the fault surface" "unknown, the container did not say"

        return 0
    fi

    printf '  %-24s %s\n' "the fault surface" "off, test 16 needs it on"
    printf '  %-24s %s\n' "" "export it, then scripts/stack_restart.sh backend"
}

# The three readings that decide whether the test runs can do their work. Each is
# reported as unknown rather than guessed at when the thing that answers it is
# not there.
print_what_the_tests_need() {
    printf '\nwhat the test runs need\n'

    if [ "$(container_state "$PRIMARY_CONTAINER")" != "up" ]; then
        printf '  %-24s %s\n' "the schema" "unknown, the primary is not up"
        printf '  %-24s %s\n' "the seed" "unknown, the primary is not up"
    else
        print_the_database_readings
    fi

    print_the_fault_reading
}

main() {
    local selection
    local stack

    selection="$(stack_selection "${1:-all}")" || exit 2

    read_the_runtime || exit 1

    while read -r stack; do
        print_one_stack "$stack"
    done <<<"$selection"

    case "$selection" in
        *backend*) print_what_the_tests_need ;;
    esac

    if [ "$missing" -gt 0 ]; then
        printf '\n%s required container(s) are not up, start them with scripts/stack_up.sh\n' "$missing"

        return 1
    fi

    printf '\neverything required is up.\n'
}

main "$@"
