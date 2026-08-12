package observability

import (
    "math"
    "net/http"
    "strconv"

    "github.com/prometheus/client_golang/prometheus"
)

// The values the application labels are allowed to take.
const (
    OutcomeGranted = "granted"
    OutcomeRefused = "refused"

    ResultHit   = "hit"
    ResultMiss  = "miss"
    ResultStale = "stale"

    PoolPrimary = "primary"
    PoolReplica = "replica"

    PoolStateAcquired = "acquired"
    PoolStateIdle     = "idle"
    PoolStateTotal    = "total"

    QueueStateReady   = "ready"
    QueueStateClaimed = "claimed"
    QueueStateParked  = "parked"
)

// queueStates is the closed list the depth gauge is published for, named once so
// the method writing a reading and the method writing that there is none cannot
// disagree about which series they cover.
var queueStates = []string{QueueStateReady, QueueStateClaimed, QueueStateParked}

// ApplicationMetrics is everything that is neither a resource number nor a
// failure count: what the service is doing when it is working.
//
// The route label is the registered pattern and never the request path. A path
// carries a booking identifier, and a metric label carrying an identifier is one
// time series per booking, which is both a leak of who booked what and a way to
// exhaust the monitoring host over a weekend.
type ApplicationMetrics struct {
    requests    *prometheus.HistogramVec
    notModified *prometheus.CounterVec
    panics      prometheus.Counter

    holdGranted       *prometheus.CounterVec
    confirmed         prometheus.Counter
    raceLost          prometheus.Counter
    duplicateRejected prometheus.Counter
    holdExpired       prometheus.Counter

    queueDepth       *prometheus.GaugeVec
    queueDepthUnread prometheus.Counter
    queueDuration    *prometheus.HistogramVec
    jobsClaimed      prometheus.Counter
    jobsCompleted    prometheus.Counter

    cacheLookup    *prometheus.CounterVec
    poolConnection *prometheus.GaugeVec
    replicationLag prometheus.Gauge

    faultEnabled  prometheus.Gauge
    faultInjected *prometheus.CounterVec
}

// newApplicationMetrics builds the group and registers it.
func newApplicationMetrics(registry prometheus.Registerer) *ApplicationMetrics {
    metrics := &ApplicationMetrics{
        requests: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    MetricRequestDurationSeconds,
            Help:    "Request latency by registered route, method, and status.",
            Buckets: prometheus.DefBuckets,
        }, []string{LabelRoute, LabelMethod, LabelStatus}),

        notModified: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricNotModified,
            Help: "Conditional reads answered without a body.",
        }, []string{LabelRoute}),

        panics: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricPanicRecovered,
            Help: "Handlers that fell over and were caught, costing one request rather than the process.",
        }),

        holdGranted: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricHoldGranted,
            Help: "Seat holds asked for, and whether one was granted.",
        }, []string{LabelOutcome}),

        confirmed: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricBookingConfirmed,
            Help: "Seats confirmed and paid for.",
        }),

        raceLost: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricRaceLost,
            Help: "Confirmations that lost the last seat to another parent, which is correct behaviour.",
        }),

        duplicateRejected: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricDuplicateRejected,
            Help: "Bookings refused because the child already has a live one for that class.",
        }),

        holdExpired: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricHoldExpired,
            Help: "Holds the worker released because they were never paid for.",
        }),

        queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
            Name: MetricQueueDepth,
            Help: "Jobs the queue holds right now, by state.",
        }, []string{LabelState}),

        queueDepthUnread: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricQueueDepthUnread,
            Help: "Scrapes where the queue could not be asked how deep it is.",
        }),

        queueDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    MetricQueueJobDuration,
            Help:    "How long a job took, by kind and how it ended.",
            Buckets: prometheus.DefBuckets,
        }, []string{LabelKind, LabelOutcome}),

        jobsClaimed: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricWorkerJobsClaimed,
            Help: "Jobs this worker took from the queue.",
        }),

        jobsCompleted: prometheus.NewCounter(prometheus.CounterOpts{
            Name: MetricWorkerJobsCompleted,
            Help: "Jobs this worker finished and removed.",
        }),

        cacheLookup: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricCacheLookup,
            Help: "Response cache reads by document and result.",
        }, []string{LabelEndpoint, LabelResult}),

        poolConnection: prometheus.NewGaugeVec(prometheus.GaugeOpts{
            Name: MetricDatabasePoolConnections,
            Help: "Connections held by each pool, by state.",
        }, []string{LabelPool, LabelState}),

        replicationLag: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: MetricReplicationLagBytes,
            Help: "How far the replica is behind the primary, in bytes of write ahead log.",
        }),

        faultEnabled: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: MetricFaultInjectionEnabled,
            Help: "One while the fault injection surface is live, which is never outside development.",
        }),

        faultInjected: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricFaultInjected,
            Help: "Faults deliberately triggered, by point.",
        }, []string{LabelPoint}),
    }

    registry.MustRegister(
        metrics.requests,
        metrics.notModified,
        metrics.panics,
        metrics.holdGranted,
        metrics.confirmed,
        metrics.raceLost,
        metrics.duplicateRejected,
        metrics.holdExpired,
        metrics.queueDepth,
        metrics.queueDepthUnread,
        metrics.queueDuration,
        metrics.jobsClaimed,
        metrics.jobsCompleted,
        metrics.cacheLookup,
        metrics.poolConnection,
        metrics.replicationLag,
        metrics.faultEnabled,
        metrics.faultInjected)

    for _, outcome := range []string{OutcomeGranted, OutcomeRefused} {
        metrics.holdGranted.WithLabelValues(outcome)
    }

    for _, result := range []string{ResultHit, ResultMiss, ResultStale, OutcomeError} {
        metrics.cacheLookup.WithLabelValues("classes", result)
    }

    return metrics
}

