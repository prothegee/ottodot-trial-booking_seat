//go:build containers

// The real Redis half of the cache contract.
//
// It needs the backend stack running, which is why it sits behind the containers
// build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/cache/...
//
// Every case writes under a key of its own, so it can run against the same
// server a reviewer is clicking through without disturbing a live entry.
package cache_test

import (
    "context"
    "fmt"
    "os"
    "testing"
    "time"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/cache"
)

// redisAddress is where the proof tier connects.
func redisAddress() string {
    if address := os.Getenv("REDIS_ADDRESS"); address != "" {
        return address
    }

    return "127.0.0.1:6379"
}

// redisFixture points the shared contract at a real server, under a key nothing
// else uses.
type redisFixture struct {
    store *cache.RedisStore
    key   string
}

func (fixture redisFixture) Store() cache.Store {
    return fixture.store
}

func (fixture redisFixture) Key() string {
    return fixture.key
}

// newRedisFixture opens a client, claims a key of its own, and removes
// everything it wrote when the case ends.
func newRedisFixture(t *testing.T) redisFixture {
    t.Helper()

    client := redis.NewClient(&redis.Options{Addr: redisAddress()})

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := client.Ping(ctx).Err(); err != nil {
        t.Fatalf("cannot reach redis, run scripts/stack_up.sh backend first: %v", err)
    }

    key := fmt.Sprintf("cache:proof:%d", time.Now().UnixNano())

    t.Cleanup(func() {
        cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cleanupCancel()

        client.Del(cleanupCtx, key+":body", key+":version")
        _ = client.Close()
    })

    store, err := cache.NewRedisStore(client)
    if err != nil {
        t.Fatalf("cannot build the store: %v", err)
    }

    return redisFixture{store: store, key: key}
}

func TestTheRedisStoreHonoursTheContract(t *testing.T) {
    runStoreContract(t, func(t *testing.T) storeFixture {
        return newRedisFixture(t)
    })
}

func TestTheRedisStoreNeverLeavesABodyWithoutAnExpiry(t *testing.T) {
    t.Run("proof: a stored body carries a ttl, so a stale seat count cannot outlive the day", func(t *testing.T) {
        fixture := newRedisFixture(t)
        client := redis.NewClient(&redis.Options{Addr: redisAddress()})

        t.Cleanup(func() { _ = client.Close() })

        payload := []byte(`{"seats_remaining":2}`)

        if err := fixture.Store().Save(context.Background(), fixture.Key(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, 30*time.Second); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        lifetime, err := client.TTL(context.Background(), fixture.Key()+":body").Result()
        if err != nil {
            t.Fatalf("cannot read the ttl: %v", err)
        }

        if lifetime <= 0 {
            t.Fatalf("the body reports a ttl of %v, so it would be served forever", lifetime)
        }
    })

    t.Run("proof: the version counter has no expiry, or a tag would be reissued", func(t *testing.T) {
        fixture := newRedisFixture(t)
        client := redis.NewClient(&redis.Options{Addr: redisAddress()})

        t.Cleanup(func() { _ = client.Close() })

        if _, err := fixture.Store().Invalidate(context.Background(), fixture.Key()); err != nil {
            t.Fatalf("cannot invalidate: %v", err)
        }

        lifetime, err := client.TTL(context.Background(), fixture.Key()+":version").Result()
        if err != nil {
            t.Fatalf("cannot read the ttl: %v", err)
        }

        // Redis reports -1 for a key that exists with no expiry set.
        if lifetime != -1 {
            t.Fatalf("the version counter reports a ttl of %v, and a counter that resets reissues a tag", lifetime)
        }
    })
}

func TestTheRedisStoreSurvivesParallelInvalidation(t *testing.T) {
    t.Run("proof: every bump lands, so no invalidation is silently lost", func(t *testing.T) {
        fixture := newRedisFixture(t)

        const bumps = 12

        done := make(chan error, bumps)

        for range bumps {
            go func() {
                _, err := fixture.Store().Invalidate(context.Background(), fixture.Key())
                done <- err
            }()
        }

        for range bumps {
            if err := <-done; err != nil {
                t.Fatalf("an invalidation failed: %v", err)
            }
        }

        version, err := fixture.Store().Version(context.Background(), fixture.Key())
        if err != nil {
            t.Fatalf("cannot read the version: %v", err)
        }

        if version != bumps {
            t.Fatalf("%d parallel invalidations produced version %d", bumps, version)
        }
    })
}
