package checkout_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
)

func TestPayingForASeat(t *testing.T) {
    t.Run("integration: money settles, then the seat is decided", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        result, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID:      granted.Booking.ID,
            Amount:         checkout.TrialPrice(),
            IdempotencyKey: newKey(t),
        })
        if err != nil {
            t.Fatalf("the checkout failed: %v", err)
        }

        if result.Booking.Status != booking.StatusConfirmed {
            t.Fatalf("the booking reads %s after a settled charge", result.Booking.Status)
        }

        if result.Booking.SeatNo != 1 {
            t.Fatalf("the booking took seat %d, wanted the lowest free one", result.Booking.SeatNo)
        }

        if result.Attempt.Status != payment.StatusSucceeded {
            t.Fatalf("the attempt reads %s", result.Attempt.Status)
        }
    })

    t.Run("behaviour: a declined charge ends the booking and moves no money", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        if err := fixture.provider.ForceOutcome(granted.Booking.ID, payment.OutcomeDeclined); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        result, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID:      granted.Booking.ID,
            Amount:         checkout.TrialPrice(),
            IdempotencyKey: newKey(t),
        })

        if !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("a declined charge answered %v", err)
        }

        if result.Booking.Status != booking.StatusPaymentFailed {
            t.Fatalf("a declined booking reads %s, wanted payment_failed", result.Booking.Status)
        }

        if result.Booking.HasSeat() {
            t.Fatalf("a declined booking holds seat %d", result.Booking.SeatNo)
        }
    })

    t.Run("behaviour: a declined hold frees the seat for the next parent", func(t *testing.T) {
        fixture := newStage(t)
        first := fixture.hold(t, studentOne, classOne)

        if err := fixture.provider.ForceOutcome(first.Booking.ID, payment.OutcomeDeclined); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        if _, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID:      first.Booking.ID,
            Amount:         checkout.TrialPrice(),
            IdempotencyKey: newKey(t),
        }); !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("the decline did not happen: %v", err)
        }

        second := fixture.hold(t, studentTwo, classOne)

        result, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID:      second.Booking.ID,
            Amount:         checkout.TrialPrice(),
            IdempotencyKey: newKey(t),
        })
        if err != nil {
            t.Fatalf("the second parent could not take the freed seat: %v", err)
        }

        if result.Booking.SeatNo != 1 {
            t.Fatalf("the second parent took seat %d in a class of one", result.Booking.SeatNo)
        }
    })

    t.Run("behaviour: money that settled with no seat left queues a refund", func(t *testing.T) {
        fixture := newStage(t)

        winner := fixture.hold(t, studentOne, classOne)
        loser := fixture.hold(t, studentTwo, classOne)

        if _, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: winner.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: newKey(t),
        }); err != nil {
            t.Fatalf("the winning checkout failed: %v", err)
        }

        result, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: loser.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: newKey(t),
        })

        if !errors.Is(err, booking.ErrSeatLost) {
            t.Fatalf("the losing checkout answered %v, wanted ErrSeatLost", err)
        }

        if result.Booking.Status != booking.StatusRefundRequired {
            t.Fatalf("the losing booking reads %s", result.Booking.Status)
        }

        if !result.RefundScheduled {
            t.Fatal("no refund was queued for a parent who paid and lost the seat")
        }

        found := false

        for _, job := range fixture.claimed(t) {
            if job.Kind == queue.KindReconcileRefund {
                found = true
            }
        }

        if !found {
            t.Fatal("the queue holds no reconciliation job")
        }
    })

    t.Run("behaviour: a replayed key produces one charge and the same answer", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        key := newKey(t)

        first, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: granted.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: key,
        })
        if err != nil {
            t.Fatalf("the first checkout failed: %v", err)
        }

        charges := fixture.provider.Charges()

        // The second call replays the payment and then finds the booking is no
        // longer holding, which is the honest answer: the seat was already
        // decided by the first call.
        _, err = fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: granted.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: key,
        })

        if !errors.Is(err, booking.ErrNotHolding) {
            t.Fatalf("the replay answered %v", err)
        }

        if fixture.provider.Charges() != charges {
            t.Fatalf("the replay charged again: %d then %d", charges, fixture.provider.Charges())
        }

        if first.Booking.SeatNo != 1 {
            t.Fatalf("the first checkout took seat %d", first.Booking.SeatNo)
        }
    })

    t.Run("edge: an unreachable provider changes nothing about the booking", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        if err := fixture.provider.ForceOutcome(granted.Booking.ID, payment.OutcomeProviderError); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        _, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: granted.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: newKey(t),
        })

        if !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("an unreachable provider answered %v", err)
        }

        stored, err := fixture.bookings.Booking(context.Background(), granted.Booking.ID)
        if err != nil {
            t.Fatalf("cannot read the booking: %v", err)
        }

        if stored.Status != booking.StatusPendingPayment {
            t.Fatalf("the booking reads %s after the provider never answered, which is a guess", stored.Status)
        }
    })

    t.Run("edge: an amount this service does not charge never reaches the provider", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        _, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID:      granted.Booking.ID,
            Amount:         payment.Amount{Cents: 1, Currency: "SGD"},
            IdempotencyKey: newKey(t),
        })

        if !errors.Is(err, payment.ErrInvalidAmount) {
            t.Fatalf("a made up amount answered %v", err)
        }

        if fixture.provider.Charges() != 0 {
            t.Fatalf("%d charges were made for an amount this service does not accept", fixture.provider.Charges())
        }
    })

    t.Run("edge: paying a booking that already finished is refused after the charge replays", func(t *testing.T) {
        fixture := newStage(t)
        granted := fixture.hold(t, studentOne, classOpen)

        if _, err := fixture.bookings.Cancel(context.Background(), booking.CancelRequest{
            BookingID: granted.Booking.ID, Actor: booking.ActorParent, Now: checkoutMoment,
        }); err != nil {
            t.Fatalf("cannot cancel: %v", err)
        }

        _, err := fixture.checkout.Pay(context.Background(), checkout.PayCommand{
            BookingID: granted.Booking.ID, Amount: checkout.TrialPrice(), IdempotencyKey: newKey(t),
        })

        if !errors.Is(err, booking.ErrNotHolding) {
            t.Fatalf("paying a cancelled booking answered %v", err)
        }
    })
}
