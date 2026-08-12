#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: prompts
#
# Inserts the seed rows. It refuses when data is already there rather than
# overwriting, so it can destroy nothing.
#
# It still asks first, and the reason is the accounts rather than the data. The
# four seeded parents share one password that is written down in how-to.md, so
# any database holding these rows is one that anybody who has read the repository
# can sign in to. That is fine on a laptop and nowhere else, and it is worth one
# question.
#
# Replacing existing data is backend/scripts/db_reset.sh, and that one prompts
# about the destruction instead.
#
# Usage:
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
#   backend/scripts/seed.sh --generate-demo-users
#
# Exit codes:
# - 0: seeded
# - 1: declined at the prompt, or nothing to seed into
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$backend_root/scripts/lib/database.sh"

seed_file="$backend_root/migrations/0002_seed.sql"

# The demo accounts this seed creates, and the password all four share. They are
# named here rather than read out of the sql, so the question a person answers
# says exactly what they are agreeing to.
SEED_DEMO_ACCOUNTS=(
    "alice.tan@example.test (parent)"
    "budi.santoso@example.test (parent)"
    "chandra.wijaya@example.test (parent)"
    "ops.admin@example.test (admin)"
)

SEED_DEMO_PASSWORD="otto123"

generate_demo_users="no"

parse_arguments() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --generate-demo-users)
                generate_demo_users="yes"
                ;;
            *)
                printf "refused: unknown flag '%s', only --generate-demo-users is accepted\\n" "$1" >&2

                exit 2
                ;;
        esac

        shift
    done
}

# ask_about_the_demo_users names every account, then waits.
#
# It defaults to No, so an accidental return key seeds nothing. Without a
# terminal there is nobody to ask, and the flag is the only way through.
ask_about_the_demo_users() {
    local account
    local reply=""

    printf '\n'
    printf 'about to: create %s accounts that all share one password\n' "${#SEED_DEMO_ACCOUNTS[@]}"
    printf 'this creates:\n'

    for account in "${SEED_DEMO_ACCOUNTS[@]}"; do
        printf '  - %s\n' "$account"
    done

    printf '\n'
    printf 'the password is %s for all of them, and it is written down in how-to.md.\n' "$SEED_DEMO_PASSWORD"
    printf 'anybody who has read this repository can sign in to whatever holds these rows.\n'
    printf '\n'

    if [ "$generate_demo_users" = "yes" ]; then
        printf 'confirmed by --generate-demo-users.\n'

        return 0
    fi

    if [ ! -t 0 ]; then
        printf 'refused: stdin is not a terminal, pass --generate-demo-users to confirm this deliberately\n' >&2

        exit 2
    fi

    printf 'Continue? [y/N] '
    read -r reply || reply=""

    if [ "$reply" != "y" ]; then
        printf 'declined, nothing was seeded.\n'

        exit 1
    fi
}

main() {
    local existing_parents

    parse_arguments "$@"
    database_require_running

    if [ ! -f "$seed_file" ]; then
        printf 'seed file is missing: %s\n' "$seed_file" >&2

        return 1
    fi

    existing_parents="$(database_scalar 'select count(*) from parents')"

    if [ "$existing_parents" != "0" ]; then
        printf 'refusing to seed: parents already holds %s row(s)\n' "$existing_parents" >&2
        printf 'use backend/scripts/db_reset.sh to start from an empty schema\n' >&2

        return 1
    fi

    ask_about_the_demo_users

    printf 'seeding from %s\n' "$(basename "$seed_file")"

    {
        printf 'begin;\n'
        cat "$seed_file"
        printf '\ncommit;\n'
    } | database_psql

    printf 'seeded %s parent(s), %s class(es), %s booking(s)\n' \
        "$(database_scalar 'select count(*) from parents')" \
        "$(database_scalar 'select count(*) from trial_classes')" \
        "$(database_scalar 'select count(*) from bookings')"
}

main "$@"
