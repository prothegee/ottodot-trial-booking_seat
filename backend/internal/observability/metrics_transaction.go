package observability

import "github.com/prometheus/client_golang/prometheus"

// The values the transaction labels are allowed to take.
const (
    OutcomeCommit   = "commit"
    OutcomeRollback = "rollback"
    OutcomeConflict = "conflict"

    OutcomeConfirmed = "confirmed"
    OutcomeSeatLost  = "seat_lost"
    OutcomeError     = "error"

    OutcomeSettled  = "settled"
    OutcomeDeclined = "declined"
)

// confirmBuckets are the boundaries the confirm transaction is timed against.
//
// They are tighter than the library's defaults because this transaction takes a
// row lock that every other booking for the same class waits behind. The
// question worth answering is whether it is taking single milliseconds or tens
// of them, and the default buckets start too coarse to tell those apart.
var confirmBuckets = []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

// TransactionMetrics is what the parts of this service that move money or seats
// did, and whether it worked.
//
// One distinction in here carries the whole group. A confirm that rolls back
// because another parent took the last seat is correct behaviour, and a confirm
// that rolls back because the transaction broke is not. Counting both as a
// rollback would make the healthy case and the broken case the same number, so
// `confirm_transaction_total` separates seat_lost from error and only error is
// alerted on.
type TransactionMetrics struct {
    database *prometheus.CounterVec
    confirm  *prometheus.CounterVec
    duration prometheus.Histogram
    payment  *prometheus.CounterVec
    jobFail  *prometheus.CounterVec
    refunds  prometheus.Gauge
}

// newTransactionMetrics builds the group and registers it.
func newTransactionMetrics(registry prometheus.Registerer) *TransactionMetrics {
    metrics := &TransactionMetrics{
        database: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricDatabaseTransaction,
            Help: "Database transactions by name and how they ended.",
        }, []string{LabelName, LabelOutcome}),

        confirm: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricConfirmTransaction,
            Help: "Seat confirmations, split into taken, lost to another parent, and broken.",
        }, []string{LabelOutcome}),

        duration: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    MetricConfirmDurationSeconds,
            Help:    "How long the confirm transaction held its lock.",
            Buckets: confirmBuckets,
        }),

        payment: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricPaymentAttempt,
            Help: "Charges attempted, split into settled, declined by the provider, and broken.",
        }, []string{LabelOutcome}),

        jobFail: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricQueueJobFailed,
            Help: "Queue jobs that failed an attempt, by job kind.",
        }, []string{LabelKind}),

        refunds: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: MetricRefundPendingBookings,
            Help: "Bookings where a parent has been charged and not yet refunded.",
        }),
    }

    registry.MustRegister(
        metrics.database,
        metrics.confirm,
        metrics.duration,
        metrics.payment,
        metrics.jobFail,
        metrics.refunds)

    for _, outcome := range []string{OutcomeConfirmed, OutcomeSeatLost, OutcomeError} {
        metrics.confirm.WithLabelValues(outcome)
    }

    for _, outcome := range []string{OutcomeSettled, OutcomeDeclined, OutcomeError} {
        metrics.payment.WithLabelValues(outcome)
    }

    return metrics
}

// DatabaseTransaction records one transaction and how it ended.
//
// Param:
// name - string (which transaction, a fixed name from the caller and never a query)
// outcome - string (commit, rollback, or conflict)
func (metrics *TransactionMetrics) DatabaseTransaction(name string, outcome string) {
    if metrics == nil {
        return
    }

    switch outcome {
    case OutcomeCommit, OutcomeRollback, OutcomeConflict:
    default:
        outcome = OutcomeRollback
    }

    metrics.database.WithLabelValues(name, outcome).Inc()
}

// ConfirmTransaction records one seat confirmation attempt and how long it took.
//
// The count and the timing are one call rather than two, so a confirm can never
// be counted without being timed or timed without being counted.
//
// Param:
// outcome - string (confirmed, seat_lost, or error)
// seconds - float64 (how long the transaction ran)
func (metrics *TransactionMetrics) ConfirmTransaction(outcome string, seconds float64) {
    if metrics == nil {
        return
    }

    switch outcome {
    case OutcomeConfirmed, OutcomeSeatLost, OutcomeError:
    default:
        outcome = OutcomeError
    }

    metrics.confirm.WithLabelValues(outcome).Inc()
    metrics.duration.Observe(seconds)
}

// PaymentAttempt records one charge and how it ended.
//
// A decline is not an error. A card that was refused worked exactly as designed,
// and mixing the two would make a bad afternoon at a card issuer look like this
// service falling over.
func (metrics *TransactionMetrics) PaymentAttempt(outcome string) {
    if metrics == nil {
        return
    }

    switch outcome {
    case OutcomeSettled, OutcomeDeclined, OutcomeError:
    default:
        outcome = OutcomeError
    }

    metrics.payment.WithLabelValues(outcome).Inc()
}

// QueueJobFailed records one job attempt that did not finish.
func (metrics *TransactionMetrics) QueueJobFailed(kind string) {
    if metrics == nil {
        return
    }

    metrics.jobFail.WithLabelValues(kind).Inc()
}

// RefundPending publishes how many parents are owed money right now.
//
// It is a gauge read from the database rather than a counter incremented in
// code, because the question is how many are outstanding at this instant and not
// how many there have ever been. It is also the only alert in this project where
// ignoring it costs somebody real money.
func (metrics *TransactionMetrics) RefundPending(count int) {
    if metrics == nil {
        return
    }

    metrics.refunds.Set(float64(count))
}
