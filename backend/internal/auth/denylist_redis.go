package auth

import (
    "context"
    "errors"
    "time"

    "github.com/redis/go-redis/v9"
)

// denylistKeyPrefix namespaces what this denylist writes.
//
// Redis is shared with the response cache and the rate limiter, so a prefix is
// what stops a withdrawn token and a cached class list colliding on a name
// somebody chose twice.
const denylistKeyPrefix = "auth:denied:"

// deniedMarker is what a key holds. The value is never read, only its existence,
// so it is one byte rather than anything that could carry a parent id.
const deniedMarker = "1"

// RedisDenylist is the shared implementation, and it is what makes a sign out
// real across more than one api instance.
//
// The per-process version answers only for the instance the logout happened to
// reach, which means a token withdrawn on one is still believed by the next. That
// gap is why this exists, and it is the whole difference between the two.
//
// Nothing here is stored beyond the token's own expiry. Redis is told the
// lifetime when the key is written, so an entry stops existing at the instant the
// signature stops verifying and the list cannot grow without bound.
type RedisDenylist struct {
    client redis.UniversalClient
}

// NewRedisDenylist wraps a client.
//
// Param:
// client - redis.UniversalClient (the connection, never nil)
//
// Return:
//   - the denylist
//   - ErrInvalidRequest when there is no client, refused here rather than as a
//     panic on the first sign out
func NewRedisDenylist(client redis.UniversalClient) (*RedisDenylist, error) {
    if client == nil {
        return nil, ErrInvalidRequest
    }

    return &RedisDenylist{client: client}, nil
}

// Deny withdraws one token id until the given instant.
//
// An instant that has already passed writes nothing. The token no longer
// verifies, so there is nothing left to withdraw, and writing a key with a
// negative lifetime is an error rather than a no-op.
func (denylist *RedisDenylist) Deny(ctx context.Context, tokenID string, until time.Time) error {
    if tokenID == "" || until.IsZero() {
        return ErrInvalidRequest
    }

    lifetime := time.Until(until)
    if lifetime <= 0 {
        return nil
    }

    if err := denylist.client.Set(ctx, denylistKeyPrefix+tokenID, deniedMarker, lifetime).Err(); err != nil {
        return ErrTokenInvalid
    }

    return nil
}

// IsDenied reports whether a token id was withdrawn and has not yet expired.
//
// A store that cannot be read is a failure rather than a "no". A denylist that
// cannot say no must not say yes: guessing here means honouring a token somebody
// has already signed out of, which is the one thing this list exists to prevent.
func (denylist *RedisDenylist) IsDenied(ctx context.Context, tokenID string, _ time.Time) (bool, error) {
    if tokenID == "" {
        return false, ErrInvalidRequest
    }

    err := denylist.client.Get(ctx, denylistKeyPrefix+tokenID).Err()

    switch {
    case errors.Is(err, redis.Nil):
        return false, nil

    case err != nil:
        return false, ErrTokenInvalid
    }

    return true, nil
}
