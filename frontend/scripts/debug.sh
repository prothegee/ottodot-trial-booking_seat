#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Gets this machine ready for a manual frontend run, then hands off to
# frontend/scripts/dev.sh, which owns actually running the dev server. The two
# are split so there is one definition of how the server starts, and this file
# holds only the checks a manual session keeps tripping over.
#
# What it initialises: node_modules, when the dependencies have never been
# installed here.
#
# What it will not start: an api, or the monitoring behind the dashboards. This
# stack owns exactly one container, the served bundle on 9001, and the dev server
# replaces it, so there is nothing here worth auto starting. Both of those belong
# to the backend stack, and a script in this directory is not allowed to reach
# into it. When either does not answer, this says which command provides it and
# carries on, because the failure states of the pages are worth debugging too.
#
# The frontend dashboard needs nothing from this stack in any case. Every series
# on it is published by the api: the browser posts what it did, the api counts
# it, and Prometheus scrapes the api. So a dev server here with the backend stack
# up is already a fully monitored frontend.
#
# It starts no container, so it stops none either. The one container this stack
# has is a refusal, not something to start: it holds 9001, which the dev server
# needs.
#
# Usage:
#   frontend/scripts/debug.sh
#   API_BASE_URL=http://127.0.0.1:9500 frontend/scripts/debug.sh
#
# Return:
# - 2: a guard refused before the server started
# - anything else: whatever the dev server returned, since this script hands its
#   own process over to it, so a ctrl-c reads as 130 here
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$frontend_root/.." && pwd)"

source "$repository_root/scripts/lib/settings.sh"

FRONTEND_CONTAINER="ottodot-frontend"

api_base_url="${API_BASE_URL:-http://127.0.0.1:9000}"
grafana_url="${GRAFANA_URL:-http://127.0.0.1:9004}"

refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit 2
}

# Asks each runtime that is installed about one container, by its literal name.
# No runtime installed means no container is running, which is the right answer
# for a machine that only has node on it.
frontend_container_is_running() {
    local runtime

    for runtime in podman docker; do
        if ! command -v "$runtime" >/dev/null 2>&1; then
            continue
        fi

        if [ "$("$runtime" inspect --format '{{.State.Running}}' "$FRONTEND_CONTAINER" 2>/dev/null)" = "true" ]; then
            return 0
        fi
    done

    return 1
}

require_the_port_is_free() {
    if ! frontend_container_is_running; then
        return 0
    fi

    printf 'refused: the container %s is running and holds 9001\n' "$FRONTEND_CONTAINER" >&2
    printf 'stop it with: scripts/stack_down.sh frontend\n' >&2

    exit 2
}

require_dependencies() {
    if ! command -v npm >/dev/null 2>&1; then
        refuse "npm is not installed, this script runs the dev server from source"
    fi

    if [ -d "$frontend_root/node_modules" ]; then
        return 0
    fi

    printf 'node_modules is missing, installing\n'
    (cd "$frontend_root" && npm install)
}

# A warning rather than a refusal. A frontend developer often wants exactly the
# screen an unreachable api produces, and this script has no business deciding
# which api the other stack should be running.
report_on_the_api() {
    if ! command -v curl >/dev/null 2>&1; then
        printf 'curl is not installed, skipping the api check\n'

        return 0
    fi

    if curl --silent --fail --output /dev/null --max-time 3 "$api_base_url/healthz" 2>/dev/null; then
        printf 'an api is answering at %s\n' "$api_base_url"

        return 0
    fi

    printf '\nno api is answering at %s, the pages will show their failure states\n' "$api_base_url"
    printf 'start one with either of:\n'
    printf '  scripts/stack_up.sh backend      the containerised api\n'
    printf '  backend/scripts/debug.sh         the api from source\n\n'
}

# The frontend panels are read in Grafana, and every series behind them is
# published by the api: the browser posts what it did, the api counts it, and
# Prometheus scrapes the api. So there is nothing for this stack to start, and
# nothing here reaches into the backend stack to start it either. It reports, and
# names the two commands that own it.
report_on_monitoring() {
    if ! command -v curl >/dev/null 2>&1; then
        return 0
    fi

    if curl --silent --fail --output /dev/null --max-time 3 "$grafana_url/api/health" 2>/dev/null; then
        printf 'grafana is answering at %s, the frontend dashboard is live\n' "$grafana_url"

        return 0
    fi

    printf 'no grafana at %s, so no dashboard while this runs\n' "$grafana_url"
    printf 'monitoring comes up with either of:\n'
    printf '  scripts/stack_up.sh backend      the whole backend stack\n'
    printf '  backend/scripts/debug.sh         the api from source, monitoring with it\n\n'
}

main() {
    if [ "$#" -gt 0 ]; then
        refuse "unknown argument '$1', this script takes none"
    fi

    settings_ensure "$frontend_root/.env" "$frontend_root/.env.template" || exit 2

    require_the_port_is_free
    require_dependencies
    report_on_the_api
    report_on_monitoring

    exec "$frontend_root/scripts/dev.sh"
}

main "$@"
