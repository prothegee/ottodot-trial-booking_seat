//go:build containers

// The real Redis half of the limiter contract, plus the proof no fake can give.
//
// It needs the backend stack running, which is why it sits behind the containers
// build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/ratelimit/...
//
// Every case counts under a key of its own, so it can run against the same
// server a reviewer is clicking through without spending a live allowance.
package ratelimit_test

import (
    "context"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/ratelimit"
)

// redisAddress is where the proof tier connects.
func redisAddress() string {
    if address := os.Getenv("REDIS_ADDRESS"); address != "" {
        return address
    }

    return "127.0.0.1:6379"
}

// redisFixture points the shared contract at a real server.
type redisFixture struct {
    limiter *ratelimit.RedisLimiter
    key     string
}

func (fixture redisFixture) Limiter() ratelimit.Limiter {
    return fixture.limiter
}

func (fixture redisFixture) Key() string {
    return fixture.key
}

// newRedisFixture opens a client, claims a key of its own, and removes what it
// wrote when the case ends.
func newRedisFixture(t *testing.T) redisFixture {
    t.Helper()

    client := redis.NewClient(&redis.Options{Addr: redisAddress()})

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        t.Fatalf("cannot reach redis, run scripts/stack_up.sh backend first: %v", err)
    }

    key := fmt.Sprintf("ratelimit:proof:%d", time.Now().UnixNano())

    t.Cleanup(func() {
        cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cleanupCancel()

        client.Del(cleanupCtx, key, key+":other")
        _ = client.Close()
    })

    limiter, err := ratelimit.NewRedisLimiter(client)
    if err != nil {
        t.Fatalf("cannot build the limiter: %v", err)
    }

    return redisFixture{limiter: limiter, key: key}
}

func TestTheRedisLimiterHonoursTheContract(t *testing.T) {
    runLimiterContract(t, func(t *testing.T) limiterFixture {
        return newRedisFixture(t)
    })
}

func TestTheRedisLimiterCountsOneBurstAcrossParallelCallers(t *testing.T) {
    t.Run("proof: a flood arriving together spends the burst exactly once", func(t *testing.T) {
        fixture := newRedisFixture(t)

        rule := ratelimit.Bucket{Burst: 5, Interval: time.Minute}
        moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

        const callers = 40

        answers := make(chan bool, callers)

        for range callers {
            go func() {
                decision, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, moment)
                answers <- err == nil && decision.Allowed
            }()
        }

        allowed := 0

        for range callers {
            if <-answers {
                allowed++
            }
        }

        // This is the property a fake cannot demonstrate. Two requests reading
        // the same count and both writing it back would let more than the burst
        // through, and only a real server running the script to completion stops
        // that.
        if allowed != rule.Burst {
            t.Fatalf("%d of %d parallel requests were allowed, the burst is %d", allowed, callers, rule.Burst)
        }
    })
}

func TestTheRedisLimiterNeverLeavesABucketBehind(t *testing.T) {
    t.Run("proof: a bucket carries a ttl, so an address seen once is not held forever", func(t *testing.T) {
        fixture := newRedisFixture(t)

        rule := ratelimit.Bucket{Burst: 3, Interval: 2 * time.Second}

        if _, err := fixture.Limiter().Allow(context.Background(), fixture.Key(), rule, time.Now()); err != nil {
            t.Fatalf("the request failed: %v", err)
        }

        client := redis.NewClient(&redis.Options{Addr: redisAddress()})

        t.Cleanup(func() { _ = client.Close() })

        lifetime, err := client.TTL(context.Background(), fixture.Key()).Result()
        if err != nil {
            t.Fatalf("cannot read the ttl: %v", err)
        }

        if lifetime <= 0 {
            t.Fatalf("the bucket reports a ttl of %v, so every caller ever seen is kept", lifetime)
        }
    })
}
