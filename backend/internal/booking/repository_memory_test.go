package booking_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ottodot-trial-booking/backend/internal/booking"
)

// memoryFixture points the shared contract at the fake.
type memoryFixture struct {
	repository *booking.MemoryRepository
}

func newMemoryFixture(_ *testing.T) repositoryFixture {
	return &memoryFixture{repository: booking.NewMemoryRepository()}
}

func (fixture *memoryFixture) Repository() booking.Repository {
	return fixture.repository
}

func (fixture *memoryFixture) AddClass(_ *testing.T, class booking.Class) {
	fixture.repository.AddClass(class)
}

func (fixture *memoryFixture) AddStudent(_ *testing.T, studentID string, parentID string) {
	fixture.repository.AddStudent(studentID, parentID)
}

func TestTheMemoryRepositoryHonoursTheContract(t *testing.T) {
	runRepositoryContract(t, newMemoryFixture)
}

func TestTheMemoryRepositoryIsSafeToShare(t *testing.T) {
	t.Run("integration: parallel confirms on one seat produce exactly one winner", func(t *testing.T) {
		// This is the same shape as the real-database proof, and it is worth
		// running here for a different reason: it is what catches a missing
		// lock in the fake. It proves nothing about the sql, because there is
		// no transaction here. That proof lives behind the containers tag.
		fixture := newMemoryFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		holders := []string{studentOne, studentThree}
		bookings := make([]booking.Booking, 0, len(holders))

		for _, studentID := range holders {
			bookings = append(bookings, mustHold(t, repository, studentID, classTight, contractMoment))
		}

		var (
			waitGroup sync.WaitGroup
			mutex     sync.Mutex
			confirmed int
			lost      int
		)

		for _, held := range bookings {
			waitGroup.Add(1)

			go func(bookingID string) {
				defer waitGroup.Done()

				_, err := repository.Confirm(context.Background(), booking.ConfirmRequest{
					BookingID: bookingID,
					Now:       contractMoment,
				})

				mutex.Lock()
				defer mutex.Unlock()

				switch {
				case err == nil:
					confirmed++
				case errors.Is(err, booking.ErrSeatLost):
					lost++
				default:
					t.Errorf("unexpected failure from a parallel confirm: %v", err)
				}
			}(held.ID)
		}

		waitGroup.Wait()

		if confirmed != 1 {
			t.Fatalf("expected exactly one winner, got %d", confirmed)
		}

		if lost != 1 {
			t.Fatalf("expected exactly one parent to lose the seat, got %d", lost)
		}

		taken, err := repository.SeatsTaken(context.Background(), classTight)
		if err != nil {
			t.Fatalf("expected the taken seats, got: %v", err)
		}

		if len(taken) != 1 || taken[0] != 1 {
			t.Fatalf("expected seat 1 taken exactly once, got %v", taken)
		}
	})
}
