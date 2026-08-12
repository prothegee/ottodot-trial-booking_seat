#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: automation
#
# The fifth tier: the invariant under real parallel connections.
#
# It exists because the four fake tiers cannot prove the thing this project is
# about. A fake repository proves the service calls the right things in the
# right order. It cannot prove that SELECT ... FOR UPDATE serializes two
# transactions, because there is no transaction. Only real Postgres can answer
# that, so these tests carry the `containers` build tag and are skipped by
# `go test ./...` entirely.
#
# It does not start or stop anything. That belongs to scripts/test_integration.sh,
# which wraps this script in a stack. Here the rule is simpler: the stack is
# either up, or this refuses with the command that brings it up.
#
# Nothing seeded is touched. Every test creates its own scratch schema, applies
# the migrations into it, and drops the whole thing on the way out, so a proof
# run and a live demo can share one database.
#
# Usage:
#   scripts/stack_up.sh backend
#   backend/scripts/test_proof.sh
#   backend/scripts/test_proof.sh ./internal/booking/...
#
# Note:
# - DATABASE_PRIMARY_URL and DATABASE_REPLICA_URL override where it connects.
#   Unset means the local pair on 5432 and 5433
#
# Return:
# - 0: the proof tier passed
# - 1: the primary is not reachable, or a test failed
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$backend_root/scripts/lib/database.sh"

cd "$backend_root"

main() {
    local packages=("$@")

    if [ "${#packages[@]}" -eq 0 ]; then
        packages=("./...")
    fi

    database_require_running

    # -count=1 defeats the test result cache, and -p 1 runs one package at a
    # time. Both matter here for the same reason: these tests share one Postgres
    # instance, and packages running in parallel would contend for connections
    # the race tests are measuring.
    printf 'running the proof tier against %s\n' "$DATABASE_CONTAINER"
    go test -tags=containers -count=1 -p 1 "${packages[@]}"
}

main "$@"
