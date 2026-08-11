package httpx

import "sync/atomic"

// The metric names this surface produces.
//
// They are constants because phase 7 exports them and a dashboard queries them
// by exact name. A metric whose name only exists inside a Grafana panel is a
// metric that gets renamed and silently orphaned.
const (
    MetricNotModified    = "http_not_modified_total"
    MetricCacheHit       = "cache_lookup_total{result=\"hit\"}"
    MetricCacheMiss      = "cache_lookup_total{result=\"miss\"}"
    MetricCacheError     = "cache_lookup_total{result=\"error\"}"
    MetricRateLimited    = "rate_limit_rejected_total"
    MetricBotRejected    = "bot_check_rejected_total"
    MetricAccessDenied   = "access_denied_total"
    MetricPanicRecovered = "panic_recovered_total"
)

// Counters are what this surface has done, for the exposition and for the tests
// that assert a request never reached the database.
//
// They are counts and nothing else. No identifier, no path with an id in it, and
// no parent: a metric label carrying a uuid is a time series per parent, which
// is both a leak and a way to run a monitoring system out of memory.
type Counters struct {
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
func NewCounters() *Counters {
    return &Counters{}
}

// NotModified records a conditional request answered without a body.
func (counters *Counters) NotModified() {
    counters.notModified.Add(1)
}

// CacheHit records a body served from the store.
func (counters *Counters) CacheHit() {
    counters.cacheHit.Add(1)
}

// CacheMiss records a body that had to be built.
func (counters *Counters) CacheMiss() {
    counters.cacheMiss.Add(1)
}

// CacheError records a store that could not be read, which is served as a miss.
func (counters *Counters) CacheError() {
    counters.cacheError.Add(1)
}

// RateLimited records a caller turned away by the bucket.
func (counters *Counters) RateLimited() {
    counters.rateLimited.Add(1)
}

// BotRejected records a submission the honeypot, the fill timer, or the
// challenge turned away.
func (counters *Counters) BotRejected() {
    counters.botRejected.Add(1)
}

// AccessDenied records a request refused for who it was rather than for what it
// asked.
func (counters *Counters) AccessDenied() {
    counters.accessDenied.Add(1)
}

// PanicRecovered records a handler that fell over and was caught.
func (counters *Counters) PanicRecovered() {
    counters.panicRecovered.Add(1)
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
