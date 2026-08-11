package ratelimit_test

import (
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/ratelimit"
)

// bucketMoment is the instant every case starts from.
var bucketMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// smallBucket is deliberately tiny, so a case can empty it in three lines and
// the arithmetic stays readable.
var smallBucket = ratelimit.Bucket{Burst: 3, Interval: 2 * time.Second}

func TestTakingFromABucket(t *testing.T) {
    t.Run("unit: a caller never seen before starts with a full bucket", func(t *testing.T) {
        _, decision := smallBucket.Take(ratelimit.State{}, bucketMoment)

        if !decision.Allowed {
            t.Fatal("a first request was refused")
        }

        if decision.Remaining != 2 {
            t.Fatalf("a first request left %d tokens, wanted 2", decision.Remaining)
        }
    })

    t.Run("unit: the burst is spent one request at a time", func(t *testing.T) {
        state := ratelimit.State{}

        for request := 1; request <= smallBucket.Burst; request++ {
            var decision ratelimit.Decision

            state, decision = smallBucket.Take(state, bucketMoment)

            if !decision.Allowed {
                t.Fatalf("request %d of the burst was refused", request)
            }
        }

        _, decision := smallBucket.Take(state, bucketMoment)

        if decision.Allowed {
            t.Fatal("a fourth request from a bucket of three was allowed")
        }
    })

    t.Run("behaviour: the allowance comes back one token per interval", func(t *testing.T) {
        state := ratelimit.State{}

        for range smallBucket.Burst {
            state, _ = smallBucket.Take(state, bucketMoment)
        }

        _, refused := smallBucket.Take(state, bucketMoment.Add(time.Second))
        if refused.Allowed {
            t.Fatal("half an interval was enough to earn a token back")
        }

        _, allowed := smallBucket.Take(state, bucketMoment.Add(2*time.Second))
        if !allowed.Allowed {
            t.Fatal("a full interval did not return a token")
        }
    })

    t.Run("behaviour: an idle caller never accumulates more than the burst", func(t *testing.T) {
        state := ratelimit.State{}
        state, _ = smallBucket.Take(state, bucketMoment)

        // An hour of silence, against a bucket that refills in six seconds.
        rested := bucketMoment.Add(time.Hour)

        for request := 1; request <= smallBucket.Burst; request++ {
            var decision ratelimit.Decision

            state, decision = smallBucket.Take(state, rested)

            if !decision.Allowed {
                t.Fatalf("request %d after a long rest was refused", request)
            }
        }

        _, decision := smallBucket.Take(state, rested)

        if decision.Allowed {
            t.Fatal("an hour of silence banked more than the burst, so a flood would be allowed through")
        }
    })

    t.Run("edge: a refusal reports a wait of at least one second", func(t *testing.T) {
        state := ratelimit.State{}

        for range smallBucket.Burst {
            state, _ = smallBucket.Take(state, bucketMoment)
        }

        _, decision := smallBucket.Take(state, bucketMoment)

        if decision.RetryAfter < time.Second {
            t.Fatalf("a refusal reported a wait of %v, and a client told to wait zero retries immediately",
                decision.RetryAfter)
        }
    })

    t.Run("edge: an allowed request reports no wait", func(t *testing.T) {
        _, decision := smallBucket.Take(ratelimit.State{}, bucketMoment)

        if decision.RetryAfter != 0 {
            t.Fatalf("an allowed request reported a wait of %v", decision.RetryAfter)
        }
    })

    t.Run("edge: a clock that went backwards takes no allowance away", func(t *testing.T) {
        state := ratelimit.State{}
        state, _ = smallBucket.Take(state, bucketMoment)

        _, decision := smallBucket.Take(state, bucketMoment.Add(-time.Hour))

        if !decision.Allowed {
            t.Fatal("a caller lost its allowance because one instance was behind the other")
        }
    })

    t.Run("edge: a refused request keeps the refill it earned", func(t *testing.T) {
        state := ratelimit.State{}

        for range smallBucket.Burst {
            state, _ = smallBucket.Take(state, bucketMoment)
        }

        // Refused at one second in, which is half a token earned.
        state, _ = smallBucket.Take(state, bucketMoment.Add(time.Second))

        _, decision := smallBucket.Take(state, bucketMoment.Add(2*time.Second))

        if !decision.Allowed {
            t.Fatal("the refill earned during a refusal was thrown away, so a refused caller can never recover")
        }
    })

    t.Run("edge: a bucket that cannot hold a token refuses everything", func(t *testing.T) {
        broken := ratelimit.Bucket{Burst: 0, Interval: time.Second}

        if _, decision := broken.Take(ratelimit.State{}, bucketMoment); decision.Allowed {
            t.Fatal("a bucket of zero allowed a request")
        }
    })

    t.Run("edge: a bucket that never refills refuses everything", func(t *testing.T) {
        broken := ratelimit.Bucket{Burst: 5}

        if _, decision := broken.Take(ratelimit.State{}, bucketMoment); decision.Allowed {
            t.Fatal("a bucket with no interval allowed a request, and it could never have refilled")
        }
    })
}

func TestTheRulesTheApiApplies(t *testing.T) {
    t.Run("unit: both rules are usable, or every route would refuse everything", func(t *testing.T) {
        for name, rule := range map[string]ratelimit.Bucket{
            "write": ratelimit.WriteRule,
            "read":  ratelimit.ReadRule,
        } {
            if !rule.IsUsable() {
                t.Fatalf("the %s rule cannot be applied: %+v", name, rule)
            }
        }
    })

    t.Run("unit: writes are tighter than reads, because a write costs a transaction", func(t *testing.T) {
        if ratelimit.WriteRule.Burst >= ratelimit.ReadRule.Burst {
            t.Fatalf("the write burst is %d and the read burst is %d, which is the wrong way round",
                ratelimit.WriteRule.Burst, ratelimit.ReadRule.Burst)
        }
    })
}
