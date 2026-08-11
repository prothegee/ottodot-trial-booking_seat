package ratelimit_test

import (
    "context"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/ratelimit"
)

// The contract every limiter has to satisfy.
//
// The risk this file exists to remove: the fake applies the bucket arithmetic
// correctly while the Lua script drifts from it, and every fast test stays
// green. The two hold the same rule in two languages, so one suite pointed at
// both is the only thing that keeps them agreeing. It runs twice, once against
// the memory limiter in the fast tiers and once against real Redis behind the
// containers tag.

// limiterFixture is one limiter, however it was built.
//
// It carries a key rather than letting the suite pick one, because the real
// implementation shares a namespace with everything else on that server, so two
// runs must not collide.
type limiterFixture interface {
    Limiter() ratelimit.Limiter
    Key() string
}

// runLimiterContract is the whole suite. Both implementations call it.
func runLimiterContract(t *testing.T, build func(t *testing.T) limiterFixture) {
    t.Helper()

    rule := ratelimit.Bucket{Burst: 3, Interval: 2 * time.Second}
    moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

    t.Run("integration: the burst is allowed and the next request is not", func(t *testing.T) {
        fixture := build(t)

        for request := 1; request <= rule.Burst; request++ {
            decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment)
            if err != nil {
                t.Fatalf("request %d failed: %v", request, err)
            }

            if !decision.Allowed {
                t.Fatalf("request %d of the burst was refused", request)
            }
        }

        decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment)
        if err != nil {
            t.Fatalf("the request past the burst failed: %v", err)
        }

        if decision.Allowed {
            t.Fatalf("request %d from a burst of %d was allowed", rule.Burst+1, rule.Burst)
        }
    })

    t.Run("unit: an allowed request reports what is left", func(t *testing.T) {
        fixture := build(t)

        decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment)
        if err != nil {
            t.Fatalf("the first request failed: %v", err)
        }

        if decision.Remaining != rule.Burst-1 {
            t.Fatalf("the first request left %d, wanted %d", decision.Remaining, rule.Burst-1)
        }
    })

    t.Run("behaviour: a refusal carries a wait a client can act on", func(t *testing.T) {
        fixture := build(t)

        for range rule.Burst {
            if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment); err != nil {
                t.Fatalf("spending the burst failed: %v", err)
            }
        }

        decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment)
        if err != nil {
            t.Fatalf("the refused request failed: %v", err)
        }

        if decision.RetryAfter < time.Second {
            t.Fatalf("the refusal reported a wait of %v, and a client told to wait zero retries immediately",
                decision.RetryAfter)
        }
    })

    t.Run("behaviour: waiting one interval earns exactly one request back", func(t *testing.T) {
        fixture := build(t)

        for range rule.Burst {
            if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment); err != nil {
                t.Fatalf("spending the burst failed: %v", err)
            }
        }

        later := moment.Add(rule.Interval)

        first, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, later)
        if err != nil {
            t.Fatalf("the request after one interval failed: %v", err)
        }

        if !first.Allowed {
            t.Fatal("one interval did not return a token")
        }

        second, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, later)
        if err != nil {
            t.Fatalf("the second request after one interval failed: %v", err)
        }

        if second.Allowed {
            t.Fatal("one interval returned more than one token")
        }
    })

    t.Run("behaviour: two keys are counted apart", func(t *testing.T) {
        fixture := build(t)
        other := fixture.Key() + ":other"

        for range rule.Burst {
            if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment); err != nil {
                t.Fatalf("spending the burst failed: %v", err)
            }
        }

        decision, err := fixture.Limiter().Allow(context.Background(), other, rule, moment)
        if err != nil {
            t.Fatalf("the second key failed: %v", err)
        }

        if !decision.Allowed {
            t.Fatal("one caller's flood refused another caller, which is a denial of service anyone can trigger")
        }
    })

    t.Run("edge: a clock that went backwards takes no allowance away", func(t *testing.T) {
        fixture := build(t)

        if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment); err != nil {
            t.Fatalf("the first request failed: %v", err)
        }

        decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment.Add(-time.Hour))
        if err != nil {
            t.Fatalf("the request from a lagging clock failed: %v", err)
        }

        if !decision.Allowed {
            t.Fatal("a caller lost its allowance because one instance was behind the other")
        }
    })

    t.Run("edge: an empty key is refused rather than counted under a blank name", func(t *testing.T) {
        fixture := build(t)

        if _, err := fixture.Limiter().Allow(context.Background(), "", rule, moment); err == nil {
            t.Fatal("an empty key was accepted, so every unnamed caller would share one bucket")
        }
    })

    t.Run("edge: a rule that cannot be applied is refused", func(t *testing.T) {
        fixture := build(t)

        broken := ratelimit.Bucket{Burst: 0, Interval: time.Second}

        if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), broken, moment); err == nil {
            t.Fatal("a bucket of zero was accepted, and it would refuse every request forever")
        }
    })
}
