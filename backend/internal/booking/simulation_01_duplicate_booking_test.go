package booking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/booking"
)

// Simulation 1: duplicate booking rejected.
//
//	parent -> api: book child C into class X
//	api    -> repository: insert booking pending_payment, ok
//	parent -> api: book child C into class X again
//	api    -> repository: refused by uq_booking_active
//	api    -> parent: 409 already_booked
//
// Asserts: exactly one booking exists for that child and class, and the second
// request never creates a row.

const (
	duplicateParent  = "0192c001-0000-7000-8000-000000000001"
	duplicateStudent = "0192c001-0000-7000-8000-000000000011"
	duplicateClass   = "0192c001-0000-7000-8000-000000000021"

	firstAttemptID  = "0192c001-0000-7000-8000-000000000031"
	secondAttemptID = "0192c001-0000-7000-8000-000000000032"
)

func TestSimulation01DuplicateBookingRejected(t *testing.T) {
	t.Run("behaviour: the same child cannot be booked into the same class twice", func(t *testing.T) {
		ctx := context.Background()

		repository := booking.NewMemoryRepository()
		repository.AddStudent(duplicateStudent, duplicateParent)
		repository.AddClass(booking.Class{
			ID: duplicateClass, Subject: "science", Title: "Science Discovery",
			StartsAt: contractMoment.Add(72 * time.Hour), DurationMinutes: 60,
			Capacity: 4, HoldAllowance: 2,
		})

		// The identifiers are handed out in a known order, so the test can ask
		// afterwards whether the second one was ever written.
		minted := []string{firstAttemptID, secondAttemptID}
		handedOut := 0

		settings := settingsAt(contractMoment)
		settings.NewBookingID = func() (string, error) {
			next := minted[handedOut]
			handedOut++

			return next, nil
		}

		service, err := booking.NewService(repository, settings)
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		first, err := service.Hold(ctx, booking.HoldCommand{StudentID: duplicateStudent, ClassID: duplicateClass})
		if err != nil {
			t.Fatalf("the first booking must be granted, got: %v", err)
		}

		if first.ID != firstAttemptID {
			t.Fatalf("expected the first identifier, got %s", first.ID)
		}

		_, err = service.Hold(ctx, booking.HoldCommand{StudentID: duplicateStudent, ClassID: duplicateClass})
		if !errors.Is(err, booking.ErrAlreadyBooked) {
			t.Fatalf("expected the second attempt to be refused with ErrAlreadyBooked, got: %v", err)
		}

		if _, err := service.Booking(ctx, secondAttemptID); !errors.Is(err, booking.ErrBookingNotFound) {
			t.Fatal("the refused attempt must leave no row behind")
		}

		still, err := service.Booking(ctx, firstAttemptID)
		if err != nil {
			t.Fatalf("the first booking must survive the refused attempt, got: %v", err)
		}

		if still.Status != booking.StatusPendingPayment {
			t.Fatalf("the first booking must be untouched, got %s", still.Status)
		}
	})

	t.Run("behaviour: the child can book again once the first booking is cancelled", func(t *testing.T) {
		// The rule is one live booking, not one booking ever. A parent who
		// cancels has to be able to start over, and that is the same index
		// doing its job rather than a second rule.
		ctx := context.Background()

		repository := booking.NewMemoryRepository()
		repository.AddStudent(duplicateStudent, duplicateParent)
		repository.AddClass(booking.Class{
			ID: duplicateClass, Subject: "science", Title: "Science Discovery",
			DurationMinutes: 60, Capacity: 4, HoldAllowance: 2,
		})

		service, err := booking.NewService(repository, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		first, err := service.Hold(ctx, booking.HoldCommand{StudentID: duplicateStudent, ClassID: duplicateClass})
		if err != nil {
			t.Fatalf("the first booking must be granted, got: %v", err)
		}

		if _, err := service.Cancel(ctx, first.ID, booking.ActorParent, "changed their mind"); err != nil {
			t.Fatalf("expected the cancel to succeed, got: %v", err)
		}

		if _, err := service.Hold(ctx, booking.HoldCommand{StudentID: duplicateStudent, ClassID: duplicateClass}); err != nil {
			t.Fatalf("a cancelled booking must not block a new one, got: %v", err)
		}
	})
}
