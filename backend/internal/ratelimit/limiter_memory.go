package ratelimit

import (
    "context"
    "sync"
    "time"
)

// sweepThreshold is how many keys may be held before idle ones are dropped.
//
// The sweep is on write rather than on a timer, so this type carries no
// goroutine. The number only has to be large enough that a busy moment is not
// spent sweeping and small enough that a flood from many addresses cannot grow
// the map without bound.
const sweepThreshold = 1024

// MemoryLimiter is the fake every fast test runs against, and the implementation
// the service falls back to when there is no Redis to talk to.
//
// It counts within this process only, which is the limit worth being clear
// about: two api instances would each allow the full burst, so the effective
// limit is the stated one times the number of instances. That is why the Redis
// implementation exists, and why this one is honest about being a fallback
// rather than a choice.
type MemoryLimiter struct {
    mutex   sync.Mutex
    buckets map[string]State
}

// NewMemoryLimiter builds an empty limiter.
func NewMemoryLimiter() *MemoryLimiter {
    return &MemoryLimiter{buckets: make(map[string]State)}
}

// Allow applies one request to the bucket behind a key.
//
// The whole decision happens under one lock. Reading a count, deciding, and
// writing it back as three steps would let two parallel requests both find the
// last token, which is exactly the failure a rate limiter exists to prevent.
func (limiter *MemoryLimiter) Allow(_ context.Context, key string, bucket Bucket, now time.Time) (Decision, error) {
    if key == "" || !bucket.IsUsable() || now.IsZero() {
        return Decision{}, ErrInvalidRequest
    }

    limiter.mutex.Lock()
    defer limiter.mutex.Unlock()

    next, decision := bucket.Take(limiter.buckets[key], now)
    limiter.buckets[key] = next

    if len(limiter.buckets) > sweepThreshold {
        limiter.forgetIdleLocked(bucket, now)
    }

    return decision, nil
}

// Size reports how many buckets are held. It exists for the test that proves
// idle callers are forgotten rather than accumulated.
func (limiter *MemoryLimiter) Size() int {
    limiter.mutex.Lock()
    defer limiter.mutex.Unlock()

    return len(limiter.buckets)
}

// forgetIdleLocked drops every caller whose bucket has refilled completely.
//
// A full bucket is indistinguishable from one that has never been seen, so
// dropping it changes no decision. That is what makes this safe to do at any
// moment rather than only when nothing is in flight.
func (limiter *MemoryLimiter) forgetIdleLocked(bucket Bucket, now time.Time) {
    for key, state := range limiter.buckets {
        if now.Sub(state.UpdatedAt) >= bucket.FullRefill() {
            delete(limiter.buckets, key)
        }
    }
}
