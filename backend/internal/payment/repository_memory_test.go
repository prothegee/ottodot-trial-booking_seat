package payment_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ottodot-trial-booking/backend/internal/payment"
)

// memoryFixture points the shared contract at the fake.
type memoryFixture struct {
	repository *payment.MemoryRepository
}

func newMemoryFixture(_ *testing.T) repositoryFixture {
	return &memoryFixture{repository: payment.NewMemoryRepository()}
}

func (fixture *memoryFixture) Repository() payment.Repository {
	return fixture.repository
}

func (fixture *memoryFixture) AddBooking(_ *testing.T, bookingID string) {
	fixture.repository.AddBooking(bookingID)
}

func TestTheMemoryRepositoryHonoursTheContract(t *testing.T) {
	runRepositoryContract(t, newMemoryFixture)
}

func TestTheMemoryRepositoryIsSafeToShare(t *testing.T) {
	t.Run("integration: parallel calls with one key open exactly one attempt", func(t *testing.T) {
		// The same shape as the real-database proof, and worth running here for
		// a different reason: it is what catches a missing lock in the fake. It
		// proves nothing about the index, because there is no transaction here.
		fixture := newMemoryFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		const callers = 8

		var (
			waitGroup sync.WaitGroup
			mutex     sync.Mutex
			opened    int
			replays   int
		)

		for i := 0; i < callers; i++ {
			waitGroup.Add(1)

			go func() {
				defer waitGroup.Done()

				_, replayed, err := repository.Begin(context.Background(), beginRequestFor(t, bookingOne, "key-parallel"))

				mutex.Lock()
				defer mutex.Unlock()

				switch {
				case err != nil:
					t.Errorf("unexpected failure from a parallel call: %v", err)
				case replayed:
					replays++
				default:
					opened++
				}
			}()
		}

		waitGroup.Wait()

		if opened != 1 {
			t.Fatalf("expected exactly one attempt to be opened, got %d", opened)
		}

		if replays != callers-1 {
			t.Fatalf("expected the other %d callers to be told it was a replay, got %d", callers-1, replays)
		}

		stored, err := repository.AttemptsFor(context.Background(), bookingOne)
		if err != nil {
			t.Fatalf("expected the attempts for the booking, got: %v", err)
		}

		if len(stored) != 1 {
			t.Fatalf("expected one row for one key, got %d", len(stored))
		}
	})
}

func TestTheMemoryRepositoryRefusesAnIncompleteRequest(t *testing.T) {
	t.Run("edge: an opening write with no attempt id and no booking is refused", func(t *testing.T) {
		repository := payment.NewMemoryRepository()

		_, _, err := repository.Begin(context.Background(), payment.BeginRequest{
			IdempotencyKey: "key-empty", Amount: contractAmount(), Now: contractMoment,
		})
		if !errors.Is(err, payment.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}
	})
}
