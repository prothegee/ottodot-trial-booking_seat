package main

import (
    "context"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/observability"
)

// sampleInterval is how often the gauges are refreshed.
//
// Prometheus scrapes every five seconds in this project, so refreshing on the
// same period keeps the published number no more than one scrape stale without
// querying the replica on every scrape. The alternative, sampling inside the
// scrape handler, would put a database query on a route that has to answer even
// when the database is the thing that is broken.
const sampleInterval = 5 * time.Second

// sampleTimeout caps the one query each round makes.
const sampleTimeout = 3 * time.Second

// refundWorklistLimit caps the refund count.
//
// A gauge that only feeds an alert does not need an exact number past the point
// where somebody would already be looking. If two hundred parents are owed
// money, the alert has been firing for a long time and the difference between
// two hundred and two hundred and one changes nothing.
const refundWorklistLimit = 200

// sampleGauges keeps the numbers that describe a state rather than a count.
//
// Everything else this service publishes is a counter incremented where
// something happened. These are different: a pool size, a replication lag, and a
// refund backlog are true at an instant and nothing raises an event when they
// change, so somebody has to go and look.
//
// It runs until the context ends and reports nothing, because there is nothing a
// caller could do about a gauge that could not be read. A value that cannot be
// sampled keeps its last one, which is the honest reading: the number is stale,
// and the readiness probe is what says a dependency is unreachable.
//
// Param:
// ctx - context.Context (ends with the process)
// pools - *database.Pools (both pools, read but never written)
// bookings - *booking.Service (where the refund backlog is counted)
// watch - bootstrap.Observability (where the gauges are published)
func sampleGauges(ctx context.Context, pools *database.Pools, bookings *booking.Service, watch bootstrap.Observability) {
    ticker := time.NewTicker(sampleInterval)
    defer ticker.Stop()

    for {
        publishPools(pools, watch)
        publishReplicationLag(ctx, pools, watch)
        publishRefundBacklog(ctx, bookings, watch)

        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
        }
    }
}

// publishPools reads both pools, which needs no query at all.
func publishPools(pools *database.Pools, watch bootstrap.Observability) {
    primary := pools.PrimaryStatistics()
    replica := pools.ReplicaStatistics()

    watch.Metrics.Application.PoolConnections(observability.PoolPrimary, primary.Acquired, primary.Idle, primary.Total)
    watch.Metrics.Application.PoolConnections(observability.PoolReplica, replica.Acquired, replica.Idle, replica.Total)
}

// publishReplicationLag asks the replica how far behind it is.
func publishReplicationLag(ctx context.Context, pools *database.Pools, watch bootstrap.Observability) {
    queryCtx, cancel := context.WithTimeout(ctx, sampleTimeout)
    defer cancel()

    lag, err := pools.ReplicationLagBytes(queryCtx)
    if err != nil {
        // The gauge keeps its last value rather than being set to zero. Zero
        // would read as a replica that is perfectly caught up, which is the
        // opposite of what an unreadable replica means.
        return
    }

    watch.Metrics.Application.ReplicationLag(lag)
}

// publishRefundBacklog counts how many parents are owed money right now.
//
// It is a count of rows rather than a counter incremented when a refund is
// marked, because the question is how many are outstanding at this instant and
// not how many there have ever been. It is also the one number in this file
// where the alert built on it costs somebody real money if it is wrong.
func publishRefundBacklog(ctx context.Context, bookings *booking.Service, watch bootstrap.Observability) {
    queryCtx, cancel := context.WithTimeout(ctx, sampleTimeout)
    defer cancel()

    owed, err := bookings.Worklist(queryCtx, booking.StatusRefundRequired, refundWorklistLimit)
    if err != nil {
        return
    }

    watch.Metrics.Transaction.RefundPending(len(owed))
}