// RequestObserved records one served request.
//
// Param:
// route - string (the registered pattern, never the path that was asked for)
// method - string (the http method)
// status - int (what went out)
// seconds - float64 (how long the whole chain took, middleware included)
func (metrics *ApplicationMetrics) RequestObserved(route string, method string, status int, seconds float64) {
    if metrics == nil {
        return
    }

    metrics.requests.WithLabelValues(route, method, strconv.Itoa(status)).Observe(seconds)
}

// DeclareRoute creates one registered route's timing series at zero.
//
// Note:
//   - status 200 only. That label is the one on this metric whose values are not
//     a closed list, and inventing a series for every code a route might answer
//     would be guessing rather than declaring
//
// Param:
// route - string (the registered pattern, never a path that arrived)
// method - string (the method that pattern is registered for)
func (metrics *ApplicationMetrics) DeclareRoute(route string, method string) {
    if metrics == nil {
        return
    }

    metrics.requests.WithLabelValues(route, method, strconv.Itoa(http.StatusOK))
}

// NotModified records one conditional read answered with no body.
func (metrics *ApplicationMetrics) NotModified(route string) {
    if metrics == nil {
        return
    }

    metrics.notModified.WithLabelValues(route).Inc()
}

// PanicRecovered records one handler that fell over.
func (metrics *ApplicationMetrics) PanicRecovered() {
    if metrics == nil {
        return
    }

    metrics.panics.Inc()
}

// HoldGranted records one hold request and whether a seat was set aside.
func (metrics *ApplicationMetrics) HoldGranted(outcome string) {
    if metrics == nil {
        return
    }

    if outcome != OutcomeGranted {
        outcome = OutcomeRefused
    }

    metrics.holdGranted.WithLabelValues(outcome).Inc()
}

// BookingConfirmed records one seat taken.
//
// It carries no label. The metric table in the plan proposed a class subject
// label here, and the confirm path does not read the class row, so populating it
// would mean adding a query inside the transaction that holds the lock every
// other parent for that class is waiting behind. A dashboard breakdown is not
// worth lengthening the one transaction the whole design exists to keep short.
func (metrics *ApplicationMetrics) BookingConfirmed() {
    if metrics == nil {
        return
    }

    metrics.confirmed.Inc()
}

// RaceLost records one parent who reached the last seat second.
func (metrics *ApplicationMetrics) RaceLost() {
    if metrics == nil {
        return
    }

    metrics.raceLost.Inc()
}

// DuplicateRejected records one booking refused as a repeat.
func (metrics *ApplicationMetrics) DuplicateRejected() {
    if metrics == nil {
        return
    }

    metrics.duplicateRejected.Inc()
}

// HoldExpired records one hold the worker released.
func (metrics *ApplicationMetrics) HoldExpired() {
    if metrics == nil {
        return
    }

    metrics.holdExpired.Inc()
}

