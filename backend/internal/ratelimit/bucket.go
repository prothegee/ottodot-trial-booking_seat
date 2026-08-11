// Package ratelimit answers one question: has this caller asked too often.
//
// The shape is a token bucket rather than a fixed window, and the difference is
// worth stating because it is the whole reason for the arithmetic in this file.
// A fixed window lets a caller spend its whole allowance at the end of one
// window and again at the start of the next, so a limit of 10 a minute permits
// 20 in a second. A bucket refills continuously, so the burst is the burst and
// nothing lines up with a clock boundary.
//
// The package is split so the rule can be read without a server in front of it.
// The arithmetic is pure and lives here. The limiter is the part that holds
// state, and it has two implementations behind one interface so the same suite
// runs against both.
package ratelimit

import "time"

// Bucket is the rule: how many requests may arrive together, and how quickly the
// allowance comes back.
type Bucket struct {
    // Burst is how many requests a caller may make at once from a full bucket.
    // It is also the size of the bucket, so it is the most a caller can ever
    // have saved up.
    Burst int

    // Interval is how long one token takes to return. A burst of 10 with an
    // interval of six seconds is ten at once and then one every six seconds,
    // which is the same sustained rate as ten a minute without the boundary
    // problem a window has.
    Interval time.Duration
}

// IsUsable reports whether this rule can be applied. A bucket that cannot hold a
// token, or that never refills, would refuse everything forever.
func (bucket Bucket) IsUsable() bool {
    return bucket.Burst >= 1 && bucket.Interval > 0
}

// FullRefill is how long an empty bucket takes to fill completely. It is what
// bounds how long state is worth keeping: past it, a caller is indistinguishable
// from one that has never been seen.
func (bucket Bucket) FullRefill() time.Duration {
    return time.Duration(bucket.Burst) * bucket.Interval
}

// State is what one caller's bucket holds. It is a value, so the arithmetic can
// be applied by any implementation to state it read from anywhere.
type State struct {
    // Tokens is the allowance left, carried as a fraction so a partial refill
    // is not thrown away on every request.
    Tokens float64

    // UpdatedAt is when Tokens was last correct. A zero value means this caller
    // has not been seen, which starts them with a full bucket.
    UpdatedAt time.Time
}

// Decision is the answer for one request.
type Decision struct {
    // Allowed is whether the request may go on.
    Allowed bool

    // Remaining is whole tokens left after this request. It goes on a response
    // header, so a well behaved client can slow down before being refused.
    Remaining int

    // RetryAfter is how long until one token is back. It is zero on an allowed
    // request, and it is what fills retry_after_seconds in the refusal.
    RetryAfter time.Duration
}

// Take applies one request to a bucket.
//
// It is a pure function of the rule, the state, and the instant, which is what
// lets both implementations share it: the memory limiter holds State in a map,
// the Redis one holds the same two numbers in a hash, and neither owns the rule.
//
// Note:
//   - a clock that moves backwards is treated as no time passing rather than as
//     time being taken away. Two api instances rarely agree to the millisecond,
//     and a caller must not lose allowance because the second one is behind.
//
// Param:
// state - State (what this caller held, zero for one never seen)
// now - time.Time (the instant the request arrived)
//
// Return:
//   - the state to store back
//   - the decision for this request
func (bucket Bucket) Take(state State, now time.Time) (State, Decision) {
    if !bucket.IsUsable() {
        return state, Decision{}
    }

    tokens := float64(bucket.Burst)

    if !state.UpdatedAt.IsZero() {
        elapsed := now.Sub(state.UpdatedAt)
        if elapsed < 0 {
            elapsed = 0
        }

        tokens = state.Tokens + float64(elapsed)/float64(bucket.Interval)
    }

    if tokens > float64(bucket.Burst) {
        tokens = float64(bucket.Burst)
    }

    if tokens < 1 {
        // The state is still written back, so the refill that has accrued is
        // not lost by being refused.
        return State{Tokens: tokens, UpdatedAt: now}, Decision{
            Allowed:    false,
            Remaining:  0,
            RetryAfter: waitFor(1-tokens, bucket.Interval),
        }
    }

    tokens--

    return State{Tokens: tokens, UpdatedAt: now}, Decision{
        Allowed:   true,
        Remaining: int(tokens),
    }
}

// waitFor is how long it takes for a fraction of a token to return, rounded up.
//
// Rounding up matters: a client told to wait 0 seconds retries immediately and
// is refused again, which turns one refusal into a loop.
func waitFor(missing float64, interval time.Duration) time.Duration {
    wait := time.Duration(missing * float64(interval))

    if remainder := wait % time.Second; remainder != 0 {
        wait += time.Second - remainder
    }

    if wait < time.Second {
        wait = time.Second
    }

    return wait
}
