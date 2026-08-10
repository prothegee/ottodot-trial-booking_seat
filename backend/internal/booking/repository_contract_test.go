package booking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/booking"
	"ottodot-trial-booking/backend/internal/identifier"
)

// The contract every repository has to satisfy.
//
// The risk this file exists to remove: the fake reimplements the invariant
// correctly while the sql is wrong, and every fast test stays green. One suite
// pointed at both implementations is the only thing that catches that, so this
// file is run twice, once against the memory repository in the fast tiers and
// once against real Postgres behind the containers tag.
//
// Nothing here proves that two real transactions serialize. That needs real
// connections and lives in race_containers_test.go.

const (
	contractHoldTTL = 10 * time.Minute
	contractHoldCap = 3
)

// Identifiers used across the suite. They are fixed rather than minted so a
// failure names the same student every time it is read.
const (
	parentOne   = "0192b000-0000-7000-8000-000000000001"
	parentTwo   = "0192b000-0000-7000-8000-000000000002"
	parentThree = "0192b000-0000-7000-8000-000000000003"

	studentOne   = "0192b000-0000-7000-8000-000000000011"
	studentTwo   = "0192b000-0000-7000-8000-000000000012"
	studentThree = "0192b000-0000-7000-8000-000000000013"
	studentFour  = "0192b000-0000-7000-8000-000000000014"

	classOpen   = "0192b000-0000-7000-8000-000000000021"
	classSingle = "0192b000-0000-7000-8000-000000000022"
	classTight  = "0192b000-0000-7000-8000-000000000023"
	classSecond = "0192b000-0000-7000-8000-000000000024"

	unknownIdentifier = "0192b000-0000-7000-8000-0000000000ff"
)

// contractMoment is the instant every case starts from.
var contractMoment = time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)

// repositoryFixture is one repository with a way to put rows in front of it.
// The memory version writes into maps, the Postgres version inserts into a
// throwaway schema.
type repositoryFixture interface {
	Repository() booking.Repository
	AddClass(t *testing.T, class booking.Class)
	AddStudent(t *testing.T, studentID string, parentID string)
}

// seedContractFixture puts the same classes and children in front of either
// implementation.
//
//	classOpen   4 seats, allowance 2, so 6 holders
//	classSingle 1 seat,  allowance 0, so 1 holder
//	classTight  1 seat,  allowance 1, so 2 holders, one of whom will be refunded
//	classSecond 4 seats, allowance 2, used when a case needs a second class
func seedContractFixture(t *testing.T, fixture repositoryFixture) {
	t.Helper()

	fixture.AddStudent(t, studentOne, parentOne)
	fixture.AddStudent(t, studentTwo, parentOne)
	fixture.AddStudent(t, studentThree, parentTwo)
	fixture.AddStudent(t, studentFour, parentThree)

	fixture.AddClass(t, booking.Class{
		ID: classOpen, Subject: "science", Title: "Science Discovery",
		StartsAt: contractMoment.Add(72 * time.Hour), DurationMinutes: 60,
		Capacity: 4, HoldAllowance: 2,
	})

	fixture.AddClass(t, booking.Class{
		ID: classSingle, Subject: "math", Title: "Math One Seat",
		StartsAt: contractMoment.Add(96 * time.Hour), DurationMinutes: 60,
		Capacity: 1, HoldAllowance: 0,
	})

	fixture.AddClass(t, booking.Class{
		ID: classTight, Subject: "science", Title: "Science Last Seat",
		StartsAt: contractMoment.Add(120 * time.Hour), DurationMinutes: 60,
		Capacity: 1, HoldAllowance: 1,
	})

	fixture.AddClass(t, booking.Class{
		ID: classSecond, Subject: "math", Title: "Math Foundations",
		StartsAt: contractMoment.Add(144 * time.Hour), DurationMinutes: 60,
		Capacity: 4, HoldAllowance: 2,
	})
}

// newBookingID mints an identifier in the same format the service uses, so the
// Postgres uuid columns accept it.
func newBookingID(t *testing.T) string {
	t.Helper()

	minted, err := identifier.NewUUIDv7()
	if err != nil {
		t.Fatalf("cannot mint an identifier: %v", err)
	}

	return minted
}

// holdRequestFor builds a request with the suite's policy already applied.
func holdRequestFor(t *testing.T, studentID string, classID string, now time.Time) booking.HoldRequest {
	t.Helper()

	return booking.HoldRequest{
		BookingID:         newBookingID(t),
		StudentID:         studentID,
		ClassID:           classID,
		Now:               now,
		ExpiresAt:         booking.HoldDeadline(now, contractHoldTTL),
		MaxHoldsPerParent: contractHoldCap,
	}
}

