#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Drops the public schema, rebuilds it from the migrations, and reseeds. Every
# booking, payment, and audit row in the development database is gone when this
# finishes.
#
# It prompts, it is development only, and it names what it will destroy before
# asking. All of that comes from scripts/lib/confirm.sh, which is the one place
# that behaviour is defined.
#
# Usage:
#   APP_ENV=development backend/scripts/db_reset.sh
#   APP_ENV=development backend/scripts/db_reset.sh --dry-run
#
# Exit codes:
# - 0: done, or the dry run finished
# - 1: declined by the operator
# - 2: refused by a guard
# ---------------------------------------------------------------------------- #

set -euo pipefail

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$backend_root/.." && pwd)"

source "$repository_root/scripts/lib/confirm.sh"
source "$backend_root/scripts/lib/database.sh"

main() {
    confirm_parse_flags "$@"
    confirm_require_environment

    database_require_running

    confirm_target_name "$DATABASE_CONTAINER" "the whole public schema in the database inside container"
    confirm_proceed "drop the public schema in database '$DATABASE_NAME', then migrate and seed it again"

    printf 'dropping the public schema\n'
    database_statement 'drop schema public cascade; create schema public;'

    printf 'rebuilding from the migrations\n'
    "$backend_root/scripts/migrate.sh"

    printf 'reseeding\n'
    "$backend_root/scripts/seed.sh"

    printf 'database is back to a freshly seeded state\n'
}

main "$@"
