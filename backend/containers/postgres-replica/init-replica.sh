#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# The replica container entrypoint. A standby cannot be created by initdb, it
# has to be cloned from the primary, so this replaces the image entrypoint
# rather than adding to it.
#
# First start: wait for the primary, take a base backup, then stream.
# Every later start: the data directory already exists, so it only streams.
#
# Note:
# - the rm below is scoped to $PGDATA and runs only when PG_VERSION is missing,
#   which is the one case where the directory holds nothing worth keeping
# ---------------------------------------------------------------------------- #

set -euo pipefail

: "${PGDATA:?PGDATA is required}"
: "${POSTGRES_REPLICATION_USER:?POSTGRES_REPLICATION_USER is required}"
: "${POSTGRES_REPLICATION_PASSWORD:?POSTGRES_REPLICATION_PASSWORD is required}"

primary_host="${POSTGRES_PRIMARY_HOST:-postgres-primary}"
primary_port="${POSTGRES_PRIMARY_PORT:-5432}"
replication_slot="${POSTGRES_REPLICATION_SLOT:-ottodot_replica}"

mkdir -p "$PGDATA"
chmod 0700 "$PGDATA"

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    echo "replica: waiting for the primary at ${primary_host}:${primary_port}"

    until pg_isready --host "$primary_host" --port "$primary_port" --quiet; do
        sleep 1
    done

    echo "replica: cloning the primary with pg_basebackup"
    rm -rf "${PGDATA:?}"/* "${PGDATA:?}"/.[!.]* 2>/dev/null || true

    PGPASSWORD="$POSTGRES_REPLICATION_PASSWORD" pg_basebackup \
        --host "$primary_host" \
        --port "$primary_port" \
        --username "$POSTGRES_REPLICATION_USER" \
        --pgdata "$PGDATA" \
        --wal-method=stream \
        --slot "$replication_slot" \
        --write-recovery-conf \
        --progress \
        --verbose

    chmod 0700 "$PGDATA"

    echo "replica: clone finished, starting in standby mode"
fi

exec postgres -c hot_standby=on -c listen_addresses='*'
