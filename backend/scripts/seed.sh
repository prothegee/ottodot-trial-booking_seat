#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Inserts the seed rows. It refuses when data is already there rather than
# overwriting, which is what keeps it in the safe class: a script that can only
# ever add to an empty database has nothing to destroy.
#
# Replacing existing data is backend/scripts/db_reset.sh, and that one prompts.
#
# Usage:
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$backend_root/scripts/lib/database.sh"

seed_file="$backend_root/migrations/0002_seed.sql"

main() {
    local existing_parents

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
