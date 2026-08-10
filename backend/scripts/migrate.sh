#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Applies pending schema migrations forward, in file name order. It only ever
# moves forward, so there is no down step to get wrong at two in the morning.
#
# Seed files are skipped here on purpose. Data is backend/scripts/seed.sh, and
# keeping them apart is what lets a reviewer reset data without touching the
# schema.
#
# Each migration and the row that records it commit together, so a half applied
# migration cannot be recorded as done.
#
# Usage:
#   scripts/stack_up.sh
#   backend/scripts/migrate.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

source "$backend_root/scripts/lib/database.sh"

migrations_directory="$backend_root/migrations"

ensure_tracking_table() {
    database_statement "
        create table if not exists schema_migrations (
            version    text        primary key,
            applied_at timestamptz not null default now()
        );
    " >/dev/null
}

is_applied() {
    local version="$1"
    local found

    found="$(database_scalar "select 1 from schema_migrations where version = '${version}'")"

    [ -n "$found" ]
}

apply_migration() {
    local file="$1"
    local version="$2"

    {
        printf 'begin;\n'
        cat "$file"
        printf "\ninsert into schema_migrations (version) values ('%s');\n" "$version"
        printf 'commit;\n'
    } | database_psql
}

main() {
    local file
    local version
    local applied_count=0

    database_require_running
    ensure_tracking_table

    shopt -s nullglob

    for file in "$migrations_directory"/[0-9]*.sql; do
        version="$(basename "$file")"

        case "$version" in
            *seed*.sql)
                continue
                ;;
        esac

        if is_applied "$version"; then
            printf 'already applied: %s\n' "$version"

            continue
        fi

        printf 'applying: %s\n' "$version"
        apply_migration "$file" "$version"
        applied_count=$((applied_count + 1))
    done

    if [ "$applied_count" -eq 0 ]; then
        printf 'schema is up to date, nothing to apply\n'

        return 0
    fi

    printf 'applied %s migration(s)\n' "$applied_count"
}

main "$@"
