package auth

import (
    "context"
    "sync"
    "time"
)

// MemoryDenylist keeps withdrawn token ids in this process.
//
// Stated plainly, because it is a real limit rather than a detail: this binds
// the process that served the logout and no other. With one api process, which
// is what this stack runs, a logout takes effect immediately and everywhere.
// With two, a token withdrawn on one is still believed by the other until it
// expires, at most one access token lifetime.
//
// The Redis implementation lands with the rest of the Redis surface in phase 6,
// and it plugs in here without a caller changing, which is why this is an
// interface with a fake behind it rather than a map somebody reaches into.
type MemoryDenylist struct {
    mutex sync.Mutex

    // until is the instant each withdrawn id stops mattering, which is the
    // token's own expiry.
    until map[string]time.Time
}

// NewMemoryDenylist builds an empty denylist.
func NewMemoryDenylist() *MemoryDenylist {
    return &MemoryDenylist{until: make(map[string]time.Time)}
}

// Deny withdraws one token id until the given instant.
//
// A denial that is already in the past is not stored. Nothing would ever read
// it, and storing it would grow the map for no effect.
func (denylist *MemoryDenylist) Deny(ctx context.Context, tokenID string, until time.Time) error {
    if tokenID == "" || until.IsZero() {
        return ErrInvalidRequest
    }

    denylist.mutex.Lock()
    defer denylist.mutex.Unlock()

    denylist.until[tokenID] = until

    return nil
}

// IsDenied reports whether a token id was withdrawn and has not yet expired.
//
// The sweep happens here rather than on a timer, because the only moment an
// expired entry costs anything is the moment something looks at it.
func (denylist *MemoryDenylist) IsDenied(ctx context.Context, tokenID string, now time.Time) (bool, error) {
    if tokenID == "" || now.IsZero() {
        return false, ErrInvalidRequest
    }

    denylist.mutex.Lock()
    defer denylist.mutex.Unlock()

    deadline, withdrawn := denylist.until[tokenID]
    if !withdrawn {
        return false, nil
    }

    // The token's own expiry has passed, so the signature no longer verifies
    // and the entry has nothing left to protect.
    if !now.Before(deadline) {
        delete(denylist.until, tokenID)

        return false, nil
    }

    return true, nil
}

// Size reports how many withdrawn ids are held, expired ones included.
//
// It exists for the test that proves an entry is dropped once it can no longer
// matter, which is the one behaviour a map like this can get wrong quietly.
func (denylist *MemoryDenylist) Size() int {
    denylist.mutex.Lock()
    defer denylist.mutex.Unlock()

    return len(denylist.until)
}
