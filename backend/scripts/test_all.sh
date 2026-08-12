#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: automation
#
# Every backend test, with nothing left out: the formatting check, the four fake
# tiers, the guards on this stack's own scripts, and the proof tier against a
# real database.
#
# The proof tier is why this file starts containers. It needs a real Postgres
# with the schema applied, so this run brings the stack up when it is down,
# applies the migrations, seeds an empty database, and takes the stack down again
# only if it was the one that started it. A stack somebody is already using is
# used and left alone.
#
# For the fast loop while writing code, scripts/test.sh is the four fake tiers on
# their own and starts nothing at all. This file is the one that answers whether
# the backend is green.
#
# Tests 6 and 16 are not here. Both live at the repository root and drive the api
# over http, so they belong to ../scripts/test_integration.sh, and everything in
# the repository at once is ../scripts/test_all.sh.
#
# Usage:
#   APP_ENV=development backend/scripts/test_all.sh                    (a terminal)
#   APP_ENV=development backend/scripts/test_all.sh --run-integration  (a workflow)
#
# Note:
# - FAULT_INJECTION_ENABLED is not needed here. Nothing in this file breaks a
#   confirm transaction on purpose, which is test 16's job
#
# Return:
# - 0: every step passed
# - 1: at least one step failed, each one named on the way out
# - 2: refused by a guard, or an unknown flag
# ---------------------------------------------------------------------------- #

set -uo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$backend_root/.." && pwd)"

source "$repository_root/scripts/lib/confirm.sh"
source "$backend_root/scripts/lib/database.sh"

# One flag, and only because the stack step has a question nobody can answer from
# a pipe: may this run start and stop containers.
run_the_integration="ask"

for argument in "$@"; do
    case "$argument" in
        --run-integration)
            run_the_integration="yes"
            ;;
        *)
            printf "refused: unknown flag '%s', only --run-integration is accepted\\n" "$argument" >&2

            exit 2
            ;;
    esac
done

confirm_require_environment

passed=0
failed=0
failed_steps=()

# Whether this run is the one that started the containers. Only then does it take
# them down again. Taking down a stack somebody else raised would cost them their
# seeded database, which is a surprise worth never causing.
started_the_stack="no"

# Whether the stack step was reached. A run that failed earlier has nothing to
# say about containers, and saying it anyway buries the real failure.
reached_the_stack="no"

tear_down() {
    if [ "$reached_the_stack" != "yes" ]; then
        return 0
    fi

    if [ "$started_the_stack" != "yes" ]; then
        printf '\nleaving the containers up, this run did not start them\n'

        return 0
    fi

    printf '\nstopping the containers this run started\n'
    "$repository_root/scripts/stack_down.sh" backend
}

trap tear_down EXIT

run_step() {
    local name="$1"

    shift

    printf '\n--> %s\n' "$name"

    if "$@"; then
        passed=$((passed + 1))

        return 0
    fi

    failed=$((failed + 1))
    failed_steps+=("$name")

    return 1
}

require_deliberate_run() {
    if [ -t 0 ]; then
        return 0
    fi

    if [ "$run_the_integration" = "yes" ]; then
        return 0
    fi

    printf 'refused: stdin is not a terminal, pass --run-integration to start and stop containers deliberately\n' >&2

    exit 2
}

bring_up() {
    if database_require_running >/dev/null 2>&1; then
        printf 'the backend stack is already up, using it\n'

        return 0
    fi

    "$repository_root/scripts/stack_up.sh" backend || return 1

    started_the_stack="yes"
}

# Applies the schema, and seeds only when there is nothing there.
#
# seed.sh refuses rather than overwriting, which is right for a script a person
# runs and wrong for one a test run calls, so the emptiness is checked here
# instead of letting a refusal fail the run.
prepare_the_database() {
    local parents

    "$backend_root/scripts/migrate.sh" || return 1

    parents="$(database_scalar 'select count(*) from parents')"

    if [ "$parents" != "0" ]; then
        printf 'the database already holds %s parent(s), leaving the seed alone\n' "$parents"

        return 0
    fi

    "$backend_root/scripts/seed.sh" --generate-demo-users
}

report() {
    local step_name

    printf '\n%d passed, %d failed\n' "$passed" "$failed"

    if [ "$failed" -eq 0 ]; then
        return 0
    fi

    printf 'failed:\n' >&2

    for step_name in "${failed_steps[@]}"; do
        printf '  - %s\n' "$step_name" >&2
    done

    return 1
}

main() {
    require_deliberate_run

    run_step "formatting" "$backend_root/scripts/format.sh" --check
    run_step "build, vet, and the four fake tiers" "$backend_root/scripts/test.sh"
    run_step "the guards on this stack's scripts" "$backend_root/scripts/debug_test.sh"
    run_step "the runtime this stack's database reaches for" "$backend_root/scripts/lib/database_test.sh"

    reached_the_stack="yes"

    run_step "the stack" bring_up &&
        run_step "the schema and the seed" prepare_the_database &&
        run_step "the proof tier" "$backend_root/scripts/test_proof.sh"

    report
}

main "$@"
