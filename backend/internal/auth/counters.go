package auth

import "sync/atomic"

// The metric names this package's counts are published under in phase 7.
//
// They are named here rather than in the observability package so the name and
// the thing it counts sit in one file. A dashboard panel that queries a name
// nobody increments is the failure this avoids.
const (
	MetricRefreshRotated       = "auth_refresh_rotated_total"
	MetricRefreshReuseDetected = "auth_refresh_reuse_detected_total"
	MetricLoginRefused         = "auth_login_refused_total"
)

// Counters is what the auth surface knows about its own run.
//
// They are counts rather than gauges: a count only goes up, so a scrape landing
// between two calls cannot read a number that was briefly wrong.
//
// No count is per parent and none carries a label. A "reuse detected by parent"
// counter would be a uuid in a metric label, which is exactly what the
// sensitive data rules forbid.
type Counters struct {
	rotated       atomic.Int64
	reuseDetected atomic.Int64
	loginRefused  atomic.Int64
}

// NewCounters builds a fresh set, all at zero.
func NewCounters() *Counters {
	return &Counters{}
}

// Rotated records one refresh that produced a successor.
func (counters *Counters) Rotated() {
	counters.rotated.Add(1)
}

// ReuseDetected records one spent refresh token presented again, which is the
// number worth alerting on: it is either a stolen token or a client bug, and
// both want a person to look.
func (counters *Counters) ReuseDetected() {
	counters.reuseDetected.Add(1)
}

// LoginRefused records one sign in that matched no account.
func (counters *Counters) LoginRefused() {
	counters.loginRefused.Add(1)
}

// CounterSnapshot is the three counts read together.
type CounterSnapshot struct {
	Rotated       int64
	ReuseDetected int64
	LoginRefused  int64
}

// Snapshot reads all three counts.
func (counters *Counters) Snapshot() CounterSnapshot {
	return CounterSnapshot{
		Rotated:       counters.rotated.Load(),
		ReuseDetected: counters.reuseDetected.Load(),
		LoginRefused:  counters.loginRefused.Load(),
	}
}
