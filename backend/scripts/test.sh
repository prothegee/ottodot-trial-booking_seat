#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# The four fake tiers: unit, edge, integration, and behaviour. Nothing needs to
# be running. No database, no cache, no containers.
#
# Three steps, in the order a failure is cheapest to read. A build failure is
# one compiler message, a vet failure is one line, and a test failure is a whole
# suite to page through. Reaching the third means the first two are already
# ruled out.
#
# This is the same set continuous integration runs, called from the same file,
# so there is no second definition of what a green backend means.
#
# The proof tier is not here. It needs a real Postgres and it lives in
# scripts/test_proof.sh.
#
# Usage:
#   backend/scripts/test.sh
#
# Return:
# - 0: every tier passed
# - non-zero: the step that failed, with its own output
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$backend_root"

main() {
    printf 'building\n'
    go build ./...

    printf 'vetting\n'
    go vet ./...

    # -count=1 defeats the test result cache. A suite that passes because it was
    # green an hour ago has not been run.
    printf 'running the four fake tiers\n'
    go test -count=1 ./...
}

main "$@"