// mustHold grants a hold and fails the test if it was refused.
func mustHold(t *testing.T, repository booking.Repository, studentID string, classID string, now time.Time) booking.Booking {
	t.Helper()

	granted, err := repository.Hold(context.Background(), holdRequestFor(t, studentID, classID, now))
	if err != nil {
		t.Fatalf("expected a hold for student %s in class %s, got: %v", studentID, classID, err)
	}

	return granted
}

// mustConfirm confirms a booking and fails the test if it was refused.
func mustConfirm(t *testing.T, repository booking.Repository, bookingID string, now time.Time) booking.Booking {
	t.Helper()

	confirmed, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: bookingID, Now: now})
	if err != nil {
		t.Fatalf("expected the confirm to succeed, got: %v", err)
	}

	return confirmed
}

// runRepositoryContract is the suite itself. Every case gets its own fixture,
// so nothing leaks from one to the next.
func runRepositoryContract(t *testing.T, newFixture func(t *testing.T) repositoryFixture) {
	t.Helper()

	ctx := context.Background()

	t.Run("integration: a hold is granted on an open class", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

		if granted.Status != booking.StatusPendingPayment {
			t.Fatalf("expected pending_payment, got %s", granted.Status)
		}

		if granted.HasSeat() {
			t.Fatalf("a hold must not carry a seat, got seat %d", granted.SeatNo)
		}

		if !granted.HoldExpiresAt.Equal(contractMoment.Add(contractHoldTTL)) {
			t.Fatalf("expected the deadline at %s, got %s",
				contractMoment.Add(contractHoldTTL), granted.HoldExpiresAt)
		}
	})

	t.Run("integration: a granted hold can be read back", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

		found, err := repository.Booking(ctx, granted.ID)
		if err != nil {
			t.Fatalf("expected to read the booking back, got: %v", err)
		}

		if found.ID != granted.ID || found.Status != granted.Status {
			t.Fatalf("the booking read back does not match the one granted: %+v", found)
		}
	})

	t.Run("edge: a second live booking for the same child and class is refused", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classOpen, contractMoment)

		_, err := repository.Hold(ctx, holdRequestFor(t, studentOne, classOpen, contractMoment))
		if !errors.Is(err, booking.ErrAlreadyBooked) {
			t.Fatalf("expected ErrAlreadyBooked, got: %v", err)
		}
	})

	t.Run("edge: the same child may hold a place in a different class", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classOpen, contractMoment)
		mustHold(t, repository, studentOne, classSecond, contractMoment)
	})

	t.Run("edge: a parent already at the hold cap is refused", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classOpen, contractMoment)

		// The cap is carried in the request, so this case sets it to one rather
		// than opening three bookings to reach the default.
		atTheCap := holdRequestFor(t, studentTwo, classSecond, contractMoment)
		atTheCap.MaxHoldsPerParent = 1

		_, err := repository.Hold(ctx, atTheCap)
		if !errors.Is(err, booking.ErrTooManyHolds) {
			t.Fatalf("expected ErrTooManyHolds, got: %v", err)
		}
	})

	t.Run("edge: another parent is unaffected by the first parent's holds", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classOpen, contractMoment)

		otherParent := holdRequestFor(t, studentThree, classSecond, contractMoment)
		otherParent.MaxHoldsPerParent = 1

		if _, err := repository.Hold(ctx, otherParent); err != nil {
			t.Fatalf("one parent's holds must not count against another, got: %v", err)
		}
	})

	t.Run("edge: a class at capacity plus allowance refuses another holder", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classTight, contractMoment)
		mustHold(t, repository, studentThree, classTight, contractMoment)

		_, err := repository.Hold(ctx, holdRequestFor(t, studentFour, classTight, contractMoment))
		if !errors.Is(err, booking.ErrClassFull) {
			t.Fatalf("expected ErrClassFull once capacity plus allowance is taken, got: %v", err)
		}
	})

	t.Run("edge: a lapsed hold stops occupying a slot", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		mustHold(t, repository, studentOne, classSingle, contractMoment)

		// classSingle admits exactly one holder, so the second parent only gets
		// in because the first hold has lapsed by then. This is what stops a
		// parent who walked away from freezing a class until the worker runs.
		afterTheDeadline := contractMoment.Add(contractHoldTTL)

		if _, err := repository.Hold(ctx, holdRequestFor(t, studentThree, classSingle, afterTheDeadline)); err != nil {
			t.Fatalf("a lapsed hold must free the slot, got: %v", err)
		}
	})

	t.Run("integration: the confirm transaction assigns the lowest free seat", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		first := mustHold(t, repository, studentOne, classOpen, contractMoment)
		second := mustHold(t, repository, studentTwo, classOpen, contractMoment)
		third := mustHold(t, repository, studentThree, classOpen, contractMoment)

		if seat := mustConfirm(t, repository, first.ID, contractMoment).SeatNo; seat != 1 {
			t.Fatalf("expected seat 1, got %d", seat)
		}

		if seat := mustConfirm(t, repository, second.ID, contractMoment).SeatNo; seat != 2 {
			t.Fatalf("expected seat 2, got %d", seat)
		}

		if seat := mustConfirm(t, repository, third.ID, contractMoment).SeatNo; seat != 3 {
			t.Fatalf("expected seat 3, got %d", seat)
		}

		taken, err := repository.SeatsTaken(ctx, classOpen)
		if err != nil {
			t.Fatalf("expected the taken seats, got: %v", err)
		}

		if len(taken) != 3 || taken[0] != 1 || taken[1] != 2 || taken[2] != 3 {
			t.Fatalf("expected seats 1, 2, 3 in order, got %v", taken)
		}
	})

	t.Run("integration: a confirmed booking is stamped and stops holding", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
		confirmed := mustConfirm(t, repository, granted.ID, contractMoment)

		if confirmed.Status != booking.StatusConfirmed {
			t.Fatalf("expected confirmed, got %s", confirmed.Status)
		}

		if confirmed.ConfirmedAt.IsZero() {
			t.Fatal("a confirmed booking must carry the instant it was confirmed")
		}

		if !confirmed.HoldExpiresAt.IsZero() {
			t.Fatalf("a confirmed booking is no longer holding, got a deadline of %s", confirmed.HoldExpiresAt)
		}
	})

	t.Run("edge: a payment that settles after the last seat is gone lands in refund_required", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		winner := mustHold(t, repository, studentOne, classTight, contractMoment)
		loser := mustHold(t, repository, studentThree, classTight, contractMoment)

		mustConfirm(t, repository, winner.ID, contractMoment)

		lost, err := repository.Confirm(ctx, booking.ConfirmRequest{BookingID: loser.ID, Now: contractMoment})
		if !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected ErrSeatLost, got: %v", err)
		}

		if lost.Status != booking.StatusRefundRequired {
			t.Fatalf("expected refund_required, got %s", lost.Status)
		}

		// The outcome has to survive, because money already moved and something
		// has to tell an operator to move it back.
		stored, err := repository.Booking(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected to read the losing booking back, got: %v", err)
		}

		if stored.Status != booking.StatusRefundRequired {
			t.Fatalf("the refund_required outcome was not kept, got %s", stored.Status)
		}

		if stored.HasSeat() {
			t.Fatalf("a booking that lost the race must hold no seat, got %d", stored.SeatNo)
		}
	})

	t.Run("edge: a booking that is not holding cannot be confirmed", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
		mustConfirm(t, repository, granted.ID, contractMoment)

		_, err := repository.Confirm(ctx, booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment})
		if !errors.Is(err, booking.ErrNotHolding) {
			t.Fatalf("expected ErrNotHolding on a second confirm, got: %v", err)
		}
	})

	t.Run("integration: cancelling releases the seat and frees the child to book again", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
		mustConfirm(t, repository, granted.ID, contractMoment)

		withdrawn, err := repository.Cancel(ctx, booking.CancelRequest{
			BookingID: granted.ID,
			Actor:     booking.ActorAdmin,
			Reason:    "withdrawn by an operator",
			Now:       contractMoment,
		})
		if err != nil {
			t.Fatalf("expected the cancel to succeed, got: %v", err)
		}

		if withdrawn.Status != booking.StatusCancelled || withdrawn.HasSeat() {
			t.Fatalf("a cancelled booking must hold no seat, got %s seat %d", withdrawn.Status, withdrawn.SeatNo)
		}

		taken, err := repository.SeatsTaken(ctx, classOpen)
		if err != nil {
			t.Fatalf("expected the taken seats, got: %v", err)
		}

		if len(taken) != 0 {
			t.Fatalf("the seat was not released, still taken: %v", taken)
		}

		// The uq_booking_active index only counts live bookings, so the same
		// child can now book the same class again.
		mustHold(t, repository, studentOne, classOpen, contractMoment)
	})

	t.Run("edge: the seat a cancellation released is the next one handed out", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		first := mustHold(t, repository, studentOne, classOpen, contractMoment)
		second := mustHold(t, repository, studentTwo, classOpen, contractMoment)
		third := mustHold(t, repository, studentThree, classOpen, contractMoment)

		mustConfirm(t, repository, first.ID, contractMoment)
		mustConfirm(t, repository, second.ID, contractMoment)
		mustConfirm(t, repository, third.ID, contractMoment)

		if _, err := repository.Cancel(ctx, booking.CancelRequest{
			BookingID: second.ID,
			Actor:     booking.ActorAdmin,
			Reason:    "withdrawn by an operator",
			Now:       contractMoment,
		}); err != nil {
			t.Fatalf("expected the cancel to succeed, got: %v", err)
		}

		fourth := mustHold(t, repository, studentFour, classOpen, contractMoment)

		if seat := mustConfirm(t, repository, fourth.ID, contractMoment).SeatNo; seat != 2 {
			t.Fatalf("expected the freed seat 2 to be reused, got %d", seat)
		}
	})

	t.Run("edge: a finished booking cannot be cancelled a second time", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

		if _, err := repository.Cancel(ctx, booking.CancelRequest{
			BookingID: granted.ID, Actor: booking.ActorParent, Reason: "changed their mind", Now: contractMoment,
		}); err != nil {
			t.Fatalf("expected the first cancel to succeed, got: %v", err)
		}

		_, err := repository.Cancel(ctx, booking.CancelRequest{
			BookingID: granted.ID, Actor: booking.ActorParent, Reason: "changed their mind", Now: contractMoment,
		})
		if !errors.Is(err, booking.ErrInvalidTransition) {
			t.Fatalf("expected ErrInvalidTransition, got: %v", err)
		}
	})

	t.Run("integration: every transition leaves a line in the audit trail", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
		mustConfirm(t, repository, granted.ID, contractMoment)

		trail, err := repository.Events(ctx, granted.ID)
		if err != nil {
			t.Fatalf("expected the audit trail, got: %v", err)
		}

		if len(trail) != 2 {
			t.Fatalf("expected two events, got %d: %+v", len(trail), trail)
		}

		if trail[0].FromStatus != "" || trail[0].ToStatus != booking.StatusPendingPayment {
			t.Fatalf("the first event is not the hold: %+v", trail[0])
		}

		if trail[0].Actor != booking.ActorParent {
			t.Fatalf("a hold is the parent acting, got %s", trail[0].Actor)
		}

		if trail[1].FromStatus != booking.StatusPendingPayment || trail[1].ToStatus != booking.StatusConfirmed {
			t.Fatalf("the second event is not the confirmation: %+v", trail[1])
		}
	})

	t.Run("edge: unknown identifiers are reported as not found", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		if _, err := repository.Class(ctx, unknownIdentifier); !errors.Is(err, booking.ErrClassNotFound) {
			t.Fatalf("expected ErrClassNotFound, got: %v", err)
		}

		if _, err := repository.Booking(ctx, unknownIdentifier); !errors.Is(err, booking.ErrBookingNotFound) {
			t.Fatalf("expected ErrBookingNotFound, got: %v", err)
		}

		_, err := repository.Hold(ctx, holdRequestFor(t, studentOne, unknownIdentifier, contractMoment))
		if !errors.Is(err, booking.ErrClassNotFound) {
			t.Fatalf("expected ErrClassNotFound when holding in a class that does not exist, got: %v", err)
		}

		_, err = repository.Hold(ctx, holdRequestFor(t, unknownIdentifier, classOpen, contractMoment))
		if !errors.Is(err, booking.ErrStudentNotFound) {
			t.Fatalf("expected ErrStudentNotFound for a child that does not exist, got: %v", err)
		}

		_, err = repository.Confirm(ctx, booking.ConfirmRequest{BookingID: unknownIdentifier, Now: contractMoment})
		if !errors.Is(err, booking.ErrBookingNotFound) {
			t.Fatalf("expected ErrBookingNotFound, got: %v", err)
		}

		_, err = repository.Cancel(ctx, booking.CancelRequest{
			BookingID: unknownIdentifier, Actor: booking.ActorParent, Now: contractMoment,
		})
		if !errors.Is(err, booking.ErrBookingNotFound) {
			t.Fatalf("expected ErrBookingNotFound, got: %v", err)
		}
	})

	t.Run("edge: a class nobody has booked reports no taken seats", func(t *testing.T) {
		fixture := newFixture(t)
		seedContractFixture(t, fixture)
		repository := fixture.Repository()

		taken, err := repository.SeatsTaken(ctx, classOpen)
		if err != nil {
			t.Fatalf("expected an empty answer, got: %v", err)
		}

		if len(taken) != 0 {
			t.Fatalf("expected no taken seats, got %v", taken)
		}
	})
}
