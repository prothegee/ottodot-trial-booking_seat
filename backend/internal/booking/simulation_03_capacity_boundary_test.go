package booking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/booking"
)

// Simulation 3: capacity boundary at 3 confirmed.
//
//	parent 4 -> api: pay and confirm
//	api      -> repository: lock class, 3 confirmed, seat 4 free
//	repository -> api: confirmed as seat 4
//	parent 5 -> api: pay and confirm
//	api      -> repository: lock class, 4 confirmed, no free seat
//	repository -> api: rejected
//	api      -> repository: status refund_required
//	api      -> parent 5: 409 seat_lost
//
// Asserts: exactly 4 confirmed, seat numbers 1 to 4, and parent 5 left in
// refund_required.
//
// Note:
//   - the reconcile_refund job that goes with the refund is enqueued in the
//     same transaction once the queue exists. That is phase 4, and this
//     simulation gains the assertion then.

const (
	boundaryParentOne = "0192c003-0000-7000-8000-000000000001"
	boundaryParentTwo = "0192c003-0000-7000-8000-000000000002"

	boundaryClass = "0192c003-0000-7000-8000-000000000021"
)

// boundaryStudents are five children spread over two parents, because one
// parent may hold at most three places at a time.
var boundaryStudents = []struct {
	studentID string
	parentID  string
}{
	{studentID: "0192c003-0000-7000-8000-000000000011", parentID: boundaryParentOne},
	{studentID: "0192c003-0000-7000-8000-000000000012", parentID: boundaryParentOne},
	{studentID: "0192c003-0000-7000-8000-000000000013", parentID: boundaryParentOne},
	{studentID: "0192c003-0000-7000-8000-000000000014", parentID: boundaryParentTwo},
	{studentID: "0192c003-0000-7000-8000-000000000015", parentID: boundaryParentTwo},
}

func TestSimulation03CapacityBoundaryAtThreeConfirmed(t *testing.T) {
	t.Run("behaviour: the fourth parent gets the last seat and the fifth is refunded", func(t *testing.T) {
		ctx := context.Background()

		repository := booking.NewMemoryRepository()

		for _, child := range boundaryStudents {
			repository.AddStudent(child.studentID, child.parentID)
		}

		// Capacity 4 with allowance 2 admits six holders, which is what lets
		// all five parents reach the payment screen in the first place.
		repository.AddClass(booking.Class{
			ID: boundaryClass, Subject: "math", Title: "Math Foundations",
			StartsAt: contractMoment.Add(96 * time.Hour), DurationMinutes: 60,
			Capacity: 4, HoldAllowance: 2,
		})

		service, err := booking.NewService(repository, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		held := make([]booking.Booking, 0, len(boundaryStudents))

		for _, child := range boundaryStudents {
			granted, err := service.Hold(ctx, booking.HoldCommand{StudentID: child.studentID, ClassID: boundaryClass})
			if err != nil {
				t.Fatalf("every one of the five parents must reach the payment screen, got: %v", err)
			}

			held = append(held, granted)
		}

		// The first three settle, which is the state the brief describes.
		for index := range 3 {
			if _, err := service.Confirm(ctx, held[index].ID); err != nil {
				t.Fatalf("the first three must confirm, got: %v", err)
			}
		}

		fourth, err := service.Confirm(ctx, held[3].ID)
		if err != nil {
			t.Fatalf("the fourth parent must take the last seat, got: %v", err)
		}

		if fourth.SeatNo != 4 {
			t.Fatalf("expected seat 4, got %d", fourth.SeatNo)
		}

		fifth, err := service.Confirm(ctx, held[4].ID)
		if !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected the fifth parent to lose the seat, got: %v", err)
		}

		if fifth.Status != booking.StatusRefundRequired {
			t.Fatalf("a parent who paid and lost must be left in refund_required, got %s", fifth.Status)
		}

		taken, err := repository.SeatsTaken(ctx, boundaryClass)
		if err != nil {
			t.Fatalf("expected the taken seats, got: %v", err)
		}

		if len(taken) != 4 {
			t.Fatalf("expected exactly 4 confirmed seats, got %d: %v", len(taken), taken)
		}

		for index, seat := range taken {
			if seat != int16(index+1) {
				t.Fatalf("expected seats 1 to 4 with no gaps and no repeats, got %v", taken)
			}
		}

		remaining, err := service.SeatsRemaining(ctx, boundaryClass)
		if err != nil {
			t.Fatalf("expected a count, got: %v", err)
		}

		if remaining != 0 {
			t.Fatalf("the class must read as full, got %d seats left", remaining)
		}
	})

	t.Run("behaviour: the refund_required booking keeps its audit trail", func(t *testing.T) {
		// The trail is what an operator reads to see that money moved and has
		// to move back. Losing it would leave a charge with no explanation.
		ctx := context.Background()

		repository := booking.NewMemoryRepository()
		repository.AddStudent(boundaryStudents[0].studentID, boundaryParentOne)
		repository.AddStudent(boundaryStudents[3].studentID, boundaryParentTwo)
		repository.AddClass(booking.Class{
			ID: boundaryClass, Subject: "math", Title: "Math Foundations",
			DurationMinutes: 60, Capacity: 1, HoldAllowance: 1,
		})

		service, err := booking.NewService(repository, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		winner, err := service.Hold(ctx, booking.HoldCommand{StudentID: boundaryStudents[0].studentID, ClassID: boundaryClass})
		if err != nil {
			t.Fatalf("expected a hold, got: %v", err)
		}

		loser, err := service.Hold(ctx, booking.HoldCommand{StudentID: boundaryStudents[3].studentID, ClassID: boundaryClass})
		if err != nil {
			t.Fatalf("expected a second hold, got: %v", err)
		}

		if _, err := service.Confirm(ctx, winner.ID); err != nil {
			t.Fatalf("the first to settle must win the seat, got: %v", err)
		}

		if _, err := service.Confirm(ctx, loser.ID); !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected ErrSeatLost, got: %v", err)
		}

		trail, err := repository.Events(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected the audit trail, got: %v", err)
		}

		if len(trail) != 2 {
			t.Fatalf("expected the hold and the lost seat, got %d events", len(trail))
		}

		if trail[1].ToStatus != booking.StatusRefundRequired || trail[1].Actor != booking.ActorSystem {
			t.Fatalf("the trail does not record the system moving the booking to refund_required: %+v", trail[1])
		}
	})
}
