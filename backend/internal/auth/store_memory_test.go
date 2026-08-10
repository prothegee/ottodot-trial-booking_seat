package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
)

// memoryStoreFixture points the shared contract at the fake.
type memoryStoreFixture struct {
	store *auth.MemoryRefreshStore
}

func newMemoryStoreFixture(t *testing.T) storeFixture {
	return &memoryStoreFixture{store: auth.NewMemoryRefreshStore()}
}

func (fixture *memoryStoreFixture) Store() auth.RefreshStore {
	return fixture.store
}

// ParentID is a fixed seeded parent. The fake has no foreign key, so any id
// would do, and a real one keeps the two fixtures reading the same.
func (fixture *memoryStoreFixture) ParentID() string {
	return "0192a000-0000-7000-8000-000000000001"
}

func TestTheMemoryRefreshStoreHonoursTheContract(t *testing.T) {
	runRefreshStoreContract(t, newMemoryStoreFixture)
}

func TestTheMemoryRefreshStoreSpendsATokenOnce(t *testing.T) {
	t.Run("edge: two rotations of one token in parallel produce one successor", func(t *testing.T) {
		// The fake serializes with a mutex, which is not what production does.
		// It is asserted here anyway, because a fake that let both through
		// would make every reuse test above pass for the wrong reason. The
		// proof on real connections lives beside the Postgres fixture.
		store := auth.NewMemoryRefreshStore()
		ctx := context.Background()

		if _, err := store.Issue(ctx, auth.IssueRequest{
			TokenID:   newTokenID(t),
			ParentID:  "0192a000-0000-7000-8000-000000000001",
			FamilyID:  newTokenID(t),
			TokenHash: auth.HashRefreshToken("R1"),
			ExpiresAt: storeMoment.Add(contractRefreshTTL),
			Now:       storeMoment,
		}); err != nil {
			t.Fatalf("cannot issue: %v", err)
		}

		const racers = 8

		var (
			start    sync.WaitGroup
			finished sync.WaitGroup
			mutex    sync.Mutex
		)

		succeeded := 0
		reused := 0

		start.Add(1)
		finished.Add(racers)

		for index := 0; index < racers; index++ {
			go func(attempt int) {
				defer finished.Done()

				start.Wait()

				_, err := store.Rotate(ctx, auth.RotateRequest{
					PresentedHash: auth.HashRefreshToken("R1"),
					NextTokenID:   newTokenID(t),
					NextTokenHash: auth.HashRefreshToken(successorName(attempt)),
					NextExpiresAt: storeMoment.Add(contractRefreshTTL),
					Now:           storeMoment.Add(time.Hour),
				})

				mutex.Lock()
				defer mutex.Unlock()

				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, auth.ErrTokenReused):
					reused++
				default:
					t.Errorf("unexpected failure: %v", err)
				}
			}(index)
		}

		start.Done()
		finished.Wait()

		if succeeded != 1 {
			t.Fatalf("expected exactly one rotation to succeed, got %d", succeeded)
		}

		if reused != racers-1 {
			t.Fatalf("expected the other %d to be reported as reuse, got %d", racers-1, reused)
		}
	})
}

// successorName gives each racer its own successor token, so a rotation that
// wins is not refused for colliding with another racer's hash.
func successorName(attempt int) string {
	return "successor-" + string(rune('a'+attempt))
}
