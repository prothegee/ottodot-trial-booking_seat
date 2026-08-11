package booking

// Status is where a booking sits in its lifecycle.
//
// The values match the booking_status enum in the migration exactly. A mismatch
// between the two would only be discovered by a write failing, which is a poor
// place to learn about a typo.
type Status string

const (
    // StatusPendingPayment means a hold is standing while the parent pays. It
    // is the only status a booking can be created in.
    StatusPendingPayment Status = "pending_payment"

    // StatusConfirmed means the seat is owned. It is the only status that
    // carries a seat number.
    StatusConfirmed Status = "confirmed"

    // StatusPaymentFailed means the provider declined and no money moved.
    StatusPaymentFailed Status = "payment_failed"

    // StatusRefundRequired means money moved and the seat went to someone
    // else. It is deliberately separate from StatusPaymentFailed: one needs
    // nothing from an operator, the other needs money sent back.
    StatusRefundRequired Status = "refund_required"

    // StatusExpired means the hold ran out before any payment settled.
    StatusExpired Status = "expired"

    // StatusCancelled means the booking was withdrawn, or a refund settled.
    StatusCancelled Status = "cancelled"
)

// allowedTransitions is the whole state machine. A status with an empty set is
// terminal, and saying so explicitly is what lets IsTerminal answer without a
// second list that could disagree with this one.
var allowedTransitions = map[Status]map[Status]struct{}{
    StatusPendingPayment: {
        StatusConfirmed:      {},
        StatusPaymentFailed:  {},
        StatusRefundRequired: {},
        StatusExpired:        {},
        StatusCancelled:      {},
    },
    StatusConfirmed: {
        StatusCancelled: {},
    },
    StatusRefundRequired: {
        StatusCancelled: {},
    },
    StatusPaymentFailed: {},
    StatusExpired:       {},
    StatusCancelled:     {},
}

// IsKnown reports whether this is one of the six statuses the enum defines.
// Anything else came from outside the service and is not to be trusted.
func (status Status) IsKnown() bool {
    _, found := allowedTransitions[status]

    return found
}

// IsTerminal reports whether a booking in this status can still change. A
// terminal booking is finished, in one direction or another.
func (status Status) IsTerminal() bool {
    return status.IsKnown() && len(allowedTransitions[status]) == 0
}

// IsLive reports whether this booking still stands between a child and a second
// booking for the same class.
//
// The two statuses here are exactly the ones in the uq_booking_active index, so
// this function and the database agree by construction. Changing one without
// the other is the bug this pairing is meant to prevent.
func (status Status) IsLive() bool {
    return status == StatusPendingPayment || status == StatusConfirmed
}

// CanTransition reports whether a booking may move from one status to another.
//
// An unknown status on either side is refused rather than assumed harmless.
//
// Param:
// from - Status (where the booking is now)
// to - Status (where it is being moved)
//
// Return:
//   - true when the move is in the table above
//   - false for every other pair, including a status moving to itself
func CanTransition(from Status, to Status) bool {
    destinations, found := allowedTransitions[from]
    if !found {
        return false
    }

    if !to.IsKnown() {
        return false
    }

    _, allowed := destinations[to]

    return allowed
}

// AllStatuses lists the six statuses, so a test can walk every pair without
// repeating the list and drifting from it.
func AllStatuses() []Status {
    return []Status{
        StatusPendingPayment,
        StatusConfirmed,
        StatusPaymentFailed,
        StatusRefundRequired,
        StatusExpired,
        StatusCancelled,
    }
}
