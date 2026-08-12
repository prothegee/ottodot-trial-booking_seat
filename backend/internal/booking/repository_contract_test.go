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

    t.Run("integration: expiring a lapsed hold releases the slot it occupied", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
        afterDeadline := contractMoment.Add(contractHoldTTL)

        lapsed, err := repository.Expire(ctx, booking.ExpireRequest{BookingID: granted.ID, Now: afterDeadline})
        if err != nil {
            t.Fatalf("expected the hold to expire, got: %v", err)
        }

        if lapsed.Status != booking.StatusExpired || lapsed.HasSeat() {
            t.Fatalf("an expired hold holds no seat, got %s seat %d", lapsed.Status, lapsed.SeatNo)
        }

        if !lapsed.HoldExpiresAt.IsZero() {
            t.Fatalf("an expired hold carries no deadline, got %v", lapsed.HoldExpiresAt)
        }

        // The child is free to start again, because uq_booking_active counts
        // only live bookings and expired is not one.
        mustHold(t, repository, studentOne, classOpen, afterDeadline)
    })

    t.Run("edge: a hold that has not run out yet is left alone", func(t *testing.T) {
        // This is the one mistake an expiry job must never make: taking a seat
        // away from a parent who is still on the payment screen.
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        _, err := repository.Expire(ctx, booking.ExpireRequest{
            BookingID: granted.ID,
            Now:       contractMoment.Add(contractHoldTTL - time.Second),
        })
        if !errors.Is(err, booking.ErrHoldStillLive) {
            t.Fatalf("expected ErrHoldStillLive, got: %v", err)
        }

        unchanged, err := repository.Booking(ctx, granted.ID)
        if err != nil {
            t.Fatalf("expected to read the booking back, got: %v", err)
        }

        if unchanged.Status != booking.StatusPendingPayment {
            t.Fatalf("the hold must still stand, got %s", unchanged.Status)
        }
    })

    t.Run("edge: a booking that already confirmed cannot be expired", func(t *testing.T) {
        // The ordinary case for a job that arrives late: the parent paid while
        // the job was waiting in the queue. Nothing is wrong, and nothing is to
        // be done.
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
        mustConfirm(t, repository, granted.ID, contractMoment)

        _, err := repository.Expire(ctx, booking.ExpireRequest{
            BookingID: granted.ID,
            Now:       contractMoment.Add(contractHoldTTL),
        })
        if !errors.Is(err, booking.ErrNotHolding) {
            t.Fatalf("expected ErrNotHolding, got: %v", err)
        }

        confirmed, err := repository.Booking(ctx, granted.ID)
        if err != nil {
            t.Fatalf("expected to read the booking back, got: %v", err)
        }

        if confirmed.Status != booking.StatusConfirmed || !confirmed.HasSeat() {
            t.Fatalf("the seat must be untouched, got %s seat %d", confirmed.Status, confirmed.SeatNo)
        }
    })

    t.Run("edge: expiring the same hold twice is refused the second time", func(t *testing.T) {
        // Two workers can both hold the same job when a lease lapses mid-run,
        // so the second write has to be refused rather than duplicated.
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
        afterDeadline := contractMoment.Add(contractHoldTTL)

        if _, err := repository.Expire(ctx, booking.ExpireRequest{BookingID: granted.ID, Now: afterDeadline}); err != nil {
            t.Fatalf("expected the first expiry to land, got: %v", err)
        }

        _, err := repository.Expire(ctx, booking.ExpireRequest{BookingID: granted.ID, Now: afterDeadline})
        if !errors.Is(err, booking.ErrNotHolding) {
            t.Fatalf("expected ErrNotHolding, got: %v", err)
        }

        trail, err := repository.Events(ctx, granted.ID)
        if err != nil {
            t.Fatalf("expected the audit trail, got: %v", err)
        }

        // One hold granted, one expiry, and nothing from the second call.
        if len(trail) != 2 {
            t.Fatalf("expected two lines in the trail, got %d", len(trail))
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

// The contract for what phase 6 added: the decline transition and the operator
// worklist. Both are in this suite rather than only in the fake, because a
// status enum value that only one implementation can write is a screen that
// works in tests and never in a browser.
func runDeclineAndWorklistContract(t *testing.T, newFixture func(t *testing.T) repositoryFixture) {
    t.Helper()

    ctx := context.Background()

    t.Run("integration: a declined payment ends the booking and releases the hold", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        declined, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID,
            Reason:    "the provider declined this charge",
            Now:       contractMoment.Add(time.Minute),
        })
        if err != nil {
            t.Fatalf("the decline was refused: %v", err)
        }

        if declined.Status != booking.StatusPaymentFailed {
            t.Fatalf("a declined booking reads %s, wanted payment_failed", declined.Status)
        }

        if declined.HasSeat() {
            t.Fatalf("a declined booking carries seat %d", declined.SeatNo)
        }

        if !declined.HoldExpiresAt.IsZero() {
            t.Fatalf("a declined booking still holds a deadline at %s, so the class stays full", declined.HoldExpiresAt)
        }
    })

    t.Run("behaviour: the decline is written into the audit trail by the payment path", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        if _, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID,
            Reason:    "the provider declined this charge",
            Now:       contractMoment.Add(time.Minute),
        }); err != nil {
            t.Fatalf("the decline was refused: %v", err)
        }

        trail, err := repository.Events(ctx, granted.ID)
        if err != nil {
            t.Fatalf("cannot read the trail: %v", err)
        }

        last := trail[len(trail)-1]

        if last.ToStatus != booking.StatusPaymentFailed {
            t.Fatalf("the last event moves to %s", last.ToStatus)
        }

        if last.Actor != booking.ActorPayment {
            t.Fatalf("the decline was recorded as %s, and a person did not do this", last.Actor)
        }
    })

    t.Run("behaviour: a declined seat is free for the next parent", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        if _, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID, Now: contractMoment.Add(time.Minute),
        }); err != nil {
            t.Fatalf("the decline was refused: %v", err)
        }

        second := mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(2*time.Minute))
        confirmed := mustConfirm(t, repository, second.ID, contractMoment.Add(3*time.Minute))

        if confirmed.SeatNo != 1 {
            t.Fatalf("the next parent got seat %d, so the declined hold was still counted", confirmed.SeatNo)
        }
    })

    t.Run("edge: a booking that already confirmed cannot be declined", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)
        mustConfirm(t, repository, granted.ID, contractMoment.Add(time.Minute))

        _, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID, Now: contractMoment.Add(2 * time.Minute),
        })

        if !errors.Is(err, booking.ErrNotHolding) {
            t.Fatalf("declining a confirmed booking answered %v, wanted ErrNotHolding", err)
        }
    })

    t.Run("edge: declining a booking nobody has answers not found", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        _, err := fixture.Repository().Fail(ctx, booking.FailRequest{
            BookingID: unknownIdentifier, Now: contractMoment,
        })

        if !errors.Is(err, booking.ErrBookingNotFound) {
            t.Fatalf("declining an unknown booking answered %v", err)
        }
    })

    t.Run("integration: the worklist lists every booking, newest first", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        first := mustHold(t, repository, studentOne, classOpen, contractMoment)
        second := mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(time.Minute))

        listed, err := repository.Worklist(ctx, booking.WorklistRequest{Limit: 10})
        if err != nil {
            t.Fatalf("the worklist was refused: %v", err)
        }

        if len(listed) != 2 {
            t.Fatalf("%d bookings were listed, wanted 2", len(listed))
        }

        if listed[0].ID != second.ID || listed[1].ID != first.ID {
            t.Fatalf("the worklist is not newest first: %s then %s", listed[0].ID, listed[1].ID)
        }
    })

    t.Run("behaviour: the worklist narrows to one status", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        holding := mustHold(t, repository, studentOne, classOpen, contractMoment)
        confirmed := mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(time.Minute))
        mustConfirm(t, repository, confirmed.ID, contractMoment.Add(2*time.Minute))

        listed, err := repository.Worklist(ctx, booking.WorklistRequest{
            Status: booking.StatusPendingPayment, Limit: 10,
        })
        if err != nil {
            t.Fatalf("the worklist was refused: %v", err)
        }

        if len(listed) != 1 || listed[0].ID != holding.ID {
            t.Fatalf("the pending_payment filter returned %d rows", len(listed))
        }
    })

    t.Run("edge: the worklist honours its cap, so one screen cannot read the table", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        mustHold(t, repository, studentOne, classOpen, contractMoment)
        mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(time.Minute))

        listed, err := repository.Worklist(ctx, booking.WorklistRequest{Limit: 1})
        if err != nil {
            t.Fatalf("the worklist was refused: %v", err)
        }

        if len(listed) != 1 {
            t.Fatalf("a limit of one returned %d rows", len(listed))
        }
    })

    t.Run("edge: a worklist with no cap is refused rather than reading everything", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        if _, err := fixture.Repository().Worklist(ctx, booking.WorklistRequest{}); !errors.Is(err, booking.ErrInvalidRequest) {
            t.Fatalf("an uncapped worklist answered %v", err)
        }
    })

    t.Run("edge: a status this service never had is refused", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        _, err := fixture.Repository().Worklist(ctx, booking.WorklistRequest{
            Status: booking.Status("refunded"), Limit: 10,
        })

        if !errors.Is(err, booking.ErrInvalidRequest) {
            t.Fatalf("an unknown status answered %v", err)
        }
    })
    t.Run("integration: the live booking behind a duplicate can be named", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        found, err := repository.LiveBooking(ctx, studentOne, classOpen)
        if err != nil {
            t.Fatalf("the live booking could not be found: %v", err)
        }

        if found.ID != granted.ID {
            t.Fatalf("the live booking reads %s, wanted %s", found.ID, granted.ID)
        }
    })

    t.Run("behaviour: a finished booking is not live, so it never blocks a second one", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        if _, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID, Now: contractMoment.Add(time.Minute),
        }); err != nil {
            t.Fatalf("the decline was refused: %v", err)
        }

        _, err := repository.LiveBooking(ctx, studentOne, classOpen)

        if !errors.Is(err, booking.ErrBookingNotFound) {
            t.Fatalf("a declined booking still reads as live: %v", err)
        }
    })

    t.Run("edge: a child who has not booked this class has no live booking", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        _, err := fixture.Repository().LiveBooking(ctx, studentTwo, classOpen)

        if !errors.Is(err, booking.ErrBookingNotFound) {
            t.Fatalf("a child with no booking answered %v", err)
        }
    })
}

