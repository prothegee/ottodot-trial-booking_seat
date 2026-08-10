package auth

import (
	"context"
	"sync"
	"time"
)

// MemoryRefreshStore is the fake, and it holds the same rules the sql holds.
//
// It exists so the four fast tiers need no database. The contract suite runs
// against this and against Postgres, which is what stops it drifting into a
// store that agrees with the tests and disagrees with production.
type MemoryRefreshStore struct {
	mutex sync.Mutex

	// byHash is the only index, because the hash is the only thing a client
	// ever presents. Nothing looks a token up by id, so nothing here does.
	byHash map[string]*RefreshRecord
}

// NewMemoryRefreshStore builds an empty store.
func NewMemoryRefreshStore() *MemoryRefreshStore {
	return &MemoryRefreshStore{byHash: make(map[string]*RefreshRecord)}
}

// Issue writes the first token of a new family.
func (store *MemoryRefreshStore) Issue(ctx context.Context, request IssueRequest) (RefreshRecord, error) {
	if err := request.Validate(); err != nil {
		return RefreshRecord{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	return store.insertLocked(RefreshRecord{
		ID:        request.TokenID,
		ParentID:  request.ParentID,
		FamilyID:  request.FamilyID,
		TokenHash: copyHash(request.TokenHash),
		ExpiresAt: request.ExpiresAt,
		CreatedAt: request.Now,
	})
}

// Rotate spends the presented token and writes its successor.
//
// The lock is held across the whole decision, which is the fake's version of
// what the sql does with a transaction and a conditional update. Two goroutines
// presenting the same token cannot both find it live.
func (store *MemoryRefreshStore) Rotate(ctx context.Context, request RotateRequest) (RefreshRecord, error) {
	if err := request.Validate(); err != nil {
		return RefreshRecord{}, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	presented, found := store.byHash[string(request.PresentedHash)]
	if !found {
		return RefreshRecord{}, ErrTokenNotFound
	}

	// A spent token coming back means two parties hold it and one of them
	// stole it. Ending the chain here signs the real parent out as well, which
	// is the correct trade rather than a flaw: this service cannot tell which
	// of the two is the thief.
	if presented.IsRevoked() {
		store.revokeFamilyLocked(presented.FamilyID, request.Now)

		return RefreshRecord{}, ErrTokenReused
	}

	if presented.IsExpired(request.Now) {
		return RefreshRecord{}, ErrTokenExpired
	}

	presented.RevokedAt = request.Now

	return store.insertLocked(RefreshRecord{
		ID:        request.NextTokenID,
		ParentID:  presented.ParentID,
		FamilyID:  presented.FamilyID,
		TokenHash: copyHash(request.NextTokenHash),
		ExpiresAt: request.NextExpiresAt,
		CreatedAt: request.Now,
	})
}

// RevokeFamily ends every live token in one chain.
func (store *MemoryRefreshStore) RevokeFamily(ctx context.Context, request RevokeFamilyRequest) (int, error) {
	if err := request.Validate(); err != nil {
		return 0, err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	return store.revokeFamilyLocked(request.FamilyID, request.Now), nil
}

// Record reads one token by its hash.
func (store *MemoryRefreshStore) Record(ctx context.Context, tokenHash []byte) (RefreshRecord, error) {
	if len(tokenHash) == 0 {
		return RefreshRecord{}, ErrInvalidRequest
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	stored, found := store.byHash[string(tokenHash)]
	if !found {
		return RefreshRecord{}, ErrTokenNotFound
	}

	return *stored, nil
}

// insertLocked writes one row, refusing a hash that is already held.
func (store *MemoryRefreshStore) insertLocked(record RefreshRecord) (RefreshRecord, error) {
	key := string(record.TokenHash)

	if _, taken := store.byHash[key]; taken {
		return RefreshRecord{}, ErrDuplicateToken
	}

	stored := record
	store.byHash[key] = &stored

	return stored, nil
}

// revokeFamilyLocked ends every live token in one chain and reports the count.
//
// Already revoked rows are left at the instant they were revoked rather than
// restamped, so the audit reads as what happened rather than as what the last
// call did.
func (store *MemoryRefreshStore) revokeFamilyLocked(familyID string, now time.Time) int {
	ended := 0

	for _, held := range store.byHash {
		if held.FamilyID != familyID || held.IsRevoked() {
			continue
		}

		held.RevokedAt = now
		ended++
	}

	return ended
}

// copyHash takes the caller's bytes out of their hands.
//
// Without it, a caller reusing one buffer across two calls would silently
// rewrite a stored row, and the fake would hold something the database never
// could.
func copyHash(hash []byte) []byte {
	held := make([]byte, len(hash))
	copy(held, hash)

	return held
}
