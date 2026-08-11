package auth

import (
    "context"
    "time"
)

// Denylist holds the access tokens that must stop being believed before they
// expire on their own.
//
// It is the price of a stateless access token. Verification touches no
// database, which is why the hot path is a signature check, and the cost is
// that a token cannot be withdrawn by deleting a row. A logout writes the jti
// here instead, for exactly as long as the token had left to run.
//
// Entries are never read after that instant, so nothing here grows without
// bound: a jti stops mattering at the moment the signature stops verifying.
type Denylist interface {
    // Deny withdraws one token id until the given instant, which is the
    // token's own expiry and never longer.
    Deny(ctx context.Context, tokenID string, until time.Time) error

    // IsDenied reports whether a token id was withdrawn and has not yet
    // expired.
    IsDenied(ctx context.Context, tokenID string, now time.Time) (bool, error)
}
