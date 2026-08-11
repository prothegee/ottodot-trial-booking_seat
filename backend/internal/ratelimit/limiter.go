package ratelimit

import (
    "context"
    "time"
)

// The rules the api applies, in one place so a limit is read rather than
// guessed.
//
// Writes are tighter than reads because a write costs a transaction and a read
// costs a cached body. The write rule is also the one a griefer would push at:
// twelve holds at once is more than any real parent needs and few enough that a
// script parking seats runs out immediately.
var (
    // WriteRule covers booking, paying, and cancelling.
    WriteRule = Bucket{Burst: 12, Interval: 5 * time.Second}

    // ReadRule covers the class list and everything else a screen loads.
    ReadRule = Bucket{Burst: 60, Interval: time.Second}
)

// Limiter holds one bucket per key and applies the rule to it.
//
// It exists as an interface for two reasons. The fast test tiers run against a
// fake, so a flood can be proven to stop at the limiter without anything being
// installed. And two api instances have to share one count, which is what the
// Redis implementation is for: a per-process limiter with two instances is a
// limit of twice what it says.
type Limiter interface {
    // Allow applies one request to the bucket behind a key.
    //
    // It both reads and writes, in one step. A caller that read a count and
    // then wrote it back would let two parallel requests both see the last
    // token, which is the same class of mistake the seat transaction exists to
    // avoid.
    Allow(ctx context.Context, key string, bucket Bucket, now time.Time) (Decision, error)
}
