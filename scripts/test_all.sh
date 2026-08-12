#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: automation
#
# Every test in the repository, in one command: the backend, then the frontend,
# then the guards on the scripts that belong to neither stack, then everything
# that needs a real stack.
#
# Each stack's own runner is called rather than copied, so there is one
# definition of what a green backend means and one of what a green frontend
# means. This file adds the eleven suites at the root, and the last step, which
# is scripts/test_integration.sh: containers up, the schema, the seed, the proof
# tier, test 16, and the containers down again if this run started them.
#
# This file and the backend runner both start containers, so both want
# APP_ENV=development, and both take --run-integration when there is no terminal
# and no stack up yet. A run that finds a stack starts nothing, so it needs no
# flag at all. The frontend runner needs neither.
#
# The proof tier runs twice: once inside the backend runner, once inside the
# integration step. Each of those commands has to be complete on its own, which
# is what the user asked for when a runner named test_all skipped something, and
# the price of that is about twenty seconds and one extra stack start here. Run
# either command on its own and there is no repetition at all.
#
# Usage:
#   APP_ENV=development scripts/test_all.sh                     (a terminal)
#   APP_ENV=development scripts/test_all.sh --run-integration   (a workflow)
#
# Return:
# - 0: every step passed
# - 1: at least one step failed, each one named on the way out
# - 2: refused by a guard, or an unknown flag
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/confirm.sh"
source "$repository_root/backend/scripts/lib/database.sh"

# One flag, and only because the last step has a question nobody can answer from
# a pipe: may this run start and stop containers. Every other flag is a typo, and
# a typo that silently ran the whole suite anyway would read as though it had
# done something.
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

# Before any step, because the last one starts containers and a production shell
# should be turned away at the door rather than four minutes in.
confirm_require_environment

passed=0
failed=0
failed_steps=()

# Whether this run started the containers, and so whether it may stop them.
started_the_stack="no"

# One stack for the whole run.
#
# Both steps that need one reuse whatever is up, and neither takes it down,
# because neither started it. Without this they raise and drop a stack each,
# which costs about a minute and prints "starting the stack" twice in a run that
# only ever needed it once.
bring_up_once() {
    if database_require_running >/dev/null 2>&1; then
        printf 'the backend stack is already up, using it\n'

        return 0
    fi

    "$repository_root/scripts/stack_up.sh" backend || return 1

    started_the_stack="yes"
}

tear_down() {
    if [ "$started_the_stack" != "yes" ]; then
        return 0
    fi

    printf '\nstopping the containers this run started\n'
    "$repository_root/scripts/stack_down.sh" backend
}

trap tear_down EXIT

run_step() {
    local name="$1"

    shift

    printf '\n======> %s\n' "$name"

    if "$@"; then
        passed=$((passed + 1))

        return 0
    fi

    failed=$((failed + 1))
    failed_steps+=("$name")

    return 1
}

# The suites for the scripts up here, which belong to neither stack. They read
# their subject or stop inside a guard, so none of them starts or removes
# anything, and together they take a few seconds.
run_the_root_suites() {
    local suite

    for suite in \
        cleanup_dev_test.sh \
        race_last_seat_test.sh \
        smoke_failure_test.sh \
        smoke_refund_test.sh \
        stack_up_test.sh \
        stack_restart_test.sh \
        stack_status_test.sh \
        test_integration_test.sh \
        lib/confirm_test.sh \
        lib/settings_test.sh \
        lib/stack_test.sh; do
        run_step "scripts/$suite" "$repository_root/scripts/$suite"
    done
}

# The one step that needs a real stack. It decides for itself whether to start
# containers and whether to stop them, and this file only passes on the answer
# to a question a pipe cannot be asked, on the runs where it arises at all.
#
# --yes is what test_integration.sh calls that answer, which is the shared pair
# in scripts/lib/confirm.sh. The name on this file's own surface says the action
# instead, because a workflow line has to read as what it did.
run_the_integration_step() {
    local flags=()

    if [ "$run_the_integration" = "yes" ]; then
        flags=(--yes)
    fi

    run_step "the stack, the proof tier, test 6, and test 16" \
        "$repository_root/scripts/test_integration.sh" "${flags[@]+"${flags[@]}"}"
}

report() {
    local step_name

    printf '\n%d passed, %d failed\n' "$passed" "$failed"

    if [ "$failed" -eq 0 ]; then
        printf '\nthe whole repository is green.\n'

        return 0
    fi

    printf 'failed:\n' >&2

    for step_name in "${failed_steps[@]}"; do
        printf '  - %s\n' "$step_name" >&2
    done

    return 1
}

main() {
    local flags=()

    # The backend runner starts containers of its own now, so it asks the same
    # question this file does and needs the same answer passed on. The frontend
    # runner starts nothing and takes no flags at all.
    if [ "$run_the_integration" = "yes" ]; then
        flags=(--run-integration)
    fi

    # Exported before the stack starts, because compose reads it only when a
    # container is created. A stack raised without it has no fault surface, and
    # test 16 further down would refuse against the very stack this run made.
    export FAULT_INJECTION_ENABLED=true

    run_step "the stack for this run" bring_up_once

    run_step "the backend" "$repository_root/backend/scripts/test_all.sh" "${flags[@]+"${flags[@]}"}"
    run_step "the frontend" "$repository_root/frontend/scripts/test_all.sh"

    run_the_root_suites
    run_the_integration_step

    report
}

main "$@"
