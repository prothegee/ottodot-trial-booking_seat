#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Stops a stack and removes its containers. It never touches any .data/
# directory, so the database survives. Removing data is what
# scripts/cleanup_dev.sh is for, and that one prompts.
#
# Usage:
#   scripts/stack_down.sh            (both stacks)
#   scripts/stack_down.sh backend
#   scripts/stack_down.sh frontend
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$repository_root/scripts/lib/stack.sh"

main() {
    local selection
    local stack

    selection="$(stack_selection "${1:-all}")" || exit 2

    while read -r stack; do
        printf 'stopping %s, its local state is left alone\n' "$stack"
        stack_compose "$stack" down
    done <<<"$selection"

    printf 'containers are down\n'
}

main "$@"
