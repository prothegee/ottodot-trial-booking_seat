package booking_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/faults"
)

// oneShot is a fault source that fires once for the named point, which is the
// default arming and the one a demonstration uses.
func oneShot(point string) booking.Fault {
    spent := false

    return func(reached string) bool {
        if spent || reached != point {
            return false
        }

        spent = true

        return true
    }
}

func TestConfirmUnderInjectedFaults(t *testing.T) {
    t.Run("integration: a broken commit leaves the seat free and the booking holding", func(t *testing.T) {
        // This is the property the whole design rests on. The transaction is
        // all or nothing, so a failure partway must leave no trace at all: no
        // seat consumed, no status moved, and nothing recorded.
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        fixture.(*memoryFixture).repository.InjectFaults(oneShot(faults.PointConfirmBeforeCommit))

        _, err := repository.Confirm(context.Background(), booking.ConfirmRequest{
            BookingID: granted.ID,
            Now:       contractMoment,
        })

        if !errors.Is(err, booking.ErrTransactionBroken) {
            t.Fatalf("the broken confirm answered %v", err)
        }

        after, readErr := repository.Booking(context.Background(), granted.ID)
        if readErr != nil {
            t.Fatalf("the booking could not be read back: %v", readErr)
        }

        if after.Status != booking.StatusPendingPayment {
            t.Errorf("the booking is %s, so the rollback did not undo the status change", after.Status)
        }

        if after.SeatNo != 0 {
            t.Errorf("seat %d is still assigned, so the rollback did not release it", after.SeatNo)
        }
    })

    t.Run("behaviour: a booking survives a broken confirm and is confirmed on the retry", func(t *testing.T) {
        // The one that matters most. A failed confirm has to leave the booking
        // retryable rather than stuck, because the parent has already paid by
        // the time this transaction runs.
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        fixture.(*memoryFixture).repository.InjectFaults(oneShot(faults.PointConfirmBeforeCommit))

        if _, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment}); err == nil {
            t.Fatal("the armed confirm succeeded")
        }

        confirmed, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment})
        if err != nil {
            t.Fatalf("the retry answered %v, so the booking was left stuck", err)
        }

        if confirmed.Status != booking.StatusConfirmed || confirmed.SeatNo != 1 {
            t.Fatalf("the retry produced %s on seat %d", confirmed.Status, confirmed.SeatNo)
        }
    })

    t.Run("edge: a broken confirm is not a lost seat, so no refund is marked", func(t *testing.T) {
        // The distinction the whole transaction metric group is built on. A seat
        // genuinely taken by somebody else moves the booking to refund_required
        // because money has to go back. A transaction that broke took nothing
        // from anybody, so marking a refund would be wrong twice over.
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        fixture.(*memoryFixture).repository.InjectFaults(oneShot(faults.PointConfirmBeforeCommit))

        _, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment})

        if errors.Is(err, booking.ErrSeatLost) {
            t.Fatal("a broken transaction was reported as a lost race")
        }

        after, _ := repository.Booking(context.Background(), granted.ID)

        if after.Status == booking.StatusRefundRequired {
            t.Fatal("a broken transaction marked a refund, so an operator would be told to move money that never moved")
        }
    })

    t.Run("edge: a lock wait timeout is refused before anything is read", func(t *testing.T) {
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        fixture.(*memoryFixture).repository.InjectFaults(oneShot(faults.PointConfirmLockWait))

        _, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment})

        if !errors.Is(err, booking.ErrLockWaitTimeout) {
            t.Fatalf("the armed lock wait answered %v", err)
        }

        after, _ := repository.Booking(context.Background(), granted.ID)

        if after.Status != booking.StatusPendingPayment {
            t.Fatalf("the booking is %s after a lock that was never taken", after.Status)
        }
    })

    t.Run("unit: a repository with no fault source behaves exactly as before", func(t *testing.T) {
        // Nil is the ordinary state, and every caller outside a demonstration is
        // in it. A call site that only works when a fault source is present
        // would be a fault surface that is always on.
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        confirmed, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment})
        if err != nil {
            t.Fatalf("an unarmed confirm answered %v", err)
        }

        if confirmed.Status != booking.StatusConfirmed {
            t.Fatalf("an unarmed confirm produced %s", confirmed.Status)
        }
    })

    t.Run("unit: an armed point that is never reached changes nothing", func(t *testing.T) {
        fixture := newMemoryFixture(t)
        seedContractFixture(t, fixture)

        repository := fixture.Repository()
        granted := mustHold(t, repository, studentOne, classOpen, contractMoment)

        // A point this repository has no call site for.
        fixture.(*memoryFixture).repository.InjectFaults(oneShot(faults.PointPaymentProviderError))

        if _, err := repository.Confirm(context.Background(), booking.ConfirmRequest{BookingID: granted.ID, Now: contractMoment}); err != nil {
            t.Fatalf("a confirm answered %v while an unrelated point was armed", err)
        }
    })
}
