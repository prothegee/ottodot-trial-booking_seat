package booking_test

import (
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
)

// fixedMoment is a stable instant, so a boundary test is exact instead of
// nearly right.
var fixedMoment = time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC)

func TestMaxHoldersIsCapacityPlusAllowance(t *testing.T) {
    t.Run("unit: allowance 2 on a 4 seat class allows 6 holders", func(t *testing.T) {
        class := booking.Class{Capacity: 4, HoldAllowance: 2}

        if got := booking.MaxHolders(class); got != 6 {
            t.Fatalf("expected 6 holders, got %d", got)
        }
    })

    t.Run("edge: allowance 0 means nobody is ever charged for a seat they cannot get", func(t *testing.T) {
        class := booking.Class{Capacity: 4, HoldAllowance: 0}

        if got := booking.MaxHolders(class); got != 4 {
            t.Fatalf("expected 4 holders, got %d", got)
        }
    })

    t.Run("edge: a one seat class with no allowance admits exactly one holder", func(t *testing.T) {
        class := booking.Class{Capacity: 1, HoldAllowance: 0}

        if got := booking.MaxHolders(class); got != 1 {
            t.Fatalf("expected 1 holder, got %d", got)
        }
    })
}

func TestAHoldDeadlineFallsOnTheExpiredSide(t *testing.T) {
    t.Run("unit: a deadline in the future is live", func(t *testing.T) {
        if !booking.HoldIsLive(fixedMoment.Add(time.Second), fixedMoment) {
            t.Fatal("a hold with a second left must still be live")
        }
    })

    t.Run("edge: a deadline exactly now counts as expired, not live", func(t *testing.T) {
        // The boundary has to fall one way. Expiring releases a slot rather
        // than holding one open on a tie, which is the direction that keeps a
        // seat available to somebody.
        if booking.HoldIsLive(fixedMoment, fixedMoment) {
            t.Fatal("a hold whose deadline is exactly now must be expired")
        }
    })

    t.Run("edge: a deadline one nanosecond in the past is expired", func(t *testing.T) {
        if booking.HoldIsLive(fixedMoment.Add(-time.Nanosecond), fixedMoment) {
            t.Fatal("a lapsed hold must not report itself as live")
        }
    })

    t.Run("edge: a booking with no deadline is not holding anything", func(t *testing.T) {
        // A confirmed or finished booking carries no deadline. Treating the
        // zero value as live would count it as a holder forever.
        if booking.HoldIsLive(time.Time{}, fixedMoment) {
            t.Fatal("a zero deadline must never be live")
        }
    })
}

func TestAHoldDeadlineIsStampedFromTheGivenInstant(t *testing.T) {
    t.Run("unit: the deadline is the instant plus the lifetime", func(t *testing.T) {
        deadline := booking.HoldDeadline(fixedMoment, 10*time.Minute)

        if !deadline.Equal(fixedMoment.Add(10 * time.Minute)) {
            t.Fatalf("expected %s, got %s", fixedMoment.Add(10*time.Minute), deadline)
        }
    })

    t.Run("edge: the deadline it stamps is immediately live", func(t *testing.T) {
        deadline := booking.HoldDeadline(fixedMoment, time.Nanosecond)

        if !booking.HoldIsLive(deadline, fixedMoment) {
            t.Fatal("a hold must be live for the whole of its lifetime, however short")
        }
    })
}
