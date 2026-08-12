package observability

import "github.com/prometheus/client_golang/prometheus"

// The funnel steps and cache results the client is allowed to report.
//
// The list is here rather than in the client for one reason: the client is code
// somebody else's browser is running, and anything it sends has to be treated as
// a claim rather than a fact. A step this service has not heard of is dropped,
// so a modified page cannot create series at will.
const (
    StepList      = "list"
    StepHold      = "hold"
    StepPay       = "pay"
    StepConfirmed = "confirmed"

    ClientResultFresh       = "fresh"
    ClientResultStale       = "stale"
    ClientResultRevalidated = "revalidated"
    ClientResultMiss        = "miss"
)

// The client routes this service will keep a series for.
//
// They are route patterns rather than paths, so `/booking/[bookingId]` is one
// series and not one per booking.
var knownRoutes = map[string]bool{
    "/":                    true,
    "/sign-in":             true,
    "/book/[classId]":      true,
    "/pay/[bookingId]":     true,
    "/booking/[bookingId]": true,
    "/bookings":            true,
    "/roster/[classId]":    true,
    "/status":              true,
}

// The typed api codes the client is allowed to report.
//
// It is the same closed set the api answers with, written out here rather than
// imported, because httpx is the package that serves this endpoint and importing
// it back would be a cycle. The test that holds the two lists together is what
// stops them drifting.
var knownCodes = map[string]bool{
    "invalid_request":        true,
    "token_expired":          true,
    "token_invalid":          true,
    "token_reused":           true,
    "not_your_child":         true,
    "forbidden_role":         true,
    "payment_declined":       true,
    "already_booked":         true,
    "too_many_holds":         true,
    "class_full":             true,
    "seat_lost":              true,
    "rate_limited":           true,
    "internal_error":         true,
    "dependency_unavailable": true,
}

// pageLoadBuckets are the boundaries a client page load is timed against.
//
// They start at 50 milliseconds and end at 8 seconds, which is the range a
// browser actually lands in. The library's defaults top out at ten seconds with
// most of their resolution under one, and a client on a slow connection spends
// most of its time above that.
var pageLoadBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 4, 8}

// FrontendMetrics is what the browser reports about itself.
//
// The client cannot be scraped, so it posts events and this service turns them
// into series. That indirection is the reason every label value here is checked
// against a fixed list before it is used: a scrape target is something this
// project runs, and a telemetry endpoint is something anybody can post to.
type FrontendMetrics struct {
    pageLoad *prometheus.HistogramVec
    apiError *prometheus.CounterVec
    funnel   *prometheus.CounterVec
    cache    *prometheus.CounterVec
}

// newFrontendMetrics builds the group and registers it.
func newFrontendMetrics(registry prometheus.Registerer) *FrontendMetrics {
    metrics := &FrontendMetrics{
        pageLoad: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    MetricFrontendPageLoadSeconds,
            Help:    "How long a client route took to become usable.",
            Buckets: pageLoadBuckets,
        }, []string{LabelRoute}),

        apiError: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricFrontendApiError,
            Help: "Typed api failures the client was shown, by code.",
        }, []string{LabelCode}),

        funnel: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricFrontendBookingFunnel,
            Help: "How far into a booking a parent got, by step.",
        }, []string{LabelStep}),

        cache: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: MetricFrontendCacheLookup,
            Help: "The client's own cache reads, by result.",
        }, []string{LabelResult}),
    }

    registry.MustRegister(metrics.pageLoad, metrics.apiError, metrics.funnel, metrics.cache)

    // Every route, so the panel has a count to average over before anybody has
    // opened the site. A percentile of no page loads is not a number at all, and
    // the average beside it is what keeps that panel drawing.
    for route := range knownRoutes {
        metrics.pageLoad.WithLabelValues(route)
    }

    // The funnel is created at zero end to end, so the drop off between two
    // steps is a real ratio from the first booking rather than a panel that
    // renders nothing until somebody happens to reach the last step.
    for _, step := range []string{StepList, StepHold, StepPay, StepConfirmed} {
        metrics.funnel.WithLabelValues(step)
    }

    for _, result := range []string{ClientResultFresh, ClientResultStale, ClientResultRevalidated, ClientResultMiss} {
        metrics.cache.WithLabelValues(result)
    }

    // Every code, for the same reason, and it matters more here than anywhere
    // else on this dashboard. The three code panels each select a group of these
    // by name, so a group nobody has hit yet has no series at all and the panel
    // reads No data. That is the same thing the panel shows when the client
    // stopped reporting, and the two must not look alike: one is a quiet
    // afternoon and the other is broken telemetry.
    for code := range knownCodes {
        metrics.apiError.WithLabelValues(code)
    }

    return metrics
}

// Every method below drops a value it does not recognise rather than folding it
// into a known one, which is the opposite of what the other three groups do.
//
// The difference is where the value comes from. Every label the api records
// about itself is a constant written into the code, so an unrecognised one is a
// typo worth folding into the nearest sensible series. Everything here arrives
// in a post from a browser, so an unrecognised one is either an old page or
// somebody trying to write into the monitoring system, and neither deserves a
// series at all.
//
// The telemetry converter checks the same lists one layer up. That is deliberate
// duplication: this is the only endpoint on the service that takes label values
// from outside, and it is worth being unable to get wrong from either direction.

// PageLoad records one client route becoming usable.
func (metrics *FrontendMetrics) PageLoad(route string, seconds float64) {
    if metrics == nil || !knownRoutes[route] {
        return
    }

    metrics.pageLoad.WithLabelValues(route).Observe(seconds)
}

// ApiError records one typed failure the client showed a parent.
func (metrics *FrontendMetrics) ApiError(code string) {
    if metrics == nil || !knownCodes[code] {
        return
    }

    metrics.apiError.WithLabelValues(code).Inc()
}

// FunnelStep records one step a parent reached.
func (metrics *FrontendMetrics) FunnelStep(step string) {
    if metrics == nil || !KnownStep(step) {
        return
    }

    metrics.funnel.WithLabelValues(step).Inc()
}

// ClientCacheLookup records one read from the client's own cache.
func (metrics *FrontendMetrics) ClientCacheLookup(result string) {
    if metrics == nil || !KnownClientResult(result) {
        return
    }

    metrics.cache.WithLabelValues(result).Inc()
}

// KnownRoute reports whether a client route is one this service keeps a series
// for.
func KnownRoute(route string) bool {
    return knownRoutes[route]
}

// KnownCode reports whether a typed failure code is one the api answers with.
func KnownCode(code string) bool {
    return knownCodes[code]
}

// KnownStep reports whether a funnel step is one of the four.
func KnownStep(step string) bool {
    switch step {
    case StepList, StepHold, StepPay, StepConfirmed:
        return true
    default:
        return false
    }
}

// KnownClientResult reports whether a client cache result is one of the four.
func KnownClientResult(result string) bool {
    switch result {
    case ClientResultFresh, ClientResultStale, ClientResultRevalidated, ClientResultMiss:
        return true
    default:
        return false
    }
}
