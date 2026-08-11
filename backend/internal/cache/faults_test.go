package cache_test

import (
    "context"
    "errors"
    "testing"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/faults"
)

// unreachableClient points at a port nothing is listening on.
//
// No case here ever reaches it. That is the assertion: the injection point sits
// in front of every command, so a case can prove the fault fires without a Redis
// running, and a case that got past it would hang rather than pass quietly.
func unreachableClient() redis.UniversalClient {
    return redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
}

func TestRedisStoreUnderInjectedFaults(t *testing.T) {
    t.Run("integration: an armed read reports the store as unusable", func(t *testing.T) {
        store, err := cache.NewRedisStore(unreachableClient())
        if err != nil {
            t.Fatalf("cannot build the store: %v", err)
        }

        store.InjectFaults(func(point string) bool { return point == faults.PointCacheRedisError })

        _, stored, err := store.Entry(context.Background(), "classes")

        if !errors.Is(err, cache.ErrUnavailable) {
            t.Fatalf("the armed read answered %v", err)
        }

        if stored {
            t.Fatal("the armed read reported a stored entry")
        }
    })

    t.Run("edge: an empty key is still refused before the fault is consulted", func(t *testing.T) {
        // Argument validation comes first, because a caller passing nothing is a
        // mistake in this service rather than a condition worth simulating.
        store, err := cache.NewRedisStore(unreachableClient())
        if err != nil {
            t.Fatalf("cannot build the store: %v", err)
        }

        store.InjectFaults(func(_ string) bool { return true })

        if _, _, err := store.Entry(context.Background(), ""); !errors.Is(err, cache.ErrInvalidRequest) {
            t.Fatalf("an empty key answered %v", err)
        }
    })

    t.Run("unit: an unrelated armed point does not reach this store", func(t *testing.T) {
        store, err := cache.NewRedisStore(unreachableClient())
        if err != nil {
            t.Fatalf("cannot build the store: %v", err)
        }

        reached := ""

        store.InjectFaults(func(point string) bool {
            reached = point

            return false
        })

        _, _, _ = store.Entry(context.Background(), "classes")

        if reached != faults.PointCacheRedisError {
            t.Fatalf("the store asked about %q rather than its own point", reached)
        }
    })
}
