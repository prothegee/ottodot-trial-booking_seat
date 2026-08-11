//go:build containers

// The proof that the gauges the dashboard draws come from somewhere real.
//
// Pool statistics can be checked against anything. Replication lag cannot: it is
// a number only a replica that is actually replaying a stream can produce, and a
// fake would only prove that a function returns what it was told to.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/database/...
package database_test

import (
    "context"
    "testing"
    "time"
)

func TestPoolStatistics(t *testing.T) {
    t.Run("integration: an acquired connection shows up in the pool statistics", func(t *testing.T) {
        // The panel this feeds answers one question: is the pool running out of
        // room. That is only answerable if an in flight query is visible while
        // it is in flight.
        pools := openBothPools(t)
        defer pools.Close()

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        connection, err := pools.Primary().Acquire(ctx)
        if err != nil {
            t.Fatalf("a connection could not be acquired: %v", err)
        }

        held := pools.PrimaryStatistics()

        connection.Release()

        if held.Acquired < 1 {
            t.Fatalf("the pool reports %d acquired while one was held", held.Acquired)
        }

        if held.Total < held.Acquired {
            t.Fatalf("the pool reports %d total and %d acquired, which cannot both be true",
                held.Total, held.Acquired)
        }
    })

    t.Run("unit: both pools report separately", func(t *testing.T) {
        // One number for both pools would hide the case worth seeing: the
        // deciding pool saturating while the advisory one sits idle.
        pools := openBothPools(t)
        defer pools.Close()

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        if err := pools.Primary().Ping(ctx); err != nil {
            t.Fatalf("the primary could not be pinged: %v", err)
        }

        if pools.PrimaryStatistics().Total < 1 {
            t.Fatal("the primary reports no connections at all after one was used")
        }
    })
}

func TestReplicationLag(t *testing.T) {
    t.Run("integration: a healthy replica reports a lag that is readable and not negative", func(t *testing.T) {
        // The alert built on this fires above a threshold, so the number has to
        // be a real byte count rather than a placeholder. On an idle stack it is
        // zero, and zero is the correct answer rather than a missing one.
        pools := openBothPools(t)
        defer pools.Close()

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        lag, err := pools.ReplicationLagBytes(ctx)
        if err != nil {
            t.Fatalf("the replication lag could not be read: %v", err)
        }

        if lag < 0 {
            t.Fatalf("the lag reads %d bytes, and a negative gauge is worse than an absent one", lag)
        }
    })

    t.Run("behaviour: the lag is read on the replica, so it survives the primary being busy", func(t *testing.T) {
        // Reading it on the primary would mean pg_stat_replication, which has no
        // row at all while a replica is disconnected. A replica that has fallen
        // over would then report perfect health, which is the opposite of what
        // is happening.
        pools := openBothPools(t)
        defer pools.Close()

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        var inRecovery bool

        if err := pools.Replica().QueryRow(ctx, `select pg_is_in_recovery()`).Scan(&inRecovery); err != nil {
            t.Fatalf("the replica could not be asked about its own state: %v", err)
        }

        if !inRecovery {
            t.Fatal("the second pool is not a replica, so the lag it reports means nothing")
        }

        first, err := pools.ReplicationLagBytes(ctx)
        if err != nil {
            t.Fatalf("the first read failed: %v", err)
        }

        // A write on the primary, then the same read again. It has to answer
        // both times whatever the primary is doing.
        if _, err := pools.Primary().Exec(ctx, `select pg_sleep(0.1)`); err != nil {
            t.Fatalf("the primary could not be kept busy: %v", err)
        }

        time.Sleep(200 * time.Millisecond)

        second, err := pools.ReplicationLagBytes(ctx)
        if err != nil {
            t.Fatalf("the second read failed: %v", err)
        }

        if first < 0 || second < 0 {
            t.Fatalf("the lag read %d and then %d", first, second)
        }
    })
}
