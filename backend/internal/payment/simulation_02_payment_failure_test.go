package payment_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/payment"
)

// Simulation 2: payment failure never reaches the roster.
//
//	parent -> api: pay for booking
//	api    -> provider: charge, seeded to decline
//	provider -> api: declined, no money moved
//	api    -> repository: one payment attempt, status failed, no provider reference
//	api    -> parent: 402 payment_declined
//	note: the roster reads confirmed bookings, and the child is not in one
//
// Asserts: one payment_attempts row with status failed, no seat is assigned,
// and the class roster is empty because the confirm transaction was never
// reached.
//
// The ordering under test is the whole point: payment settles first, and the
// seat is only decided afterwards. A decline stops the sequence before the
// confirm transaction runs, which is why the payment package can prove this
// without importing the booking package. The test wires the two together the
// way the http handler will in phase 6.

const (
    failureParent  = "0192c002-0000-7000-8000-000000000001"
    failureStudent = "0192c002-0000-7000-8000-000000000011"
    failureClass   = "0192c002-0000-7000-8000-000000000021"

    // The seeded failure target is priced to be declined by the amount rule, so
    // a reviewer reproduces this with a number and no external account.
    declinedPriceCents = contractPriceCents + 1
)

func TestSimulation02PaymentFailureNeverReachesTheRoster(t *testing.T) {
    t.Run("behaviour: a declined payment leaves one failed attempt and no seat", func(t *testing.T) {
        ctx := context.Background()

        bookings := booking.NewMemoryRepository()
        bookings.AddStudent(failureStudent, failureParent)
        bookings.AddClass(booking.Class{
            ID: failureClass, Subject: "math", Title: "Math Challenge",
            StartsAt: contractMoment.Add(96 * time.Hour), DurationMinutes: 60,
            Capacity: 4, HoldAllowance: 2,
        })

        bookingSettings := booking.DefaultSettings()
        bookingSettings.Clock = func() time.Time { return contractMoment }

        bookingService, err := booking.NewService(bookings, bookingSettings)
        if err != nil {
            t.Fatalf("expected the booking service to build, got: %v", err)
        }

        held, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: failureStudent, ClassID: failureClass})
        if err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }

        attempts := payment.NewMemoryRepository()
        attempts.AddBooking(held.ID)

        provider := payment.NewMockProvider()

        paymentService, err := payment.NewService(attempts, provider, settingsAt(contractMoment))
        if err != nil {
            t.Fatalf("expected the payment service to build, got: %v", err)
        }

        declined, err := paymentService.Pay(ctx, payCommandFor(held.ID, "key-simulation-2", declinedPriceCents))
        if !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("expected the charge to be declined, got: %v", err)
        }

        if declined.Status != payment.StatusFailed || declined.ProviderRef != "" {
            t.Fatalf("a decline moved no money, so it names no charge: %+v", declined)
        }

        stored, err := paymentService.AttemptsFor(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 1 {
            t.Fatalf("expected exactly one payment attempt, got %d", len(stored))
        }

        // The decline is where the sequence stops. Confirm is never called, so
        // the booking holds no seat and the class shows every seat free.
        unchanged, err := bookingService.Booking(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected to read the booking back, got: %v", err)
        }

        if unchanged.Status == booking.StatusConfirmed || unchanged.HasSeat() {
            t.Fatalf("a declined payment must never own a seat, got %s seat %d", unchanged.Status, unchanged.SeatNo)
        }

        taken, err := bookings.SeatsTaken(ctx, failureClass)
        if err != nil {
            t.Fatalf("expected the taken seats, got: %v", err)
        }

        if len(taken) != 0 {
            t.Fatalf("the roster reads confirmed seats, and there must be none: %v", taken)
        }
    })

    t.Run("behaviour: a settled payment does reach the roster, so the case above proves something", func(t *testing.T) {
        // The mirror of the case above. Without it, a decline leaving no seat
        // could equally mean the wiring never works at all.
        ctx := context.Background()

        bookings := booking.NewMemoryRepository()
        bookings.AddStudent(failureStudent, failureParent)
        bookings.AddClass(booking.Class{
            ID: failureClass, Subject: "math", Title: "Math Challenge",
            StartsAt: contractMoment.Add(96 * time.Hour), DurationMinutes: 60,
            Capacity: 4, HoldAllowance: 2,
        })

        bookingSettings := booking.DefaultSettings()
        bookingSettings.Clock = func() time.Time { return contractMoment }

        bookingService, err := booking.NewService(bookings, bookingSettings)
        if err != nil {
            t.Fatalf("expected the booking service to build, got: %v", err)
        }

        held, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: failureStudent, ClassID: failureClass})
        if err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }

        attempts := payment.NewMemoryRepository()
        attempts.AddBooking(held.ID)

        paymentService, err := payment.NewService(attempts, payment.NewMockProvider(), settingsAt(contractMoment))
        if err != nil {
            t.Fatalf("expected the payment service to build, got: %v", err)
        }

        if _, err := paymentService.Pay(ctx, payCommandFor(held.ID, "key-simulation-2-paid", contractPriceCents)); err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        confirmed, err := bookingService.Confirm(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected the seat to be assigned once the money moved, got: %v", err)
        }

        if confirmed.Status != booking.StatusConfirmed || confirmed.SeatNo != 1 {
            t.Fatalf("expected seat 1 confirmed, got %s seat %d", confirmed.Status, confirmed.SeatNo)
        }
    })
}
