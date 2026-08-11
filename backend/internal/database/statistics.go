package database

import (
    "context"

    "github.com/jackc/pgx/v5/pgxpool"
)

// PoolStatistics is what one pool is holding right now.
//
// It is this package's own shape rather than the driver's, so nothing outside
// here has to import pgx to read a number about a connection. Three values
// rather than the driver's dozen, because these are the three that answer the
// question worth asking: is the pool running out of room.
type PoolStatistics struct {
    Acquired int32
    Idle     int32
    Total    int32
}

// PrimaryStatistics is what the deciding pool is holding.
func (pools *Pools) PrimaryStatistics() PoolStatistics {
    return statisticsOf(pools.Primary())
}

// ReplicaStatistics is what the advisory pool is holding.
func (pools *Pools) ReplicaStatistics() PoolStatistics {
    return statisticsOf(pools.Replica())
}

// ReplicationLagBytes is how far the replica is behind the primary.
//
// It is read on the replica rather than on the primary, and that is the part
// worth being exact about. The primary's `pg_stat_replication` only has a row
// while a replica is actually connected, so a replica that has fallen over
// reports no lag at all from that side, which reads as perfectly caught up. The
// replica knows how far it has replayed whether or not it is still receiving.
//
// Note:
//   - a replica that is fully caught up reports zero rather than null. The
//     coalesce is what makes an idle stack read as zero lag instead of as a
//     failure.
//
// Param:
// ctx - context.Context (the sampler's deadline)
//
// Return:
//   - the lag in bytes of write ahead log
//   - the driver's failure, which the caller treats as "do not publish a number"
//     rather than as something to act on
func (pools *Pools) ReplicationLagBytes(ctx context.Context) (int64, error) {
    var lag int64

    err := pools.Replica().QueryRow(ctx, `
        select coalesce(
            pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()),
            0)::bigint`).Scan(&lag)
    if err != nil {
        return 0, err
    }

    if lag < 0 {
        // Replay ahead of receive is not a real state, and a negative gauge on a
        // dashboard is worse than an absent one.
        return 0, nil
    }

    return lag, nil
}

// statisticsOf reads one pool.
func statisticsOf(pool *pgxpool.Pool) PoolStatistics {
    if pool == nil {
        return PoolStatistics{}
    }

    stat := pool.Stat()

    return PoolStatistics{
        Acquired: stat.AcquiredConns(),
        Idle:     stat.IdleConns(),
        Total:    stat.TotalConns(),
    }
}
