#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Runs once, inside the primary container, from the image init hook. It has
# three jobs:
# 1. create the replication role the standby logs in with
# 2. create the physical replication slot, so the primary keeps the write ahead
#    log a disconnected standby still needs
# 3. install this project's postgresql.conf and pg_hba.conf into the data
#    directory, because initdb writes its own defaults there first
#
# The init hook runs against a temporary server. The real server starts after
# this script returns, which is what picks up the two config files.
# ---------------------------------------------------------------------------- #

set -euo pipefail

: "${POSTGRES_REPLICATION_USER:?POSTGRES_REPLICATION_USER is required}"
: "${POSTGRES_REPLICATION_PASSWORD:?POSTGRES_REPLICATION_PASSWORD is required}"

replication_slot="${POSTGRES_REPLICATION_SLOT:-ottodot_replica}"
config_source="${OTTODOT_CONFIG_DIR:-/etc/ottodot/postgresql}"

# The role name and the password are passed as psql variables rather than
# pasted into the statement, so a character in either one cannot end the quote.
psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    --set replication_user="$POSTGRES_REPLICATION_USER" \
    --set replication_password="$POSTGRES_REPLICATION_PASSWORD" \
    --set replication_slot="$replication_slot" <<'SQL'
create role :"replication_user" with replication login password :'replication_password';

select pg_create_physical_replication_slot(:'replication_slot')
where not exists (
    select 1 from pg_replication_slots where slot_name = :'replication_slot'
);
SQL

install --mode=0600 "$config_source/postgresql.conf" "$PGDATA/postgresql.conf"
install --mode=0600 "$config_source/pg_hba.conf" "$PGDATA/pg_hba.conf"

echo "primary: replication role, slot '$replication_slot', and project config are in place"
