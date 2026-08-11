package auth

import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
    "fmt"
    "io"
)

// refreshTokenBytes is how much randomness one refresh token carries. 256 bits
// is not guessable, and the token is opaque, so there is nothing to gain from
// making it shorter.
const refreshTokenBytes = 32

// NewRefreshToken mints one opaque refresh token.
//
// Note:
//   - it is random, not a JWT. There is nothing to read inside it and nothing
//     to verify offline: the only way to judge one is to look it up, which is
//     exactly what makes rotation and revocation possible.
//
// Return:
//   - the token in base64url text, which is what travels in the cookie
//   - an error when randomness is unavailable, which is never ignored, because
//     a token built from a failed read would not be unguessable
func NewRefreshToken() (string, error) {
    return newRefreshTokenFrom(rand.Reader)
}

// newRefreshTokenFrom is the whole implementation with its source handed in,
// which is what lets a test fail the randomness on purpose.
//
// Param:
// entropy - io.Reader (where the bytes come from)
//
// Return:
//   - the token in base64url text
//   - an error when the read fails or comes up short
func newRefreshTokenFrom(entropy io.Reader) (string, error) {
    raw := make([]byte, refreshTokenBytes)

    if _, err := io.ReadFull(entropy, raw); err != nil {
        return "", fmt.Errorf("cannot read randomness for a refresh token: %w", err)
    }

    return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashRefreshToken reduces a token to what is stored.
//
// The row holds this and never the token. A copy of the database is then a copy
// of hashes, and a hash cannot be presented: the lookup hashes what arrived and
// compares, so a reader of the table holds nothing they can sign in with.
//
// sha256 with no salt and no work factor is correct here and would be wrong for
// a password. The input is 256 bits of randomness this service minted, so there
// is no dictionary to run and nothing to slow down.
//
// Param:
// token - string (the token exactly as it arrived)
//
// Return:
//   - the 32 byte digest, which is what the bytea column holds
func HashRefreshToken(token string) []byte {
    digest := sha256.Sum256([]byte(token))

    return digest[:]
}