// QueueDepth publishes what the queue holds right now.
func (metrics *ApplicationMetrics) QueueDepth(ready int, claimed int, parked int) {
    if metrics == nil {
        return
    }

    metrics.queueDepth.WithLabelValues(QueueStateReady).Set(float64(ready))
    metrics.queueDepth.WithLabelValues(QueueStateClaimed).Set(float64(claimed))
    metrics.queueDepth.WithLabelValues(QueueStateParked).Set(float64(parked))
}

// QueueDepthUnknown publishes that the queue could not be asked how deep it is.
//
// Note:
//   - not-a-number, because zero reads as an empty queue and leaving the gauge
//     alone keeps serving the last reading as though it were current
//   - the counter beside it is what an alert fires on, since a comparison
//     against not-a-number is always false
func (metrics *ApplicationMetrics) QueueDepthUnknown() {
    if metrics == nil {
        return
    }

    for _, state := range queueStates {
        metrics.queueDepth.WithLabelValues(state).Set(math.NaN())
    }

    metrics.queueDepthUnread.Inc()
}

// QueueJob records one finished job attempt.
func (metrics *ApplicationMetrics) QueueJob(kind string, outcome string, seconds float64) {
    if metrics == nil {
        return
    }

    metrics.queueDuration.WithLabelValues(kind, outcome).Observe(seconds)
}

// JobsClaimed records jobs handed to this worker.
func (metrics *ApplicationMetrics) JobsClaimed(jobs int) {
    if metrics == nil || jobs <= 0 {
        return
    }

    metrics.jobsClaimed.Add(float64(jobs))
}

// JobsCompleted records one job that finished and was removed.
func (metrics *ApplicationMetrics) JobsCompleted() {
    if metrics == nil {
        return
    }

    metrics.jobsCompleted.Inc()
}

// CacheLookup records one response cache read.
//
// Param:
// endpoint - string (which document, from the cache package's key builders)
// result - string (hit, miss, stale, or error)
func (metrics *ApplicationMetrics) CacheLookup(endpoint string, result string) {
    if metrics == nil {
        return
    }

    metrics.cacheLookup.WithLabelValues(endpoint, result).Inc()
}

// PoolConnections publishes how one pool is doing.
func (metrics *ApplicationMetrics) PoolConnections(pool string, acquired int32, idle int32, total int32) {
    if metrics == nil {
        return
    }

    metrics.poolConnection.WithLabelValues(pool, PoolStateAcquired).Set(float64(acquired))
    metrics.poolConnection.WithLabelValues(pool, PoolStateIdle).Set(float64(idle))
    metrics.poolConnection.WithLabelValues(pool, PoolStateTotal).Set(float64(total))
}

// ReplicationLag publishes how far behind the replica is.
func (metrics *ApplicationMetrics) ReplicationLag(bytes int64) {
    if metrics == nil {
        return
    }

    metrics.replicationLag.Set(float64(bytes))
}

// FaultInjectionEnabled publishes whether the fault surface is live.
//
// It is set on every start, including when the surface is off, so the series
// always exists. A banner bound to a metric that is simply absent would read the
// same as a banner bound to a metric that says zero, and the whole point of the
// banner is that nobody mistakes a deliberately broken stack for a healthy one.
func (metrics *ApplicationMetrics) FaultInjectionEnabled(enabled bool) {
    if metrics == nil {
        return
    }

    value := 0.0
    if enabled {
        value = 1
    }

    metrics.faultEnabled.Set(value)
}

// FaultInjected records one fault that actually fired.
func (metrics *ApplicationMetrics) FaultInjected(point string) {
    if metrics == nil {
        return
    }

    metrics.faultInjected.WithLabelValues(point).Inc()
}

// DeclareFaultPoints creates the trigger series for every point at zero.
//
// Note:
//   - it is the counter half of the argument the gauge beside it already makes.
//     A panel bound to a metric that is simply absent reads the same as one
//     bound to a metric that says nothing has fired, and only one of those is
//     news
//   - the points come from the caller, which is the package that owns the list
//
// Param:
// points - []string (every point this service can be broken at)
func (metrics *ApplicationMetrics) DeclareFaultPoints(points []string) {
    if metrics == nil {
        return
    }

    for _, point := range points {
        metrics.faultInjected.WithLabelValues(point)
    }
}
