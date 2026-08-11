package worker

import (
    "context"
    "errors"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/queue"
)

// HoldExpirer is the one thing this handler needs from the booking side.
//
// It is declared here, and it is one method wide, so the runner can be tested
// against something four lines long. Depending on the whole booking service
// would mean a test of the queue needed a class, a student, and a seat.
type HoldExpirer interface {
    Expire(ctx context.Context, bookingID string) (booking.Booking, error)
}

// ExpireHoldHandler releases holds whose deadline has passed.
//
// This is the job that makes an abandoned payment screen cost the class one
// seat for ten minutes instead of forever.
type ExpireHoldHandler struct {
    bookings HoldExpirer
}

// NewExpireHoldHandler wires the handler to the booking service.
//
// Return:
//   - the handler, ready to register
//   - ErrHandlerMissing when there is nothing for it to call
func NewExpireHoldHandler(bookings HoldExpirer) (*ExpireHoldHandler, error) {
    if bookings == nil {
        return nil, ErrHandlerMissing
    }

    return &ExpireHoldHandler{bookings: bookings}, nil
}

// Handle expires one hold.
//
// Three answers arrive from the booking side and each means something
// different, which is the whole of this method:
//
//	nil               the hold was released and the seat is free again
//	ErrNotHolding     the booking already moved on, so the job did its job by
//	                  arriving too late. The parent paid, or cancelled, or
//	                  another worker got there first
//	ErrHoldStillLive  the deadline has not passed. Taking the seat now would
//	                  take it from somebody who is still paying, so the job
//	                  goes back and waits
//
// Anything else is unexpected, and unexpected work is retried rather than
// thrown away.
//
// Return:
//   - nil when there is nothing left to do for this booking
//   - the failure otherwise, which hands the job back for another attempt
func (handler *ExpireHoldHandler) Handle(ctx context.Context, job queue.Job) error {
    payload, err := queue.DecodeBookingPayload(job.Payload)
    if err != nil {
        return err
    }

    _, err = handler.bookings.Expire(ctx, payload.BookingID)

    if errors.Is(err, booking.ErrNotHolding) {
        return nil
    }

    return err
}
