package httpx

import (
    "sync/atomic"

    "ottodot-trial-booking/backend/internal/observability"
)

// MetricSink is where this surface's counts are published.
//
// It is an interface declared here rather than the metrics type itself, for the
// ordinary reason: a test drives this package without a Prometheus registry
// existing, and a fake sink is what lets a case assert that a refusal was
// recorded with the right reason rather than only that it happened.
//
// Nil is the ordinary state in a test and never the state in a running service.
type MetricSink interface {
    AccessDenied(reason string)
    RateLimitRejected(scope string)
    BotCheckRejected(check string)
    CacheLookup(endpoint string, result string)
    NotModified(route string)
    PanicRecovered()
    RequestObserved(route string, method string, status int, seconds float64)
    DeclareRoute(route string, method string)
}

// Counters are what this surface has done, for the tests that assert a request
// never reached the database and for the exposition.
//
// They are kept alongside the metric sink rather than replaced by it, and that
// is deliberate. A Prometheus counter can only be read back by scraping and
// parsing text, and a test that wants to know whether a class list was built
// twice should not have to do that. These are the same numbers, in the form a
// case can assert on.
//
// They are counts and nothing else. No identifier, no path with an id in it, and
// no parent: a metric label carrying a uuid is a time series per parent, which
// is both a leak and a way to run a monitoring system out of memory.
type Counters struct {
    sink MetricSink

    notModified    atomic.Int64
    cacheHit       atomic.Int64
    cacheMiss      atomic.Int64
    cacheError     atomic.Int64
    rateLimited    atomic.Int64
    botRejected    atomic.Int64
    accessDenied   atomic.Int64
    panicRecovered atomic.Int64
}

// Snapshot is the counts at one instant.
type Snapshot struct {
    NotModified    int64
    CacheHit       int64
    CacheMiss      int64
    CacheError     int64
    RateLimited    int64
    BotRejected    int64
    AccessDenied   int64
    PanicRecovered int64
}

// NewCounters builds a fresh set at zero.
//
// Param:
// sink - MetricSink (where the labelled counts are published, nil for nowhere)
//
// Return:
//   - the counters
func NewCounters(sink MetricSink) *Counters {
    return &Counters{sink: sink}
}

// NotModified records a conditional request answered without a body.
func (counters *Counters) NotModified(route string) {
    counters.notModified.Add(1)

    if counters.sink != nil {
        counters.sink.NotModified(route)
    }
}

// CacheHit records a body served from the store.
func (counters *Counters) CacheHit(endpoint string) {
    counters.cacheHit.Add(1)

    if counters.sink != nil {
        counters.sink.CacheLookup(endpoint, observability.ResultHit)
    }
}

// CacheMiss records a body that had to be built.
func (counters *Counters) CacheMiss(endpoint string) {
    counters.cacheMiss.Add(1)

    if counters.sink != nil {
        counters.sink.CacheLookup(endpoint, observability.ResultMiss)
    }
}

// CacheError records a store that could not be read, which is served as a miss.
func (counters *Counters) CacheError(endpoint string) {
    counters.cacheError.Add(1)

    if counters.sink != nil {
        counters.sink.CacheLookup(endpoint, observability.OutcomeError)
    }
}

// RateLimited records a caller turned away by the bucket.
func (counters *Counters) RateLimited(scope string) {
    counters.rateLimited.Add(1)

    if counters.sink != nil {
        counters.sink.RateLimitRejected(scope)
    }
}

// BotRejected records a submission the honeypot, the fill timer, or the
// challenge turned away.
//
// The check that caught it is recorded here and is never told to the caller. An
// operator needs to know which layer is doing the work, and a script told which
// layer caught it is a script that gets past that layer next time.
func (counters *Counters) BotRejected(check string) {
    counters.botRejected.Add(1)

    if counters.sink != nil {
        counters.sink.BotCheckRejected(check)
    }
}

// AccessDenied records a request refused for who it was rather than for what it
// asked.
func (counters *Counters) AccessDenied(reason string) {
    counters.accessDenied.Add(1)

    if counters.sink != nil {
        counters.sink.AccessDenied(reason)
    }
}

// PanicRecovered records a handler that fell over and was caught.
func (counters *Counters) PanicRecovered() {
    counters.panicRecovered.Add(1)

    if counters.sink != nil {
        counters.sink.PanicRecovered()
    }
}

// RequestObserved records one served request, by registered route.
func (counters *Counters) RequestObserved(route string, method string, status int, seconds float64) {
    if counters.sink != nil {
        counters.sink.RequestObserved(route, method, status, seconds)
    }
}

// DeclareRoute creates one route's timing series before any request arrives.
//
// It has no local count beside it, unlike the others here, because nothing in a
// test needs to assert that a route was registered: the router refusing to build
// is what says that.
func (counters *Counters) DeclareRoute(route string, method string) {
    if counters.sink != nil {
        counters.sink.DeclareRoute(route, method)
    }
}

// Snapshot reads every count at once.
func (counters *Counters) Snapshot() Snapshot {
    return Snapshot{
        NotModified:    counters.notModified.Load(),
        CacheHit:       counters.cacheHit.Load(),
        CacheMiss:      counters.cacheMiss.Load(),
        CacheError:     counters.cacheError.Load(),
        RateLimited:    counters.rateLimited.Load(),
        BotRejected:    counters.botRejected.Load(),
        AccessDenied:   counters.accessDenied.Load(),
        PanicRecovered: counters.panicRecovered.Load(),
    }
}
