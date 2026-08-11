// Package auth issues, verifies, and rotates the tokens this service runs on.
//
// The access token is a JWT and is stored nowhere. Verifying it costs a
// signature check and a clock comparison, which is the whole reason it was
// chosen over a session row: the hot path touches no database.
//
// The refresh token is opaque and has a row, and only as a sha256 hash. It
// rotates on every use, and presenting a spent one revokes the entire chain.
//
// What is deliberately not here: passwords. Sign in is by seeded email, which
// the brief asks for. The machinery around it is real, so a password or a
// provider later touches one method.
package auth

import "time"

// The roles this service knows. Anything else is a typo, and a typo in this
// value would quietly open an admin route.
const (
    RoleParent = "parent"
    RoleAdmin  = "admin"
)

// TypeAccess is the only token type carried as a JWT.
//
// The refresh token is opaque, so nothing signs a refresh JWT today. The claim
// is checked anyway, because the day a second type is signed is the day an
// unchecked typ lets one be spent as the other.
const TypeAccess = "access"

// IsKnownRole reports whether a role is one this service acts on.
func IsKnownRole(role string) bool {
    return role == RoleParent || role == RoleAdmin
}

// Claims is the entire payload of an access token, and the field list is the
// contract.
//
// A JWT payload is base64, not encryption. Anyone holding the token reads it,
// including whoever picked it out of a shared screen recording. So there is no
// email here, no name, no child, and no class: only what this service needs to
// decide whether a request may proceed.
//
// The struct is closed for the same reason. There is no map of extra claims to
// pass something through, because a pass-through is how an email ends up in a
// token six months from now.
type Claims struct {
    // Subject is the parent id this token speaks for.
    Subject string `json:"sub"`

    // Role is parent or admin. It is in the token rather than read per request
    // because a role read would put a database query back on the hot path.
    Role string `json:"role"`

    // Type is TypeAccess.
    Type string `json:"typ"`

    // TokenID is what a logout denylists. Without it a token could be verified
    // but never withdrawn.
    TokenID string `json:"jti"`

    // IssuedAt is unix seconds.
    IssuedAt int64 `json:"iat"`

    // ExpiresAt is unix seconds.
    ExpiresAt int64 `json:"exp"`
}

// Validate reports whether every claim this service requires is present and
// sane.
//
// Note:
//   - a missing jti is refused here rather than tolerated. A token nobody can
//     denylist verifies forever, so accepting one would quietly remove logout.
//   - expiry is not judged here. That needs a clock, and a caller that has one
//     asks IsExpired, which reports a different failure.
//
// Return:
//   - nil when the claim set is one this service issued
//   - ErrTokenInvalid otherwise, naming nothing about which part was wrong
func (claims Claims) Validate() error {
    if claims.Subject == "" || claims.TokenID == "" {
        return ErrTokenInvalid
    }

    if claims.Type != TypeAccess || !IsKnownRole(claims.Role) {
        return ErrTokenInvalid
    }

    if claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt {
        return ErrTokenInvalid
    }

    return nil
}

// IsExpired reports whether the token's life is over at the given instant.
//
// The boundary is inclusive: a token whose expiry is exactly now is spent. A
// second of leeway either way is a second in which a withdrawn token still
// works, and there is nothing to buy with it here because one service issues
// and verifies, on one clock.
func (claims Claims) IsExpired(now time.Time) bool {
    return !now.Before(time.Unix(claims.ExpiresAt, 0))
}

// Expiry is when this token stops being believed, which is how long a logout
// has to keep its jti on the denylist.
func (claims Claims) Expiry() time.Time {
    return time.Unix(claims.ExpiresAt, 0).UTC()
}
