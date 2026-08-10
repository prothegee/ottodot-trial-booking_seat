package booking_test

import (
	"testing"

	"ottodot-trial-booking/backend/internal/booking"
)

// allowedPairs is the state machine written out by hand.
//
// It is deliberately a second copy rather than a read of the package's own
// table. A test that asks the implementation what it allows and then agrees
// with the answer proves nothing. This list is what the diagram in the plan
// says, so a change to the machine has to be made twice on purpose.
var allowedPairs = map[booking.Status][]booking.Status{
	booking.StatusPendingPayment: {
		booking.StatusConfirmed,
		booking.StatusPaymentFailed,
		booking.StatusRefundRequired,
		booking.StatusExpired,
		booking.StatusCancelled,
	},
	booking.StatusConfirmed: {
		booking.StatusCancelled,
	},
	booking.StatusRefundRequired: {
		booking.StatusCancelled,
	},
}

func isAllowed(from booking.Status, to booking.Status) bool {
	for _, destination := range allowedPairs[from] {
		if destination == to {
			return true
		}
	}

	return false
}

func TestEveryAllowedTransitionIsAccepted(t *testing.T) {
	t.Run("unit: each pair in the state machine is allowed", func(t *testing.T) {
		for from, destinations := range allowedPairs {
			for _, to := range destinations {
				if !booking.CanTransition(from, to) {
					t.Errorf("expected %s to %s to be allowed", from, to)
				}
			}
		}
	})
}

func TestEveryDisallowedTransitionIsRefused(t *testing.T) {
	t.Run("edge: every pair outside the state machine is refused", func(t *testing.T) {
		for _, from := range booking.AllStatuses() {
			for _, to := range booking.AllStatuses() {
				if isAllowed(from, to) {
					continue
				}

				if booking.CanTransition(from, to) {
					t.Errorf("expected %s to %s to be refused", from, to)
				}
			}
		}
	})

	t.Run("edge: a confirmed booking cannot go back to pending payment", func(t *testing.T) {
		// Called out on its own because it is the one that would quietly
		// un-sell a seat. A confirmed booking holds a seat number, and moving
		// it back to pending_payment would leave that seat owned by a booking
		// nobody is watching.
		if booking.CanTransition(booking.StatusConfirmed, booking.StatusPendingPayment) {
			t.Fatal("a confirmed booking must never return to pending_payment")
		}
	})

	t.Run("edge: a status cannot transition to itself", func(t *testing.T) {
		for _, status := range booking.AllStatuses() {
			if booking.CanTransition(status, status) {
				t.Errorf("expected %s to itself to be refused", status)
			}
		}
	})

	t.Run("edge: an unknown status is refused on either side", func(t *testing.T) {
		unknown := booking.Status("half_booked")

		if booking.CanTransition(unknown, booking.StatusCancelled) {
			t.Fatal("an unknown status must not be a valid starting point")
		}

		if booking.CanTransition(booking.StatusPendingPayment, unknown) {
			t.Fatal("an unknown status must not be a valid destination")
		}

		if unknown.IsKnown() {
			t.Fatal("an invented status must not report itself as known")
		}
	})
}

func TestTerminalStatusesHaveNoWayOut(t *testing.T) {
	t.Run("unit: the three finished statuses are terminal", func(t *testing.T) {
		terminal := []booking.Status{
			booking.StatusPaymentFailed,
			booking.StatusExpired,
			booking.StatusCancelled,
		}

		for _, status := range terminal {
			if !status.IsTerminal() {
				t.Errorf("expected %s to be terminal", status)
			}
		}
	})

	t.Run("unit: the three unfinished statuses are not terminal", func(t *testing.T) {
		unfinished := []booking.Status{
			booking.StatusPendingPayment,
			booking.StatusConfirmed,
			booking.StatusRefundRequired,
		}

		for _, status := range unfinished {
			if status.IsTerminal() {
				t.Errorf("expected %s to still have somewhere to go", status)
			}
		}
	})

	t.Run("edge: an unknown status is not terminal, it is not a status at all", func(t *testing.T) {
		if booking.Status("").IsTerminal() {
			t.Fatal("the empty status must not report itself as terminal")
		}
	})
}

func TestLiveMatchesTheDatabaseIndex(t *testing.T) {
	t.Run("unit: pending payment and confirmed are the live pair", func(t *testing.T) {
		// These two statuses are exactly the ones in uq_booking_active. If this
		// ever disagrees with the migration, a duplicate booking is refused by
		// the database with a driver error instead of by the service with a
		// reason a parent can read.
		if !booking.StatusPendingPayment.IsLive() || !booking.StatusConfirmed.IsLive() {
			t.Fatal("pending_payment and confirmed must both count as live")
		}
	})

	t.Run("edge: every finished status is not live", func(t *testing.T) {
		finished := []booking.Status{
			booking.StatusPaymentFailed,
			booking.StatusRefundRequired,
			booking.StatusExpired,
			booking.StatusCancelled,
		}

		for _, status := range finished {
			if status.IsLive() {
				t.Errorf("expected %s not to block a second booking", status)
			}
		}
	})
}
