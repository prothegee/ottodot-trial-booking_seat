package cache

import (
    "context"
    "sync"
    "time"
)

// held is one stored body with the instant it stops being usable.
type held struct {
    entry    Entry
    expireAt time.Time
}

// MemoryStore is the fake every fast test runs against, and the implementation
// the service falls back to when there is no Redis to talk to.
//
// It keeps everything in this process, which is exactly the limit worth being
// clear about: two api instances would each hold their own copy and their own
// version counters, so a mutation on one would not invalidate the other. That is
// tolerable only because everything cacheable here is advisory, and it is why
// the Redis implementation exists at all.
type MemoryStore struct {
    mutex    sync.Mutex
    entries  map[string]held
    versions map[string]uint64

    // clock is where expiry is judged from. A test sets it so an entry can be
    // aged past its lifetime without sleeping.
    clock func() time.Time
}

// NewMemoryStore builds an empty store on the real clock.
func NewMemoryStore() *MemoryStore {
    return &MemoryStore{
        entries:  make(map[string]held),
        versions: make(map[string]uint64),
        clock:    time.Now,
    }
}

// SetClock points the store at another clock. It is for tests, so an entry can
// be aged past its lifetime in one line instead of by waiting.
func (store *MemoryStore) SetClock(clock func() time.Time) {
    if clock == nil {
        return
    }

    store.mutex.Lock()
    defer store.mutex.Unlock()

    store.clock = clock
}

// Entry reads a stored body and its tag.
//
// An expired entry is dropped as it is read. Sweeping on read rather than on a
// timer keeps this type free of a goroutine, and the only thing a timer would
// buy is memory back sooner from keys nobody is asking for.
func (store *MemoryStore) Entry(_ context.Context, key string) (Entry, bool, error) {
    if key == "" {
        return Entry{}, false, ErrInvalidRequest
    }

    store.mutex.Lock()
    defer store.mutex.Unlock()

    stored, found := store.entries[key]
    if !found {
        return Entry{}, false, nil
    }

    if !store.clock().Before(stored.expireAt) {
        delete(store.entries, key)

        return Entry{}, false, nil
    }

    return copyEntry(stored.entry), true, nil
}

// Save stores a body and its tag for a bounded time.
func (store *MemoryStore) Save(_ context.Context, key string, entry Entry, lifetime time.Duration) error {
    if key == "" || !entry.IsUsable() || lifetime <= 0 {
        return ErrInvalidRequest
    }

    store.mutex.Lock()
    defer store.mutex.Unlock()

    store.entries[key] = held{
        entry:    copyEntry(entry),
        expireAt: store.clock().Add(lifetime),
    }

    return nil
}

// Version reads the counter a key's tag is built from.
func (store *MemoryStore) Version(_ context.Context, key string) (uint64, error) {
    if key == "" {
        return 0, ErrInvalidRequest
    }

    store.mutex.Lock()
    defer store.mutex.Unlock()

    return store.versions[key], nil
}

// Invalidate bumps the version and drops the stored body.
func (store *MemoryStore) Invalidate(_ context.Context, key string) (uint64, error) {
    if key == "" {
        return 0, ErrInvalidRequest
    }

    store.mutex.Lock()
    defer store.mutex.Unlock()

    store.versions[key]++
    delete(store.entries, key)

    return store.versions[key], nil
}

// Size reports how many bodies are held, expired ones included. It exists for
// the test that proves an entry is dropped rather than merely hidden.
func (store *MemoryStore) Size() int {
    store.mutex.Lock()
    defer store.mutex.Unlock()

    return len(store.entries)
}

// copyEntry takes the payload out of the caller's hands.
//
// The slice handed in is usually a buffer the caller is about to reuse, and a
// cache that hands back a body somebody else has since overwritten is worse than
// no cache at all.
func copyEntry(entry Entry) Entry {
    payload := make([]byte, len(entry.Payload))
    copy(payload, entry.Payload)

    return Entry{ETag: entry.ETag, Payload: payload}
}
