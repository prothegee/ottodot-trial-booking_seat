#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: library
#
# The one confirmation path shared by every destructive script in this
# repository. This file is sourced, never executed.
#
# Order of the guards, and the reason for each:
# 1. APP_ENV must be development. An unset value counts as refusal, so the
#    dangerous default is the safe one.
# 2. Every target must be named. A path must sit inside this repository, a
#    container or volume must carry the project prefix.
# 3. --dry-run prints the manifest and stops, so a reviewer can look first.
# 4. A pipe cannot confirm. Without a terminal the script needs --yes.
# 5. The manifest prints before the question, because the parent of every
#    mistake is a prompt that does not say what it will destroy.
# 6. The default answer is No.
#
# Usage:
#   set -euo pipefail
#   source "<repository root>/scripts/lib/confirm.sh"
#
#   confirm_parse_flags "$@"
#   confirm_require_environment
#
#   confirm_target_path "$repository_root/backend/containers/redis/.data"
#   confirm_target_name "ottodot-redis" "container"
#   confirm_proceed "remove local container state"
#
#   rm -rf "$repository_root/backend/containers/redis/.data"
#
# confirm_require_environment is optional and confirm_proceed runs it anyway.
# Call it early in a script that reaches for a database or a container runtime
# before it asks, so a production shell is refused before any of that happens.
#
# Exit codes this library produces:
# - 0: the dry run finished, or the caller may proceed
# - 1: the operator declined
# - 2: a guard refused
# ---------------------------------------------------------------------------- #

# The prefix every container and volume name must carry, so a script can never
# remove something it does not own.
CONFIRM_PROJECT_PREFIX="${CONFIRM_PROJECT_PREFIX:-ottodot}"

CONFIRM_EXIT_DECLINED=1
CONFIRM_EXIT_REFUSED=2

confirm_dry_run="no"
confirm_assume_yes="no"
confirm_targets=()

# Repository root, derived from this file rather than from the caller, so a
# script in any of the three script directories resolves the same path.
confirm_repository_root() {
    cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
}

confirm_refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit "$CONFIRM_EXIT_REFUSED"
}

# Reads the two flags every destructive script accepts and rejects anything
# else, so a typo never silently becomes an unguarded run.
confirm_parse_flags() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --dry-run)
                confirm_dry_run="yes"
                ;;
            --yes)
                confirm_assume_yes="yes"
                ;;
            *)
                confirm_refuse "unknown flag '$1', only --dry-run and --yes are accepted"
                ;;
        esac

        shift
    done
}

confirm_is_dry_run() {
    [ "$confirm_dry_run" = "yes" ]
}

# Whether --yes was passed.
#
# The destructive scripts never need to ask, because confirm_proceed reads it
# for them. The automation class does: it never prompts, so refusing without
# this flag is the whole of its guard.
confirm_is_assumed_yes() {
    [ "$confirm_assume_yes" = "yes" ]
}

# The environment guard on its own, so a script can refuse before it reaches for
# anything.
#
# confirm_proceed runs this too, and that is the one every destructive script
# ends at. This exists for the scripts that inspect a running system on the way
# there: connecting to a database or listing containers in a production shell is
# already the wrong thing to be doing, even when the destroying part never
# happens.
confirm_require_environment() {
    if [ "${APP_ENV:-}" != "development" ]; then
        confirm_refuse "APP_ENV is '${APP_ENV:-unset}', this script runs only with APP_ENV=development"
    fi
}

# Declares a filesystem target. It must be absolute, free of '..', inside this
# repository, and not the repository root itself.
confirm_target_path() {
    local path="$1"
    local root

    root="$(confirm_repository_root)"

    case "$path" in
        /*) ;;
        *) confirm_refuse "target path '$path' is not absolute" ;;
    esac

    case "$path" in
        *..*) confirm_refuse "target path '$path' contains '..'" ;;
    esac

    if [ "$path" = "$root" ]; then
        confirm_refuse "target path is the repository root itself"
    fi

    case "$path" in
        "$root"/*) ;;
        *) confirm_refuse "target path '$path' is outside the repository at '$root'" ;;
    esac

    confirm_targets+=("path: $path")
}

# Declares a container, image, network, or volume target. It must carry the
# project prefix, which is what stops a wildcard from ever being needed.
#
# Param:
# name - string (what will be removed)
# kind - string (optional, what sort of thing it is. It only reaches the
#        manifest, and it is worth passing when one name is used for more than
#        one sort, which is what a container and its network usually are)
confirm_target_name() {
    local name="$1"
    local kind="${2:-container, image, or volume}"

    case "$name" in
        "$CONFIRM_PROJECT_PREFIX"*) ;;
        *) confirm_refuse "target '$name' does not carry the project prefix '$CONFIRM_PROJECT_PREFIX'" ;;
    esac

    confirm_targets+=("$kind: $name")
}

confirm_print_manifest() {
    local action="$1"
    local target

    printf '\n'
    printf 'about to: %s\n' "$action"
    printf 'this destroys:\n'

    for target in "${confirm_targets[@]}"; do
        printf '  - %s\n' "$target"
    done

    printf '\n'
}

# Runs every guard in order. Returns 0 only when the caller may do the work.
confirm_proceed() {
    local action="$1"
    local reply=""

    confirm_require_environment

    if [ "${#confirm_targets[@]}" -eq 0 ]; then
        confirm_refuse "no target was declared, refusing to destroy something this script cannot name"
    fi

    confirm_print_manifest "$action"

    if [ "$confirm_dry_run" = "yes" ]; then
        printf 'dry run, nothing was touched.\n'

        exit 0
    fi

    if [ ! -t 0 ]; then
        if [ "$confirm_assume_yes" != "yes" ]; then
            confirm_refuse "stdin is not a terminal, pass --yes to confirm this deliberately"
        fi

        printf 'confirmed by --yes, no terminal attached.\n'

        return 0
    fi

    if [ "$confirm_assume_yes" = "yes" ]; then
        printf 'confirmed by --yes.\n'

        return 0
    fi

    printf 'Continue? [y/N] '
    read -r reply || reply=""

    if [ "$reply" != "y" ]; then
        printf 'declined, nothing was touched.\n'

        exit "$CONFIRM_EXIT_DECLINED"
    fi

    return 0
}
