#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Takes this project's local development footprint off the machine: both stacks'
# containers, the images built here, both networks, and the data directory of
# every service. What is left is a clone.
#
# It does not remove images this project did not build, since they are shared
# with whatever else on this machine uses them, and it does not remove anything
# not named literally below. No wildcard, no prune, no `-a`, because each of
# those can reach a container this project does not own.
#
# It prompts, it is development only, and it names every target before asking,
# all from scripts/lib/confirm.sh.
#
# Usage:
#   APP_ENV=development scripts/cleanup_dev.sh
#   APP_ENV=development scripts/cleanup_dev.sh --dry-run
#
# Note:
# - a target that is already gone is not a failure. This is the script somebody
#   runs when the machine is in an unknown state, so every removal tolerates the
#   thing not being there
#
# Exit codes:
# - 0: done, or the dry run finished
# - 1: declined by the operator, or a container it could not remove, which stops
#   it from deleting a data directory out from under one that is still there
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/confirm.sh"
source "$repository_root/scripts/lib/stack.sh"

# Every container both compose files create, named exactly as they name them.
CLEANUP_CONTAINERS=(
    ottodot-api
    ottodot-worker
    ottodot-postgres-primary
    ottodot-postgres-replica
    ottodot-redis
    ottodot-prometheus
    ottodot-grafana
    ottodot-node-exporter
    ottodot-cadvisor
    ottodot-frontend
)

# Only the three images this repository builds. The tag follows BUILD_VERSION
# the same way the compose files do, so a stack started with a version and
# cleaned up without one still names the same image.
#
# The fallback has to be the one backend/compose.yml uses. A different fallback
# here names an image that was never built, reports it removed because a missing
# target is not a failure, and leaves the real one on the machine.
CLEANUP_IMAGES=(
    "ottodot-api:${BUILD_VERSION:-0.1.0}"
    "ottodot-worker:${BUILD_VERSION:-0.1.0}"
    "ottodot-frontend"
)

# One network per stack, because the two stacks share nothing.
CLEANUP_NETWORKS=(
    ottodot-backend
    ottodot-frontend
)

# The bind mounts, one per service, each inside that service's own directory.
#
# They are named individually rather than as a tree above them. A single path
# that holds every service's state is a single mistake away from taking all of
# it, which is exactly what happened when one container survived this script and
# its database was deleted underneath it.
#
# The list is read from the same place compose reads it, so a service added there
# is removed here without this file being touched.
CLEANUP_DATA_PATHS=()

for cleanup_relative_path in "${STACK_DATA_DIRECTORIES[@]}"; do
    CLEANUP_DATA_PATHS+=("$repository_root/$cleanup_relative_path")
done

runtime=""

declare_targets() {
    local target

    for target in "${CLEANUP_CONTAINERS[@]}"; do
        confirm_target_name "$target" "container"
    done

    for target in "${CLEANUP_IMAGES[@]}"; do
        confirm_target_name "$target" "image"
    done

    for target in "${CLEANUP_NETWORKS[@]}"; do
        confirm_target_name "$target" "network"
    done

    local path

    for path in "${CLEANUP_DATA_PATHS[@]}"; do
        confirm_target_path "$path"
    done
}

# Containers this run was asked to remove and could not.
#
# It is a list rather than a count because of what comes next. Removing the data
# tree while a postgres still holds it leaves a container serving from files that
# are no longer there: it answers pg_isready, it fails every query, and the next
# run reports something impossible about the database instead of the truth, which
# is that this cleanup did not finish.
survivors=()

# A target that is already gone is not a failure, which is the whole reason the
# removal is tolerant. A target that is still here after being asked to go is a
# different thing entirely, and it has to be said out loud.
container_exists() {
    "$runtime" inspect "$1" >/dev/null 2>&1
}

# Whether this runtime knows --depend, asked of the runtime rather than assumed
# from its name. On this machine the docker command is a shim over podman, so the
# name is not evidence of anything.
runtime_takes_depend() {
    "$runtime" rm --help 2>/dev/null | grep --quiet -- '--depend'
}

# Removes one container, and reports why it could not be removed.
#
# Podman refuses while another container is recorded as depending on this one,
# even after that other container is gone and only the record is left. --depend
# clears the record with it. Docker keeps no such record, so it never fails this
# way and never reaches the retry.
#
# Param:
# target - string (the container name)
#
# Return:
# - 0 when the container is gone, including when it was never there
# - 1 with the runtime's own words on standard output
remove_one_container() {
    local target="$1"
    local failure

    if failure="$("$runtime" rm --force --volumes "$target" 2>&1 >/dev/null)"; then
        return 0
    fi

    if runtime_takes_depend; then
        if failure="$("$runtime" rm --force --volumes --depend "$target" 2>&1 >/dev/null)"; then
            return 0
        fi
    fi

    printf '%s' "$failure"

    return 1
}

remove_containers() {
    local target
    local failure

    for target in "${CLEANUP_CONTAINERS[@]}"; do
        if failure="$(remove_one_container "$target")"; then
            printf '  removed container %s\n' "$target"

            continue
        fi

        if ! container_exists "$target"; then
            continue
        fi

        printf '  could not remove container %s: %s\n' "$target" "$failure" >&2
        survivors+=("$target")
    done
}

remove_images() {
    local target

    for target in "${CLEANUP_IMAGES[@]}"; do
        if "$runtime" rmi --force "$target" >/dev/null 2>&1; then
            printf '  removed image %s\n' "$target"
        fi
    done
}

remove_networks() {
    local target

    for target in "${CLEANUP_NETWORKS[@]}"; do
        if "$runtime" network rm "$target" >/dev/null 2>&1; then
            printf '  removed network %s\n' "$target"
        fi
    done
}

remove_one_data_directory() {
    local path="$1"

    if [ ! -d "$path" ]; then
        return 0
    fi

    # Written by container users that a rootless runtime maps elsewhere, so some
    # of it is not the caller's to delete. The unshared attempt is tried first
    # and the plain one is the fallback, rather than reaching for sudo.
    if ! rm -rf "$path" 2>/dev/null; then
        if command -v podman >/dev/null 2>&1; then
            podman unshare rm -rf "$path"
        else
            rm -rf "$path"
        fi
    fi

    printf '  removed %s\n' "$path"
}

remove_data_directories() {
    local path

    # Refused rather than done anyway. A database whose files are deleted under
    # it keeps answering until it is asked for something it has to read, so
    # taking this half of the cleanup without the other half produces a stack
    # that looks up and is not.
    if [ "${#survivors[@]}" -gt 0 ]; then
        printf '  refusing to remove any state, these containers are still here: %s\n' "${survivors[*]}" >&2
        printf '  remove those first, then run this again\n' >&2

        return 1
    fi

    for path in "${CLEANUP_DATA_PATHS[@]}"; do
        remove_one_data_directory "$path"
    done
}

main() {
    confirm_parse_flags "$@"
    confirm_require_environment

    declare_targets
    confirm_proceed "remove this project's containers, its own images, both networks, and every byte of local state"

    runtime="$(stack_container_runtime)" || exit 2

    printf 'removing containers\n'
    remove_containers

    printf 'removing images built by this repository\n'
    remove_images

    printf 'removing networks\n'
    remove_networks

    printf 'removing local state\n'

    if ! remove_data_directories; then
        exit 1
    fi

    printf '\nthis machine is back to a fresh clone, start again with scripts/stack_up.sh\n'
}

main "$@"
