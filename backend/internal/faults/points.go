// Package faults is the surface that makes a failure happen on purpose.
//
// It exists because of a specific problem with monitoring: a metric nobody has
// ever seen move is a decoration, and an alert nobody has ever seen fire is
// worse, because it is mistaken for coverage. The only way to trust
// `confirm_transaction_total{outcome="error"}` and the alert built on it is to
// break that transaction on a running stack and watch the number arrive.
//
// Everything here is guarded four ways and off by default. The guards are in
// `registry.go` and `handler.go` rather than in this file, and the reason the
// surface exists at all rather than being a build tag is written up in the
// backend's decision records: one binary and one command during a recording,
// instead of a rebuild in the middle of it.
package faults

// The points a fault can be injected at.
//
// Each one names exactly one place in the real code path, and each one simulates
// a failure that can genuinely happen. That is the rule this list is written to:
// a fault that has no real counterpart proves the metric moves and proves
// nothing about the system.
//
// The names are dotted rather than underscored so the part before the dot reads
// as where and the part after reads as what. They travel as a metric label, so
// they are a closed list for the same reason every other label value is.
const (
    // PointConfirmBeforeCommit fails inside the confirm transaction, after the
    // seat has been written and before the commit. It is the database dying
    // mid-transaction, and it is the one that answers the question: the whole
    // design rests on that transaction being all or nothing.
    PointConfirmBeforeCommit = "confirm.before_commit"

    // PointConfirmLockWait fails at the class row lock. It is a lock wait
    // timeout under contention, which is what a real overloaded database does
    // rather than hanging forever.
    PointConfirmLockWait = "confirm.lock_wait"

    // PointPaymentProviderError fails in the mock provider, and it is
    // deliberately not a decline. A decline is an answer. This is no answer at
    // all, which is the case where nobody knows whether money moved.
    PointPaymentProviderError = "payment.provider_error"

    // PointQueueJobError fails inside a worker job handler, so the retry and
    // then the parking can be watched happening.
    PointQueueJobError = "queue.job_error"

    // PointCacheRedisError fails at the Redis boundary. The request still has to
    // succeed from Postgres, because the cache is an optimisation and was never
    // allowed to be a dependency.
    PointCacheRedisError = "cache.redis_error"
)

// points is the closed list, in the order they are worth demonstrating.
var points = []string{
    PointConfirmBeforeCommit,
    PointConfirmLockWait,
    PointPaymentProviderError,
    PointQueueJobError,
    PointCacheRedisError,
}

// Points is every point that can be armed.
//
// Return:
//   - a copy of the list, so a caller cannot reorder or extend the real one
func Points() []string {
    listed := make([]string, len(points))
    copy(listed, points)

    return listed
}

// Known reports whether a name is a point this service has.
//
// Param:
// point - string (what a caller asked to arm)
//
// Return:
//   - true when the name is on the list above
func Known(point string) bool {
    for _, known := range points {
        if known == point {
            return true
        }
    }

    return false
}
