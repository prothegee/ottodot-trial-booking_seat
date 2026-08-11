package cache

import (
    "context"
    "errors"
    "time"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/faults"
)

// The two suffixes one cached document uses.
//
// They are separate keys rather than one because they have different lifetimes.
// The body expires, so a stale seat count cannot outlive its usefulness. The
// version does not, because a counter that expires would restart at zero and
// hand out a tag a client is still holding.
const (
    bodySuffix    = ":body"
    versionSuffix = ":version"
)

// The two fields a stored body is kept under.
const (
    etagField    = "etag"
    payloadField = "payload"
)

// RedisStore is the shared implementation. It is what makes an invalidation on
// one api instance visible to the next request served by another.
//
// Nothing here is a source of truth. Every method can fail, and every caller
// treats a failure as a miss, so this store being unreachable costs a database
// read rather than a request.
type RedisStore struct {
    client redis.UniversalClient
    fault  Fault
}

// NewRedisStore wraps a client.
//
// Param:
// client - redis.UniversalClient (the connection, never nil)
//
// Return:
//   - the store
//   - ErrInvalidRequest when there is no client, refused here rather than as a
//     panic on the first request
func NewRedisStore(client redis.UniversalClient) (*RedisStore, error) {
    if client == nil {
        return nil, ErrInvalidRequest
    }

    return &RedisStore{client: client}, nil
}

// InjectFaults points this store at a fault source.
//
// Only the read path carries an injection point. What the fault is meant to
// prove is that a request still succeeds with Redis gone, and it is the read
// that would otherwise serve the answer.
func (store *RedisStore) InjectFaults(fault Fault) {
    store.fault = fault
}

// Entry reads a stored body and its tag.
//
// A partially written hash is treated as a miss rather than as a failure. It
// cannot happen through Save, which writes both fields in one command, but a
// cache is a shared namespace and answering "not here" is always safe.
func (store *RedisStore) Entry(ctx context.Context, key string) (Entry, bool, error) {
    if key == "" {
        return Entry{}, false, ErrInvalidRequest
    }

    // Injection point: Redis is down. The caller has to serve the request from
    // Postgres anyway, because caching is an optimisation and was never allowed
    // to become a dependency, and this is what proves that is still true.
    if store.fault.triggered(faults.PointCacheRedisError) {
        return Entry{}, false, ErrUnavailable
    }

    fields, err := store.client.HGetAll(ctx, key+bodySuffix).Result()
    if err != nil {
        return Entry{}, false, unreachable(err)
    }

    entry := Entry{ETag: fields[etagField], Payload: []byte(fields[payloadField])}
    if !entry.IsUsable() {
        return Entry{}, false, nil
    }

    return entry, true, nil
}

// Save stores a body and its tag for a bounded time.
//
// The two fields and the expiry go out in one pipeline, so a body can never be
// left without the tag it was published with, and can never be left without an
// expiry either. A stored seat count with no expiry is the one shape of this bug
// that nobody notices until a class looks full for a day.
func (store *RedisStore) Save(ctx context.Context, key string, entry Entry, lifetime time.Duration) error {
    if key == "" || !entry.IsUsable() || lifetime <= 0 {
        return ErrInvalidRequest
    }

    bodyKey := key + bodySuffix

    _, err := store.client.TxPipelined(ctx, func(pipeline redis.Pipeliner) error {
        pipeline.HSet(ctx, bodyKey, etagField, entry.ETag, payloadField, entry.Payload)
        pipeline.Expire(ctx, bodyKey, lifetime)

        return nil
    })
    if err != nil {
        return unreachable(err)
    }

    return nil
}

// Version reads the counter a key's tag is built from.
func (store *RedisStore) Version(ctx context.Context, key string) (uint64, error) {
    if key == "" {
        return 0, ErrInvalidRequest
    }

    version, err := store.client.Get(ctx, key+versionSuffix).Uint64()

    switch {
    case errors.Is(err, redis.Nil):
        // A key nobody has invalidated yet is version zero. Treating an absent
        // counter as a failure would make the very first request an error.
        return 0, nil

    case err != nil:
        return 0, unreachable(err)
    }

    return version, nil
}

// Invalidate bumps the version and drops the stored body.
//
// The order is the point, and it is why both commands go out together: the
// counter rises before the body disappears, so a reader arriving in the middle
// finds no body and builds its tag from the higher number. The other order would
// let that reader republish the old tag against fresh bytes.
func (store *RedisStore) Invalidate(ctx context.Context, key string) (uint64, error) {
    if key == "" {
        return 0, ErrInvalidRequest
    }

    var bumped *redis.IntCmd

    _, err := store.client.TxPipelined(ctx, func(pipeline redis.Pipeliner) error {
        bumped = pipeline.Incr(ctx, key+versionSuffix)
        pipeline.Del(ctx, key+bodySuffix)

        return nil
    })
    if err != nil {
        return 0, unreachable(err)
    }

    return uint64(bumped.Val()), nil
}

// unreachable turns a driver failure into this package's own.
//
// A redis error string can carry the address it was dialling, and an address
// carries a password. Nothing from the driver is wrapped into what a caller
// sees, only recorded as the store being unusable.
func unreachable(err error) error {
    if err == nil {
        return nil
    }

    return ErrUnavailable
}