// The contract for the parent's own list.
//
// It is in this suite rather than only in the fake because the scoping is the
// whole of the authorisation: the fake reads a map of children and the sql
// reads the students table, and an implementation that got that wrong would
// hand one family another family's bookings while every fast test stayed green.
func runParentBookingsContract(t *testing.T, newFixture func(t *testing.T) repositoryFixture) {
    t.Helper()

    ctx := context.Background()

    t.Run("integration: a parent's own bookings come back newest first", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        first := mustHold(t, repository, studentOne, classOpen, contractMoment)
        second := mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(time.Minute))

        listed, err := repository.ParentBookings(ctx, booking.ParentBookingsRequest{
            ParentID: parentOne, Limit: 10,
        })
        if err != nil {
            t.Fatalf("the list was refused: %v", err)
        }

        if len(listed) != 2 {
            t.Fatalf("%d bookings were listed, wanted 2", len(listed))
        }

        if listed[0].ID != second.ID || listed[1].ID != first.ID {
            t.Fatalf("the list is not newest first: %s then %s", listed[0].ID, listed[1].ID)
        }
    })

    t.Run("behaviour: another parent's booking is never in the list", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        mine := mustHold(t, repository, studentOne, classOpen, contractMoment)
        theirs := mustHold(t, repository, studentThree, classOpen, contractMoment.Add(time.Minute))

        listed, err := repository.ParentBookings(ctx, booking.ParentBookingsRequest{
            ParentID: parentOne, Limit: 10,
        })
        if err != nil {
            t.Fatalf("the list was refused: %v", err)
        }

        if len(listed) != 1 || listed[0].ID != mine.ID {
            t.Fatalf("%d bookings were listed, wanted only %s", len(listed), mine.ID)
        }

        for _, one := range listed {
            if one.ID == theirs.ID {
                t.Fatal("another parent's booking reached this list")
            }
        }
    })

    t.Run("behaviour: a finished booking is still listed, so a parent can see what happened", func(t *testing.T) {
        // The point of this screen is the booking that did not go through. A
        // list of only live bookings would hide the declined payment that the
        // parent is trying to find out about.
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        if _, err := repository.Fail(ctx, booking.FailRequest{
            BookingID: granted.ID, Now: contractMoment.Add(time.Minute),
        }); err != nil {
            t.Fatalf("the decline was refused: %v", err)
        }

        listed, err := repository.ParentBookings(ctx, booking.ParentBookingsRequest{
            ParentID: parentOne, Limit: 10,
        })
        if err != nil {
            t.Fatalf("the list was refused: %v", err)
        }

        if len(listed) != 1 || listed[0].Status != booking.StatusPaymentFailed {
            t.Fatalf("the declined booking is not in the list: %+v", listed)
        }
    })

    t.Run("edge: the list honours its cap", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        mustHold(t, repository, studentOne, classOpen, contractMoment)
        mustHold(t, repository, studentTwo, classOpen, contractMoment.Add(time.Minute))

        listed, err := repository.ParentBookings(ctx, booking.ParentBookingsRequest{
            ParentID: parentOne, Limit: 1,
        })
        if err != nil {
            t.Fatalf("the list was refused: %v", err)
        }

        if len(listed) != 1 {
            t.Fatalf("a limit of one returned %d rows", len(listed))
        }
    })

    t.Run("edge: a parent who has booked nothing gets an empty list rather than a failure", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        listed, err := fixture.Repository().ParentBookings(ctx, booking.ParentBookingsRequest{
            ParentID: parentThree, Limit: 10,
        })
        if err != nil {
            t.Fatalf("a parent with no bookings was refused: %v", err)
        }

        if len(listed) != 0 {
            t.Fatalf("%d bookings were listed for a parent who has none", len(listed))
        }
    })

    t.Run("edge: a list naming nobody is refused rather than reading every parent", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        _, err := fixture.Repository().ParentBookings(ctx, booking.ParentBookingsRequest{Limit: 10})

        if !errors.Is(err, booking.ErrInvalidRequest) {
            t.Fatalf("a list scoped to nobody answered %v", err)
        }
    })

    t.Run("edge: a list with no cap is refused rather than reading everything", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)

        _, err := fixture.Repository().ParentBookings(ctx, booking.ParentBookingsRequest{ParentID: parentOne})

        if !errors.Is(err, booking.ErrInvalidRequest) {
            t.Fatalf("an uncapped list answered %v", err)
        }
    })
}
