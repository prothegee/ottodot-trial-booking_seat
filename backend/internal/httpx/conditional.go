package httpx

import (
    "context"
    "encoding/json"
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/cache"
)

// BuildFunc produces the body for a cacheable read. It is called only on a miss,
// which is the whole point of the machinery around it.
type BuildFunc func(ctx context.Context) (any, error)

// Conditional serves the two cacheable reads.
//
// The goal it exists for is exact: when the data has not changed, this service
// does no work. A repeat class list costs one store read and zero database
// queries, and the simulation asserts that by counting calls on the reader.
//
// Nothing that decides anything passes through here. Only the class list and one
// class are cacheable, and both are advisory by construction, so a stale copy
// can cost a parent a wasted click and can never cost anybody a seat.
type Conditional struct {
    store    cache.Store
    lifetime time.Duration
    counters *Counters
}

// NewConditional wires the cache.
//
// Param:
// store - cache.Store (the shared one in production, the fake in tests)
// lifetime - time.Duration (how long a stored body is usable, zero for the default)
// counters - *Counters (where hits and misses are recorded, nil for nowhere)
//
// Return:
//   - the server
//   - cache.ErrInvalidRequest when there is no store, refused here rather than
//     as a nil dereference on the first request
func NewConditional(store cache.Store, lifetime time.Duration, counters *Counters) (*Conditional, error) {
    if store == nil {
        return nil, cache.ErrInvalidRequest
    }

    if lifetime <= 0 {
        lifetime = cache.DefaultLifetime
    }

    return &Conditional{store: store, lifetime: lifetime, counters: counters}, nil
}

// Serve answers one cacheable read.
//
// The path through it, in order:
//
//	the stored body is read. A store that cannot be reached is a miss, never a
//	failure, because caching is an optimisation and never a dependency
//
//	a stored tag the client already holds answers 304 with no body, and build is
//	never called
//
//	a stored body the client does not hold is written straight out, still without
//	calling build
//
//	only a real miss builds, and the tag it publishes carries the version
//	counter, so an invalidation changes the tag even when the bytes are identical
//
// Param:
// key - string (which document, from the cache package's key builders)
// policy - string (the Cache-Control value for a successful answer)
// build - BuildFunc (how to produce the body, called only on a miss)
func (conditional *Conditional) Serve(response http.ResponseWriter, request *http.Request, key string, policy string, build BuildFunc) {
    presented := request.Header.Get("If-None-Match")

    entry, stored, err := conditional.store.Entry(request.Context(), key)

    switch {
    case err != nil:
        conditional.count(func(counters *Counters) { counters.CacheError(key) })

    case stored && cache.ETagMatches(presented, entry.ETag):
        conditional.count(func(counters *Counters) {
            counters.CacheHit(key)
            counters.NotModified(key)
        })

        conditional.notModified(response, policy, entry.ETag)

        return

    case stored:
        conditional.count(func(counters *Counters) { counters.CacheHit(key) })

        writeBytes(response, http.StatusOK, policy, entry.ETag, entry.Payload)

        return

    default:
        conditional.count(func(counters *Counters) { counters.CacheMiss(key) })
    }

    conditional.rebuild(response, request, key, policy, presented, build)
}

// rebuild produces the body, publishes a tag for it, and stores both.
func (conditional *Conditional) rebuild(response http.ResponseWriter, request *http.Request, key string, policy string, presented string, build BuildFunc) {
    body, err := build(request.Context())
    if err != nil {
        Deny(response, request, err)

        return
    }

    payload, err := json.Marshal(body)
    if err != nil {
        Deny(response, request, err)

        return
    }

    // A version that cannot be read is treated as zero rather than as a
    // failure. The digest half of the tag still changes with the body, so the
    // worst case is a client holding a tag that a later invalidation cannot
    // move, and the stored body expires anyway.
    version, _ := conditional.store.Version(request.Context(), key)

    tag := cache.BuildETag(version, payload)

    // Storing is best effort for the same reason reading is: an unreachable
    // store costs the next caller a database read and nothing else.
    _ = conditional.store.Save(request.Context(), key, cache.Entry{ETag: tag, Payload: payload}, conditional.lifetime)

    if cache.ETagMatches(presented, tag) {
        // The body was rebuilt and turned out to be exactly what the client
        // already holds. Sending it again would be work nobody needs.
        conditional.count(func(counters *Counters) { counters.NotModified(key) })

        conditional.notModified(response, policy, tag)

        return
    }

    writeBytes(response, http.StatusOK, policy, tag, payload)
}

// notModified answers a client that already holds this representation.
//
// The tag and the cache policy go out with it. A 304 that dropped either would
// leave the client unable to revalidate again, which turns one saved body into
// every later request being a full one.
func (conditional *Conditional) notModified(response http.ResponseWriter, policy string, tag string) {
    response.Header().Set("Cache-Control", policy)
    response.Header().Set("ETag", tag)
    response.WriteHeader(http.StatusNotModified)
}

// Invalidate bumps a document's version and drops its stored body.
//
// Every mutation that can change a seat count calls it. A failure is ignored on
// purpose: the write already happened, the parent is owed an answer about it,
// and the stored body expires on its own within the lifetime.
func (conditional *Conditional) Invalidate(ctx context.Context, keys ...string) {
    for _, key := range keys {
        if key == "" {
            continue
        }

        _, _ = conditional.store.Invalidate(ctx, key)
    }
}

// count records one outcome, when there is anywhere to record it.
func (conditional *Conditional) count(record func(*Counters)) {
    if conditional.counters == nil {
        return
    }

    record(conditional.counters)
}
