package checkout

import (
    "errors"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/payment"
)

// MetricSink is where this package's numbers are published.
//
// The checkout is the one place in the service that sees a whole attempt from
// the charge to the seat, which is why the transaction metrics are recorded from
// here rather than from booking or payment. Neither of those knows how the other
// half went, and a confirm counted without knowing whether money moved first
// would be a number nobody could act on.
//
// Nil is the ordinary state in a test and never the state in a running service.
type MetricSink interface {
    HoldGranted(outcome string)
    DuplicateRejected()
    BookingConfirmed()
    RaceLost()
    ConfirmTransaction(outcome string, seconds float64)
    PaymentAttempt(outcome string)
}

// The label values this package records under.
//
// They are declared here rather than imported so the checkout does not have to
// know where its numbers end up. The values match the ones the metric layer
// publishes, and the test that reads both is what keeps them the same.
const (
    outcomeGranted   = "granted"
    outcomeRefused   = "refused"
    outcomeConfirmed = "confirmed"
    outcomeSeatLost  = "seat_lost"
    outcomeError     = "error"
    outcomeSettled   = "settled"
    outcomeDeclined  = "declined"
)

// recordHold counts one hold request and what came of it.
func (service *Service) recordHold(err error) {
    if service.metrics == nil {
        return
    }

    if err == nil {
        service.metrics.HoldGranted(outcomeGranted)

        return
    }

    service.metrics.HoldGranted(outcomeRefused)

    if errors.Is(err, booking.ErrAlreadyBooked) {
        service.metrics.DuplicateRejected()
    }
}

// recordPayment counts one charge.
//
// A decline is not an error. A card that was refused worked exactly as designed,
// and folding the two together would make a bad afternoon at a card issuer look
// like this service falling over.
func (service *Service) recordPayment(err error) {
    if service.metrics == nil {
        return
    }

    switch {
    case err == nil:
        service.metrics.PaymentAttempt(outcomeSettled)
    case errors.Is(err, payment.ErrDeclined):
        service.metrics.PaymentAttempt(outcomeDeclined)
    default:
        service.metrics.PaymentAttempt(outcomeError)
    }
}

// recordConfirm counts one seat confirmation and how long it took.
//
// This is the distinction the whole transaction group exists for. A confirm that
// rolls back because another parent took the last seat is correct behaviour, and
// one that rolls back because the transaction broke is not. Counting both as a
// failure would make the healthy case and the broken case the same number.
func (service *Service) recordConfirm(err error, took time.Duration) {
    if service.metrics == nil {
        return
    }

    switch {
    case err == nil:
        service.metrics.ConfirmTransaction(outcomeConfirmed, took.Seconds())
        service.metrics.BookingConfirmed()

    case errors.Is(err, booking.ErrSeatLost):
        service.metrics.ConfirmTransaction(outcomeSeatLost, took.Seconds())
        service.metrics.RaceLost()

    default:
        service.metrics.ConfirmTransaction(outcomeError, took.Seconds())
    }
}
