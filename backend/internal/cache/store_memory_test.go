package cache_test

import (
    "context"
    "sync"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/cache"
)

// memoryFixture points the shared contract at the in-process store.
type memoryFixture struct {
    store *cache.MemoryStore
}

func (fixture memoryFixture) Store() cache.Store {
    return fixture.store
}

func (fixture memoryFixture) Key() string {
    return cache.ClassListKey()
}

func TestTheMemoryStoreHonoursTheContract(t *testing.T) {
    runStoreContract(t, func(t *testing.T) storeFixture {
        return memoryFixture{store: cache.NewMemoryStore()}
    })
}

func TestTheMemoryStoreForgetsWhatHasExpired(t *testing.T) {
    moment := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

    t.Run("behaviour: a body is served until its lifetime is up and never after", func(t *testing.T) {
        now := moment
        store := cache.NewMemoryStore()
        store.SetClock(func() time.Time { return now })

        payload := []byte(`{"classes":[]}`)

        if err := store.Save(context.Background(), cache.ClassListKey(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, 30*time.Second); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        now = moment.Add(29 * time.Second)

        if _, found, _ := store.Entry(context.Background(), cache.ClassListKey()); !found {
            t.Fatal("a body one second inside its lifetime was already gone")
        }

        now = moment.Add(30 * time.Second)

        if _, found, _ := store.Entry(context.Background(), cache.ClassListKey()); found {
            t.Fatal("a body at exactly its expiry was still served")
        }
    })

    t.Run("edge: an expired body is dropped as it is read, not merely hidden", func(t *testing.T) {
        now := moment
        store := cache.NewMemoryStore()
        store.SetClock(func() time.Time { return now })

        payload := []byte("body")

        if err := store.Save(context.Background(), cache.ClassListKey(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, time.Second); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        now = moment.Add(time.Minute)

        if _, _, err := store.Entry(context.Background(), cache.ClassListKey()); err != nil {
            t.Fatalf("cannot read: %v", err)
        }

        if store.Size() != 0 {
            t.Fatalf("%d expired bodies are still held, so a long run grows without bound", store.Size())
        }
    })
}

func TestTheMemoryStoreKeepsTheCallersPayloadOutOfItsHands(t *testing.T) {
    t.Run("edge: rewriting the buffer after saving does not rewrite the cached body", func(t *testing.T) {
        store := cache.NewMemoryStore()
        payload := []byte(`{"seats_remaining":2}`)

        if err := store.Save(context.Background(), cache.ClassListKey(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, contractLifetime); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        for index := range payload {
            payload[index] = 'x'
        }

        entry, found, err := store.Entry(context.Background(), cache.ClassListKey())
        if err != nil || !found {
            t.Fatalf("cannot read back, found %v, err %v", found, err)
        }

        if string(entry.Payload) != `{"seats_remaining":2}` {
            t.Fatalf("the cached body followed the caller's buffer and now reads %s", entry.Payload)
        }
    })

    t.Run("edge: rewriting what was read does not rewrite what is held", func(t *testing.T) {
        store := cache.NewMemoryStore()
        payload := []byte(`{"seats_remaining":2}`)

        if err := store.Save(context.Background(), cache.ClassListKey(),
            cache.Entry{ETag: cache.BuildETag(0, payload), Payload: payload}, contractLifetime); err != nil {
            t.Fatalf("cannot save: %v", err)
        }

        first, _, _ := store.Entry(context.Background(), cache.ClassListKey())

        for index := range first.Payload {
            first.Payload[index] = 'x'
        }

        second, _, _ := store.Entry(context.Background(), cache.ClassListKey())

        if string(second.Payload) != `{"seats_remaining":2}` {
            t.Fatalf("a reader corrupted the cache and the next read got %s", second.Payload)
        }
    })
}

func TestTheMemoryStoreIsSafeToShare(t *testing.T) {
    t.Run("integration: parallel saves, reads, and invalidations leave a usable store", func(t *testing.T) {
        store := cache.NewMemoryStore()

        var waiting sync.WaitGroup

        for worker := 0; worker < 16; worker++ {
            waiting.Add(1)

            go func(worker int) {
                defer waiting.Done()

                payload := []byte(`{"worker":` + string(rune('0'+worker%10)) + `}`)

                _ = store.Save(context.Background(), cache.ClassListKey(),
                    cache.Entry{ETag: cache.BuildETag(uint64(worker), payload), Payload: payload}, contractLifetime)
                _, _, _ = store.Entry(context.Background(), cache.ClassListKey())
                _, _ = store.Invalidate(context.Background(), cache.ClassListKey())
            }(worker)
        }

        waiting.Wait()

        version, err := store.Version(context.Background(), cache.ClassListKey())
        if err != nil {
            t.Fatalf("cannot read the version after the race: %v", err)
        }

        if version != 16 {
            t.Fatalf("16 invalidations produced version %d, so a bump was lost", version)
        }
    })
}

func TestTheClassKeys(t *testing.T) {
    t.Run("unit: two classes never share a key", func(t *testing.T) {
        first := cache.ClassKey("11111111-1111-7111-8111-111111111111")
        second := cache.ClassKey("22222222-2222-7222-8222-222222222222")

        if first == second {
            t.Fatal("two classes collided on one key, so one would serve the other's seat count")
        }
    })

    t.Run("unit: the class list has a key of its own", func(t *testing.T) {
        if cache.ClassListKey() == cache.ClassKey("11111111-1111-7111-8111-111111111111") {
            t.Fatal("the list and a class share a key")
        }
    })

    t.Run("edge: an empty class id has no key, which reads as do not cache", func(t *testing.T) {
        if cache.ClassKey("   ") != "" {
            t.Fatal("a blank id produced a key, so every unnamed class would share one entry")
        }
    })
}
