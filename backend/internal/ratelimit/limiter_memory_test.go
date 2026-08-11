package ratelimit_test

import (
    "context"
    "fmt"
    "sync"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/ratelimit"
)

// memoryFixture points the shared contract at the in-process limiter.
type memoryFixture struct {
    limiter *ratelimit.MemoryLimiter
}

func (fixture memoryFixture) Limiter() ratelimit.Limiter {
    return fixture.limiter
}

func (fixture memoryFixture) Key() string {
    return ratelimit.SubjectKey("11111111-1111-7111-8111-111111111111")
}

func TestTheMemoryLimiterHonoursTheContract(t *testing.T) {
    runLimiterContract(t, func(t *testing.T) limiterFixture {
        return memoryFixture{limiter: ratelimit.NewMemoryLimiter()}
    })
}

func TestTheMemoryLimiterIsSafeToShare(t *testing.T) {
    t.Run("integration: a parallel flood is allowed exactly the burst and no more", func(t *testing.T) {
        rule := ratelimit.Bucket{Burst: 5, Interval: time.Minute}
        moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
        key := ratelimit.SubjectKey("11111111-1111-7111-8111-111111111111")

        limiter := ratelimit.NewMemoryLimiter()

        const callers = 40

        var (
            waiting sync.WaitGroup
            counted sync.Mutex
            allowed int
        )

        for range callers {
            waiting.Add(1)

            go func() {
                defer waiting.Done()

                decision, err := limiter.Allow(context.Background(), key, rule, moment)
                if err != nil || !decision.Allowed {
                    return
                }

                counted.Lock()
                allowed++
                counted.Unlock()
            }()
        }

        waiting.Wait()

        if allowed != rule.Burst {
            t.Fatalf("%d of %d parallel requests were allowed, the burst is %d", allowed, callers, rule.Burst)
        }
    })
}

func TestTheMemoryLimiterForgetsIdleCallers(t *testing.T) {
    t.Run("behaviour: a caller whose bucket has refilled is dropped, so a flood cannot grow the map", func(t *testing.T) {
        rule := ratelimit.Bucket{Burst: 2, Interval: time.Second}
        moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

        limiter := ratelimit.NewMemoryLimiter()

        // Well past the sweep threshold, each from a different address, which is
        // the shape of a flood from a botnet rather than from one account.
        for caller := range 2000 {
            key := ratelimit.AddressKey(fmt.Sprintf("10.0.%d.%d", caller/256, caller%256))

            if _, err := limiter.Allow(context.Background(), key, rule, moment); err != nil {
                t.Fatalf("caller %d failed: %v", caller, err)
            }
        }

        held := limiter.Size()

        // One more request, long after every one of those buckets refilled.
        if _, err := limiter.Allow(context.Background(), ratelimit.AddressKey("10.9.9.9"), rule, moment.Add(time.Hour)); err != nil {
            t.Fatalf("the request after the rest failed: %v", err)
        }

        if limiter.Size() >= held {
            t.Fatalf("%d buckets were held and %d remain, so nothing is ever forgotten",
                held, limiter.Size())
        }
    })

    t.Run("edge: forgetting an idle caller changes no decision, because a full bucket is a new one", func(t *testing.T) {
        rule := ratelimit.Bucket{Burst: 2, Interval: time.Second}
        moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
        key := ratelimit.SubjectKey("11111111-1111-7111-8111-111111111111")

        limiter := ratelimit.NewMemoryLimiter()

        for range rule.Burst {
            if _, err := limiter.Allow(context.Background(), key, rule, moment); err != nil {
                t.Fatalf("spending the burst failed: %v", err)
            }
        }

        rested := moment.Add(time.Hour)

        for request := 1; request <= rule.Burst; request++ {
            decision, err := limiter.Allow(context.Background(), key, rule, rested)
            if err != nil {
                t.Fatalf("request %d after the rest failed: %v", request, err)
            }

            if !decision.Allowed {
                t.Fatalf("request %d after a full refill was refused", request)
            }
        }
    })
}

func TestTheLimiterKeys(t *testing.T) {
    t.Run("unit: a subject and an address never share a bucket", func(t *testing.T) {
        if ratelimit.SubjectKey("abc") == ratelimit.AddressKey("abc") {
            t.Fatal("a parent id and an address collided, so one would spend the other's allowance")
        }
    })

    t.Run("edge: an unknown subject has no key, which reads as nothing to count", func(t *testing.T) {
        if ratelimit.SubjectKey("  ") != "" {
            t.Fatal("a blank subject produced a key, so every anonymous caller would share one bucket")
        }
    })

    t.Run("edge: an unknown address has no key", func(t *testing.T) {
        if ratelimit.AddressKey("") != "" {
            t.Fatal("a blank address produced a key")
        }
    })
}
