#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Empties every data table and inserts the seed rows again. Every booking,
# payment, audit line, queued job, and refresh token in the development database
# is gone when this finishes.
#
# The difference from backend/scripts/db_reset.sh is the schema. That one drops
# it and rebuilds from the migrations, which is what to reach for after a
# migration changes. This one leaves the schema alone and only replaces the
# data, which is what to reach for between two demonstration runs.
#
# It prompts, it is development only, and it names what it will destroy before
# asking. All of that comes from scripts/lib/confirm.sh, which is the one place
# that behaviour is defined.
#
# Usage:
#   APP_ENV=development scripts/seed_reset.sh
#   APP_ENV=development scripts/seed_reset.sh --dry-run
#
# Exit codes:
# - 0: done, or the dry run finished
# - 1: declined by the operator
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_root="$repository_root/backend"

source "$repository_root/scripts/lib/confirm.sh"
source "$backend_root/scripts/lib/database.sh"

# Every table this script empties, named literally. No wildcard, no "everything
# in public", because a script that discovers its own targets can discover the
# wrong one.
#
# schema_migrations is deliberately absent: the schema is not being rebuilt, so
# the record of what has already been applied has to survive.
SEED_RESET_TABLES=(
    parents
    students
    trial_classes
    bookings
    payment_attempts
    booking_events
    job_queue
    refresh_tokens
)

# Refuses when the database holds a table this script does not know about.
#
# The list above is written by hand, and a migration that adds a table without
# adding it here would leave stale rows behind after a reset that reported
# success. That is the failure this catches: silence, rather than an error.
require_table_list_complete() {
    local present
    local table

    present="$(database_scalar "
        select table_name
        from information_schema.tables
        where table_schema = 'public'
          and table_type = 'BASE TABLE'
          and table_name <> 'schema_migrations'
        order by table_name
    ")"

    while read -r table; do
        [ -n "$table" ] || continue

        case " ${SEED_RESET_TABLES[*]} " in
            *" $table "*) ;;
            *) confirm_refuse "the database holds table '$table', which this script does not list. Add it to SEED_RESET_TABLES or use backend/scripts/db_reset.sh" ;;
        esac
    done <<<"$present"
}

main() {
    confirm_parse_flags "$@"
    confirm_require_environment

    database_require_running
    require_table_list_complete

    confirm_target_name "$DATABASE_CONTAINER" "every data row in the database inside container"
    confirm_proceed "empty ${#SEED_RESET_TABLES[@]} table(s) in database '$DATABASE_NAME', then insert the seed rows again"

    # One statement, so the tables come back empty together or not at all.
    # cascade covers the foreign keys between them, and restart identity resets
    # any sequence they own.
    printf 'emptying %s table(s)\n' "${#SEED_RESET_TABLES[@]}"
    database_statement "truncate table $(
        IFS=,
        printf '%s' "${SEED_RESET_TABLES[*]}"
    ) restart identity cascade;"

    printf 'reseeding\n'

    # The operator has already answered for this run, and the accounts about to
    # be created are the ones they just agreed to replace.
    "$backend_root/scripts/seed.sh" --generate-demo-users

    printf 'data is back to a freshly seeded state, the schema was not touched\n'
}

main "$@"
