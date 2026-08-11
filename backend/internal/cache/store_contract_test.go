package cache_test

import (
    "context"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/cache"
)

// The contract every cache store has to satisfy.
//
// The risk this file exists to remove: the fake stores and invalidates exactly
// as intended while the Redis implementation drops a field, leaves a body
// without an expiry, or bumps a counter in the wrong order, and every fast test
// stays green. One suite pointed at both is what catches that, so this file runs
// twice, once against the memory store in the fast tiers and once against real
// Redis behind the containers tag.

// contractLifetime is what every stored body in this suite is written with,
// unless the case is about expiry.
const contractLifetime = 30 * time.Second

// storeFixture is one store, however it was built.
//
// It carries a key rather than letting the suite pick one, because the real
// implementation shares a namespace with the rate limiter and with anything else
// on that server, so two runs of this suite must not collide.
type storeFixture interface {
    Store() cache.Store
    Key() string
}

// runStoreContract is the whole suite. Both implementations call it.
func runStoreContract(t *testing.T, build func(t *testing.T) storeFixture) {
    t.Helper()

    t.Run("integration: a body that was saved reads back with its tag", func(t *testing.T) {
        fixture := build(t)
        key := fixture.Key()

        payload := []byte(`{"classes":[]}`)
        tag := cache.BuildETag(1, payload)

        if err := fixture.Store().Save(context.Background(), key, cache.Entry{ETag: tag, Payload: payload}, contractLifetime); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        entry, found, err := fixture.Store().Entry(context.Background(), key)
        if err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if !found {
            t.Fatal("what was just saved reads as a miss")
        }

        if entry.ETag != tag {
            t.Fatalf("the tag read back as %s, wanted %s", entry.ETag, tag)
        }

        if string(entry.Payload) != string(payload) {
            t.Fatalf("the body read back as %s, wanted %s", entry.Payload, payload)
        }
    })

    t.Run("unit: a key nobody has written is a miss and not a failure", func(t *testing.T) {
        fixture := build(t)

        _, found, err := fixture.Store().Entry(context.Background(), fixture.Key())
        if err != nil {
            t.Fatalf("a cold key reported a failure: %v", err)
        }

        if found {
            t.Fatal("a key nobody wrote reported a hit")
        }
    })

    t.Run("unit: a key nobody has invalidated is version zero", func(t *testing.T) {
        fixture := build(t)

        version, err := fixture.Store().Version(context.Background(), fixture.Key())
        if err != nil {
            t.Fatalf("a cold version reported a failure: %v", err)
        }

        if version != 0 {
            t.Fatalf("a cold key is version %d, wanted 0", version)
        }
    })

    t.Run("behaviour: invalidating raises the version and drops the body", func(t *testing.T) {
        fixture := build(t)
        key := fixture.Key()

        payload := []byte(`{"seats_remaining":2}`)

        if err := fixture.Store().Save(context.Background(), key,
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, contractLifetime); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        bumped, err := fixture.Store().Invalidate(context.Background(), key)
        if err != nil {
            t.Fatalf("cannot invalidate: %v", err)
        }

        if bumped != 1 {
            t.Fatalf("the first invalidation reported version %d, wanted 1", bumped)
        }

        _, found, err := fixture.Store().Entry(context.Background(), key)
        if err != nil {
            t.Fatalf("cannot read after invalidating: %v", err)
        }

        if found {
            t.Fatal("the body survived its invalidation, so a stale seat count would be served")
        }
    })

    t.Run("behaviour: the version keeps rising, so a tag is never reissued", func(t *testing.T) {
        fixture := build(t)
        key := fixture.Key()

        var previous uint64

        for round := 1; round <= 3; round++ {
            bumped, err := fixture.Store().Invalidate(context.Background(), key)
            if err != nil {
                t.Fatalf("cannot invalidate: %v", err)
            }

            if bumped <= previous {
                t.Fatalf("round %d reported version %d after %d, a counter that does not rise reissues a tag",
                    round, bumped, previous)
            }

            previous = bumped
        }
    })

    t.Run("behaviour: a body saved after an invalidation survives the next read", func(t *testing.T) {
        fixture := build(t)
        key := fixture.Key()

        if _, err := fixture.Store().Invalidate(context.Background(), key); err != nil {
            t.Fatalf("cannot invalidate: %v", err)
        }

        version, err := fixture.Store().Version(context.Background(), key)
        if err != nil {
            t.Fatalf("cannot read the version: %v", err)
        }

        payload := []byte(`{"seats_remaining":1}`)

        if err := fixture.Store().Save(context.Background(), key,
            cache.Entry{ETag: cache.BuildETag(version, payload), Payload: payload}, contractLifetime); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        entry, found, err := fixture.Store().Entry(context.Background(), key)
        if err != nil || !found {
            t.Fatalf("the refreshed body did not read back, found %v, err %v", found, err)
        }

        if entry.ETag != cache.BuildETag(version, payload) {
            t.Fatalf("the refreshed body carries %s, which is not the tag it was published with", entry.ETag)
        }
    })

    t.Run("edge: an empty key is refused rather than written under a blank name", func(t *testing.T) {
        fixture := build(t)

        if _, _, err := fixture.Store().Entry(context.Background(), ""); err == nil {
            t.Fatal("reading an empty key was allowed")
        }

        if err := fixture.Store().Save(context.Background(), "",
            cache.Entry{ETag: `"1-aa"`, Payload: []byte("body")}, contractLifetime); err == nil {
            t.Fatal("saving under an empty key was allowed")
        }

        if _, err := fixture.Store().Version(context.Background(), ""); err == nil {
            t.Fatal("reading the version of an empty key was allowed")
        }

        if _, err := fixture.Store().Invalidate(context.Background(), ""); err == nil {
            t.Fatal("invalidating an empty key was allowed")
        }
    })

    t.Run("edge: a body with no tag is refused, because half a response is not one", func(t *testing.T) {
        fixture := build(t)

        if err := fixture.Store().Save(context.Background(), fixture.Key(),
            cache.Entry{Payload: []byte("body")}, contractLifetime); err == nil {
            t.Fatal("a body with no tag was stored, and nothing could ever revalidate it")
        }
    })

    t.Run("edge: a lifetime of zero is refused, so nothing is stored forever", func(t *testing.T) {
        fixture := build(t)

        payload := []byte("body")

        if err := fixture.Store().Save(context.Background(), fixture.Key(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, 0); err == nil {
            t.Fatal("a body was stored with no expiry")
        }
    })
}
