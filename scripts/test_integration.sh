#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: automation
#
# Everything that needs a real stack, in one command: containers up, the proof
# tier, test 6, test 16, containers down.
#
# Test 6 is given --fresh-class, so it races a throwaway class it makes and
# drops rather than the seeded seat. That is what lets this file run twice in a
# row: the seeded seat can only ever be raced once.
#
# It is the automation class, which is its own set of rules. It never prompts,
# because a workflow has nobody to answer, and it refuses to run without --yes
# when there is no terminal and no stack up yet, because a script that starts
# and stops containers should not be reachable by accident from a pipe. Finding
# a stack already up starts nothing, so that run needs no flag.
#
# The same file runs locally and in continuous integration. There is no second
# definition of what the proof tier is, so nothing can drift between the two.
#
# A stack that was already up is used and left up. Taking down containers this
# script did not start would be a surprise, and a surprise that costs somebody
# their seeded database is the kind worth avoiding.
#
# Usage:
#   APP_ENV=development scripts/test_integration.sh            (a terminal)
#   APP_ENV=development scripts/test_integration.sh --yes      (a workflow)
#   APP_ENV=development scripts/test_integration.sh --dry-run  (the plan only)
#
# Note:
# - FAULT_INJECTION_ENABLED is exported before the stack starts, because
#   test 16 has nothing to arm without it. It is refused outside
#   development by the api's own configuration, so this cannot travel
#
# Exit codes:
# - 0: every step passed
# - 1: a step failed, named on the way out
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_root="$repository_root/backend"

source "$repository_root/scripts/lib/confirm.sh"
source "$backend_root/scripts/lib/database.sh"

# Whether this run is the one that started the containers. Only then does it
# take them down again.
started_the_stack="no"

# Whether the guards were passed. A run refused before any of the work started
# has nothing to say about containers, and saying it anyway buries the refusal.
reached_the_work="no"

# The step that failed, so the last line of output says what to look at.
failed_step=""

tear_down() {
    if [ "$reached_the_work" != "yes" ]; then
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

refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit 2
}

# The automation guard. A terminal may run this without ceremony, and so may a
# pipe when the stack is already up: that run starts nothing and stops nothing,
# so the question the flag answers does not arise.
require_deliberate_run() {
    if [ -t 0 ]; then
        return 0
    fi

    if confirm_is_assumed_yes; then
        return 0
    fi

    if stack_is_up; then
        return 0
    fi

    refuse "stdin is not a terminal, pass --yes to start and stop containers deliberately"
}

stack_is_up() {
    database_require_running >/dev/null 2>&1
}

bring_up() {
    if stack_is_up; then
        printf 'the backend stack is already up, using it\n'

        return 0
    fi

    printf 'starting the backend stack\n'
    "$repository_root/scripts/stack_up.sh" backend || return 1

    started_the_stack="yes"
}

# Applies the schema, and seeds only when there is nothing there.
#
# seed.sh refuses rather than overwriting, which is correct for a script a
# person runs and wrong for one a workflow runs, so the emptiness is checked
# here instead of letting a refusal fail the run.
prepare_the_database() {
    local parents

    "$backend_root/scripts/migrate.sh" || return 1

    parents="$(database_scalar 'select count(*) from parents')"

    if [ "$parents" != "0" ]; then
        printf 'the database already holds %s parent(s), leaving the seed alone\n' "$parents"

        return 0
    fi

    # The flag rather than the prompt, for the same reason the emptiness is
    # checked above: nothing here has a terminal to ask.
    "$backend_root/scripts/seed.sh" --generate-demo-users
}

# Runs one step and remembers the first one that failed.
step() {
    local name="$1"

    shift

    printf '\n--> %s\n' "$name"

    if "$@"; then
        return 0
    fi

    failed_step="$name"

    return 1
}

print_the_plan() {
    printf 'this would, in order:\n'
    printf '  1. start the backend stack, unless it is already up\n'
    printf '  2. apply the migrations, and seed if the database is empty\n'
    printf '  3. run the proof tier: go test -tags=containers\n'
    printf '  4. run test 6: scripts/race_last_seat.sh --fresh-class\n'
    printf '  5. run test 16: scripts/smoke_failure.sh\n'
    printf '  6. stop the containers, if step 1 started them\n'
    printf '\ndry run, nothing was started.\n'
}

main() {
    confirm_parse_flags "$@"
    confirm_require_environment

    # The dry run comes before the terminal guard on purpose. It starts nothing,
    # so there is nothing for a pipe to be deliberate about, and a workflow
    # being able to print the plan is worth more than the symmetry.
    if confirm_is_dry_run; then
        print_the_plan

        exit 0
    fi

    require_deliberate_run

    reached_the_work="yes"

    # Exported before the stack starts, so the api comes up with the surface
    # test 16 needs. Compose reads it from this environment.
    export FAULT_INJECTION_ENABLED=true

    step "starting the stack" bring_up &&
        step "preparing the database" prepare_the_database &&
        step "the proof tier" "$backend_root/scripts/test_proof.sh" &&
        step "test 6" "$repository_root/scripts/race_last_seat.sh" --fresh-class &&
        step "test 16" "$repository_root/scripts/smoke_failure.sh" --yes

    if [ -n "$failed_step" ]; then
        printf '\nfailed at: %s\n' "$failed_step" >&2

        return 1
    fi

    printf '\nevery step passed\n'
}

main "$@"
