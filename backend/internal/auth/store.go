package auth

import (
	"context"
	"time"
)

// RefreshStore is the only way a refresh token is written, spent, or ended.
//
// It exists as an interface for one reason: the four fast test tiers run
// against a fake, and the same behaviour suite runs against real Postgres in
// the proof tier. Here the disagreement that matters is whether one token can
// be spent twice, because that is what separates a rotation from a replay.
//
// Rotate is one transaction rather than a read followed by a write. A caller
// that read a row, decided it was live, and then updated it would leave a
// window in which two refreshes both read the same live row and both succeed,
// which is the exact hole reuse detection exists to close.
type RefreshStore interface {
	// Issue writes the first token of a new family, which is what a sign in
	// produces.
	Issue(ctx context.Context, request IssueRequest) (RefreshRecord, error)

	// Rotate spends the presented token and writes its successor in the same
	// family, in one transaction.
	//
	// It reports ErrTokenReused when the presented token was already spent,
	// and the whole family is revoked by the time it does.
	Rotate(ctx context.Context, request RotateRequest) (RefreshRecord, error)

	// RevokeFamily ends every live token in one chain and reports how many it
	// ended. A logout calls it, and so does reuse detection.
	RevokeFamily(ctx context.Context, request RevokeFamilyRequest) (int, error)

	// Record reads one token by its hash. It reports ErrTokenNotFound when
	// there is none.
	Record(ctx context.Context, tokenHash []byte) (RefreshRecord, error)
}

// RefreshRecord is one stored refresh token.
//
// It carries the hash and never the token. Nothing downstream can turn this
// back into something a client could present, which is the property the whole
// storage rule is built on.
type RefreshRecord struct {
	ID       string
	ParentID string

	// FamilyID groups one rotation chain. Every token descended from one sign
	// in shares it, which is what makes "revoke the whole family" a single
	// condition rather than a walk.
	FamilyID string

	TokenHash []byte

	ExpiresAt time.Time

	// RevokedAt is the zero value while the token is live.
	RevokedAt time.Time

	CreatedAt time.Time
}

// IsRevoked reports whether this token has already been spent or ended.
func (record RefreshRecord) IsRevoked() bool {
	return !record.RevokedAt.IsZero()
}

// IsExpired reports whether this token's life is over at the given instant.
//
// The boundary is inclusive, matching the access token: a token expiring on
// this exact instant is spent.
func (record RefreshRecord) IsExpired(now time.Time) bool {
	return !now.Before(record.ExpiresAt)
}

// IssueRequest is everything the first write of a family needs.
//
// The instant and both identifiers are handed in rather than read inside the
// implementation, so both answer identically and a test can pin the clock
// instead of sleeping.
type IssueRequest struct {
	TokenID  string
	ParentID string
	FamilyID string

	// TokenHash is what the row holds. The caller keeps the token itself and
	// this package never sees it again.
	TokenHash []byte

	ExpiresAt time.Time
	Now       time.Time
}

// Validate refuses a write nobody could act on, before either implementation
// touches storage.
//
// Return:
//   - nil when the request describes a token this service can later find
//   - ErrInvalidRequest otherwise
func (request IssueRequest) Validate() error {
	if request.TokenID == "" || request.ParentID == "" || request.FamilyID == "" {
		return ErrInvalidRequest
	}

	if len(request.TokenHash) == 0 || request.Now.IsZero() {
		return ErrInvalidRequest
	}

	if !request.ExpiresAt.After(request.Now) {
		return ErrInvalidRequest
	}

	return nil
}

// RotateRequest spends one token and writes the next.
type RotateRequest struct {
	// PresentedHash is the hash of whatever the client sent. The token itself
	// never reaches storage, on either path.
	PresentedHash []byte

	NextTokenID   string
	NextTokenHash []byte
	NextExpiresAt time.Time

	// Now decides whether the presented token has expired, and stamps the
	// revocation of the old row and the creation of the new one.
	Now time.Time
}

// Validate refuses a rotation that could not be completed, before any row is
// locked.
func (request RotateRequest) Validate() error {
	if len(request.PresentedHash) == 0 || len(request.NextTokenHash) == 0 {
		return ErrInvalidRequest
	}

	if request.NextTokenID == "" || request.Now.IsZero() {
		return ErrInvalidRequest
	}

	if !request.NextExpiresAt.After(request.Now) {
		return ErrInvalidRequest
	}

	return nil
}

// RevokeFamilyRequest ends one chain.
type RevokeFamilyRequest struct {
	FamilyID string

	// Now stamps the revocation.
	Now time.Time
}

// Validate refuses a revocation that names nothing.
func (request RevokeFamilyRequest) Validate() error {
	if request.FamilyID == "" || request.Now.IsZero() {
		return ErrInvalidRequest
	}

	return nil
}
