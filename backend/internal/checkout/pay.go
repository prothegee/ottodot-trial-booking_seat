package checkout

import (
    "context"
    "errors"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
)

// PayCommand is a parent settling one booking.
type PayCommand struct {
    BookingID string

    // Amount is what the client said it was paying. It is checked against the
    // price this service owns before it reaches the provider.
    Amount payment.Amount

    // IdempotencyKey comes from the request header. The same key twice is a
    // retry and must produce the first answer again rather than a second charge.
    IdempotencyKey string

    // Development relaxes the amount check to the two values the mock provider
    // reads as a decline and as an unreachable provider. It is false everywhere
    // but a local run.
    Development bool
}

// PayResult is what a checkout produced.
type PayResult struct {
    // Booking is the row as it stands after everything below ran. It is the
    // answer the parent is shown, whichever way it went.
    Booking booking.Booking

    // Attempt is the payment row. It exists even on a decline, which is what
    // makes a declined charge auditable rather than merely absent.
    Attempt payment.Attempt

    // RefundScheduled is whether the reconciliation job was written, and it is
    // only meaningful when the seat was lost.
    RefundScheduled bool
}

// Pay settles the charge and then decides the seat.
//
// This ordering is the single most consequential decision in the service, and it
// is chosen rather than inherited:
//
//	the amount is checked against the price this service owns, so a client
//	cannot name its own charge
//
//	the provider settles first, because a seat handed out before the money moved
//	is a seat that has to be taken back from someone who is looking at it
//
//	the confirm transaction runs second and is the only authority on the seat
//
//	a lost race leaves refund_required and a queued job, because money moved and
//	has to move back, and the job survives a restart while a deferred call does
//	not
//
// The cost of that order is real and is not hidden: a parent can be charged and
// still not get a seat. refund_required exists precisely to make that visible
// and actionable instead of quiet.
//
// Note:
//   - a decline ends the booking. No money moved, so there is nothing to send
//     back, and the hold is released so the seat is free for the next parent.
//   - an unreachable provider changes nothing about the booking. Nobody knows
//     whether money moved, so writing any status would be a guess.
//
// Return:
//   - the confirmed booking and its settled attempt, when everything worked
//   - the booking in payment_failed and payment.ErrDeclined, when the provider
//     said no
//   - the booking in refund_required and booking.ErrSeatLost, when the money
//     settled and every seat was gone
//   - payment.ErrInvalidAmount or payment.ErrInvalidCurrency, refused before
//     anything is charged
func (service *Service) Pay(ctx context.Context, command PayCommand) (PayResult, error) {
    if err := AcceptAmount(command.Amount, command.Development); err != nil {
        return PayResult{}, err
    }

    attempt, err := service.payments.Pay(ctx, payment.PayCommand{
        BookingID:      command.BookingID,
        Amount:         command.Amount.Normalised(),
        IdempotencyKey: command.IdempotencyKey,
    })

    service.recordPayment(err)

    if errors.Is(err, payment.ErrDeclined) {
        return service.endOnDecline(ctx, command.BookingID, attempt)
    }

    if err != nil {
        // Nothing is written about the booking. An unreachable provider, a
        // malformed key, and an unfinished replay all leave the row exactly as
        // it was, because none of them says anything about whether money moved.
        return PayResult{Attempt: attempt}, err
    }

    startedConfirm := service.settings.Clock()

    confirmed, confirmErr := service.bookings.Confirm(ctx, command.BookingID)

    service.recordConfirm(confirmErr, service.settings.Clock().Sub(startedConfirm))

    if errors.Is(confirmErr, booking.ErrSeatLost) {
        return service.queueRefund(ctx, confirmed, attempt)
    }

    if confirmErr != nil {
        return PayResult{Booking: confirmed, Attempt: attempt}, confirmErr
    }

    return PayResult{Booking: confirmed, Attempt: attempt}, nil
}

// endOnDecline finishes a booking whose payment the provider refused.
//
// The decline is reported whatever happens to the transition. The parent's money
// did not move and that is the fact they are being told, so a booking that could
// not be updated is a row for an operator to look at, not a different answer for
// the parent.
func (service *Service) endOnDecline(ctx context.Context, bookingID string, attempt payment.Attempt) (PayResult, error) {
    declined, err := service.bookings.Fail(ctx, bookingID, attempt.FailureReason)
    if err != nil {
        return PayResult{Attempt: attempt}, payment.ErrDeclined
    }

    return PayResult{Booking: declined, Attempt: attempt}, payment.ErrDeclined
}

// queueRefund schedules the money going back for a parent who paid and lost the
// seat.
//
// The job is written after the transaction that rejected the parent, not inside
// it, and that gap is stated rather than hidden: this service enqueues into the
// same database, but through a separate call, so a crash between the two leaves
// a refund_required row with no job. That row is the record, and the operator
// worklist is where it surfaces. Losing the row would be the unrecoverable
// failure, and it cannot happen, because the confirm transaction commits it.
func (service *Service) queueRefund(ctx context.Context, lost booking.Booking, attempt payment.Attempt) (PayResult, error) {
    scheduleErr := service.schedule(ctx, queue.KindReconcileRefund, lost.ID, service.settings.Clock())

    return PayResult{
        Booking:         lost,
        Attempt:         attempt,
        RefundScheduled: scheduleErr == nil,
    }, booking.ErrSeatLost
}
