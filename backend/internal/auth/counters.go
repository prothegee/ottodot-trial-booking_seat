package auth

import "sync/atomic"

// Counters is what the auth surface knows about its own run.
//
// They are counts rather than gauges: a count only goes up, so a scrape landing
// between two calls cannot read a number that was briefly wrong.
//
// No count is per parent and none carries a label. A "reuse detected by parent"
// counter would be a uuid in a metric label, which is exactly what the
// sensitive data rules forbid.
type Counters struct {
    sink MetricSink

    rotated       atomic.Int64
    reuseDetected atomic.Int64
    loginRefused  atomic.Int64
    tokensIssued  atomic.Int64
}

// NewCounters builds a fresh set, all at zero.
//
// Param:
// sink - MetricSink (where the same counts are published, nil for nowhere)
//
// Return:
//   - the counters
func NewCounters(sink MetricSink) *Counters {
    return &Counters{sink: sink}
}

// Rotated records one refresh that produced a successor.
func (counters *Counters) Rotated() {
    counters.rotated.Add(1)

    if counters.sink != nil {
        counters.sink.RefreshRotated()
    }
}

// ReuseDetected records one spent refresh token presented again, which is the
// number worth alerting on: it is either a stolen token or a client bug, and
// both want a person to look.
func (counters *Counters) ReuseDetected() {
    counters.reuseDetected.Add(1)

    if counters.sink != nil {
        counters.sink.RefreshReuseDetected()
    }
}

// LoginRefused records one sign in that matched no account.
func (counters *Counters) LoginRefused() {
    counters.loginRefused.Add(1)

    if counters.sink != nil {
        counters.sink.LoginRefused()
    }
}

// TokenIssued records one token handed out.
//
// Both kinds are counted here and told apart only by the label on the metric.
// A session that issues an access token without a refresh token, or the other
// way round, would be a bug rather than a number worth two atomics.
func (counters *Counters) TokenIssued(kind string) {
    counters.tokensIssued.Add(1)

    if counters.sink != nil {
        counters.sink.TokenIssued(kind)
    }
}

// CounterSnapshot is the counts read together.
type CounterSnapshot struct {
    Rotated       int64
    ReuseDetected int64
    LoginRefused  int64
    TokensIssued  int64
}

// Snapshot reads every count.
func (counters *Counters) Snapshot() CounterSnapshot {
    return CounterSnapshot{
        Rotated:       counters.rotated.Load(),
        ReuseDetected: counters.reuseDetected.Load(),
        LoginRefused:  counters.loginRefused.Load(),
        TokensIssued:  counters.tokensIssued.Load(),
    }
}
