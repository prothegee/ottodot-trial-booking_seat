package cache

import (
    "context"
    "time"
)

// DefaultLifetime is how long a stored body stays usable.
//
// Thirty seconds is chosen against the one thing that can go stale here, a seat
// count. It is short enough that a parent refreshing a page after a friend
// booked sees the change, and long enough that a burst of arrivals costs one
// database read rather than one each.
const DefaultLifetime = 30 * time.Second

// Entry is one cached response: the bytes, and the tag those bytes carry.
//
// They are stored together and never separately. A tag that can be read without
// its body is a tag that can be served for a body somebody else replaced.
type Entry struct {
    // ETag is the validator this payload was published with.
    ETag string

    // Payload is the exact response body, already encoded.
    Payload []byte
}

// IsUsable reports whether an entry can be served. An entry missing either half
// is treated as a miss rather than as an error, because half a cached response
// is not a response.
func (entry Entry) IsUsable() bool {
    return entry.ETag != "" && len(entry.Payload) > 0
}

// Store is where cached bodies and their version counters live.
//
// It exists as an interface for two reasons. The fast test tiers run against a
// fake, so nothing has to be installed to prove a conditional request skips the
// database. And Redis is an optimisation here, never a source of truth, so the
// service has to be able to run with an implementation that keeps nothing at
// all.
//
// Every method may fail, and every caller in this service treats a failure the
// same way: as a miss. That is the whole reason Redis being down degrades this
// service rather than stopping it.
type Store interface {
    // Entry reads a stored body and its tag.
    //
    // The boolean is the miss, not the error. A miss is the ordinary answer,
    // and reporting it as a failure would make every cold start look broken.
    Entry(ctx context.Context, key string) (Entry, bool, error)

    // Save stores a body and its tag for a bounded time.
    Save(ctx context.Context, key string, entry Entry, lifetime time.Duration) error

    // Version reads the counter a key's tag is built from. An unknown key is
    // version zero rather than an error, so a first request needs no setup.
    Version(ctx context.Context, key string) (uint64, error)

    // Invalidate bumps the version and drops the stored body.
    //
    // The order matters and is the implementation's job to get right: the
    // version rises first, so a reader that arrives between the two steps
    // builds a new tag rather than republishing the old one.
    Invalidate(ctx context.Context, key string) (uint64, error)
}
