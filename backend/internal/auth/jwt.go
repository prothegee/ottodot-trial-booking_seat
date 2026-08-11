package auth

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "strings"
    "time"
)

// AlgorithmHS256 is the only algorithm this service signs or accepts.
//
// One service signs and one service verifies, so an asymmetric key buys nothing
// here beyond key handling. ADR-027 records that RS256 is the move the day a
// second service needs to verify without holding the signing key.
const AlgorithmHS256 = "HS256"

// typeJWT is the header's own type field, which is not the typ claim. The
// header describes the envelope, the claim describes what is inside it.
const typeJWT = "JWT"

// segmentCount is header, payload, signature. Anything else is not a token this
// service produced.
const segmentCount = 3

// minimumSecretBytes is the shortest signing key this package accepts. Shorter
// than a sha256 block is brute forceable offline, and the whole security of
// HS256 is that the key is not guessable.
const minimumSecretBytes = 32

// header is the envelope. It is fixed, so it is written rather than configured.
type header struct {
    Algorithm string `json:"alg"`
    Type      string `json:"typ"`
}

// Signer issues and verifies access tokens.
//
// It holds the secret and nothing else. No clock, no store: the two things it
// does are pure given the key, which is what lets a test pin every instant it
// cares about.
type Signer struct {
    secret []byte
}

// NewSigner takes the signing key.
//
// Note:
//   - a short key is refused here rather than at startup as well, because a
//     test or a future entrypoint that builds a signer directly must not be
//     able to build a weak one.
//
// Param:
// secret - string (the signing key, from configuration, never a literal)
//
// Return:
//   - the signer
//   - ErrInvalidRequest when the key is too short to be worth signing with
func NewSigner(secret string) (*Signer, error) {
    if len(secret) < minimumSecretBytes {
        return nil, ErrInvalidRequest
    }

    return &Signer{secret: []byte(secret)}, nil
}

// Sign writes one access token.
//
// Note:
//   - the claims are validated first. Signing a claim set this service would
//     refuse to verify produces a token that fails on its first use, at the
//     other end of the system from the bug.
//
// Param:
// claims - Claims (the closed claim set, already filled by the caller)
//
// Return:
//   - the token, three base64url segments joined by dots
//   - ErrTokenInvalid when the claim set is not one this service issues
func (signer *Signer) Sign(claims Claims) (string, error) {
    if err := claims.Validate(); err != nil {
        return "", err
    }

    encodedHeader, err := encodeSegment(header{Algorithm: AlgorithmHS256, Type: typeJWT})
    if err != nil {
        return "", err
    }

    encodedPayload, err := encodeSegment(claims)
    if err != nil {
        return "", err
    }

    signingInput := encodedHeader + "." + encodedPayload

    return signingInput + "." + signer.signature(signingInput), nil
}

// Verify checks one access token and returns what it says.
//
// The order is the whole point of this method:
//
//	shape, then algorithm, then signature, then claims, then expiry
//
// Nothing inside the payload is believed before the signature has been checked,
// because until then the payload is whatever the caller typed. And expiry is
// judged last, so a tampered token that also happens to be expired reports
// ErrTokenInvalid rather than telling its holder that fixing the clock is the
// remaining problem.
//
// Param:
// token - string (the raw token, exactly as it arrived)
// now - time.Time (the instant expiry is judged at)
//
// Return:
//   - the claims, when the token is one this service issued and still live
//   - ErrTokenInvalid for a bad shape, a wrong algorithm, a failed signature,
//     or a claim set this service would not have issued
//   - ErrTokenExpired when the token verified and its life is over
func (signer *Signer) Verify(token string, now time.Time) (Claims, error) {
    segments := strings.Split(token, ".")
    if len(segments) != segmentCount {
        return Claims{}, ErrTokenInvalid
    }

    var envelope header

    if err := decodeSegment(segments[0], &envelope); err != nil {
        return Claims{}, ErrTokenInvalid
    }

    // The algorithm is read from the token and then refused unless it is the
    // one this service signs with. This is the check that stops the two classic
    // forgeries: alg "none", where the signature segment is empty and a
    // trusting verifier believes the payload, and a token signed with a public
    // key the service already publishes.
    if envelope.Algorithm != AlgorithmHS256 || envelope.Type != typeJWT {
        return Claims{}, ErrTokenInvalid
    }

    if !signer.matches(segments[0]+"."+segments[1], segments[2]) {
        return Claims{}, ErrTokenInvalid
    }

    var claims Claims

    if err := decodeSegment(segments[1], &claims); err != nil {
        return Claims{}, ErrTokenInvalid
    }

    if err := claims.Validate(); err != nil {
        return Claims{}, err
    }

    if claims.IsExpired(now) {
        return claims, ErrTokenExpired
    }

    return claims, nil
}

// signature is the base64url mac over the signing input.
func (signer *Signer) signature(signingInput string) string {
    mac := hmac.New(sha256.New, signer.secret)
    mac.Write([]byte(signingInput))

    return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// matches compares the presented signature with the expected one.
//
// hmac.Equal is a constant time comparison. A plain string comparison returns
// as soon as two bytes differ, and that timing is enough to recover a signature
// one byte at a time given enough attempts.
func (signer *Signer) matches(signingInput string, presented string) bool {
    expected, err := base64.RawURLEncoding.DecodeString(presented)
    if err != nil {
        return false
    }

    mac := hmac.New(sha256.New, signer.secret)
    mac.Write([]byte(signingInput))

    return hmac.Equal(mac.Sum(nil), expected)
}

// encodeSegment writes one json value as a base64url segment.
//
// Raw encoding, so there is no padding. A padded segment is not what any other
// implementation produces, and the '=' would have to be escaped in a cookie.
func encodeSegment(value any) (string, error) {
    encoded, err := json.Marshal(value)
    if err != nil {
        return "", errors.Join(ErrTokenInvalid, err)
    }

    return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// decodeSegment reads one base64url segment into a value.
func decodeSegment(segment string, into any) error {
    decoded, err := base64.RawURLEncoding.DecodeString(segment)
    if err != nil {
        return ErrTokenInvalid
    }

    if err := json.Unmarshal(decoded, into); err != nil {
        return ErrTokenInvalid
    }

    return nil
}
