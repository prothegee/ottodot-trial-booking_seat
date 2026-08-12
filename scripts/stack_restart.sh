#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Stops a stack and starts it again, in one command. It creates, stops, and
# starts, so it never prompts and no data directory is touched.
#
# It is the command for a change a running container cannot pick up: an edited
# compose file, a rebuilt image, an environment variable compose only reads when
# a container starts. A stack that is already down is simply started.
#
# Usage:
#   scripts/stack_restart.sh              (both stacks)
#   scripts/stack_restart.sh backend
#   scripts/stack_restart.sh frontend
#
# Note:
# - the two scripts underneath do the work, so a restart cannot drift from what
#   a stop and a start each do on their own
# - the environment is passed through unchanged, so FAULT_INJECTION_ENABLED and
#   the build identity behave exactly as they do with stack_up.sh
#
# Exit codes:
# - 0: the selected stacks are up
# - 1: a stack did not come back, named by stack_up.sh
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"

main() {
    local selection

    selection="$(stack_selection "${1:-all}")" || exit 2

    printf 'restarting: %s\n' "$(printf '%s' "$selection" | tr '\n' ' ')"

    "$repository_root/scripts/stack_down.sh" "${1:-all}"
    "$repository_root/scripts/stack_up.sh" "${1:-all}"
}

main "$@"
