package worker

import (
	"context"

	"ottodot-trial-booking/backend/internal/booking"
	"ottodot-trial-booking/backend/internal/payment"
	"ottodot-trial-booking/backend/internal/queue"
)

// refundReason is the line written into the audit trail when a refund settles.
//
// It names what happened and nothing else. The refund's own reference is handed
// to the recorder instead of being written here, because this column is free
// text an operator reads and the rule for it is that it carries no identifier.
const refundReason = "refund settled, booking closed by the worker"

// BookingCloser is what this handler needs from the booking side: read the
// current state, and close the booking once the money has gone back.
type BookingCloser interface {
	Booking(ctx context.Context, bookingID string) (booking.Booking, error)
	Cancel(ctx context.Context, bookingID string, actor booking.Actor, reason string) (booking.Booking, error)
}

// RefundReconciler is what it needs from the payment side.
type RefundReconciler interface {
	Refund(ctx context.Context, command payment.RefundCommand) (payment.Refund, error)
}

// ReconcileRefundHandler sends money back to a parent who paid and lost the
// seat, then closes the booking.
//
// It is the other half of the ordering this whole service is built on. Payment
// settles first and the seat is decided second, so a parent can be charged for
// a seat that is gone by the time their turn comes. `refund_required` is the
// record of that, and this handler is what clears it.
type ReconcileRefundHandler struct {
	bookings BookingCloser
	payments RefundReconciler
	onRefund func(refund payment.Refund)
}

// NewReconcileRefundHandler wires the handler to both services.
//
// Param:
// bookings - BookingCloser (reads the booking, and closes it afterwards)
// payments - RefundReconciler (sends the settled charge back)
// onRefund - func (told about every refund that settled, so the reference is
// written down somewhere. Nil means nowhere, which only a test wants)
//
// Return:
//   - the handler, ready to register
//   - ErrHandlerMissing when either service is absent
func NewReconcileRefundHandler(bookings BookingCloser, payments RefundReconciler, onRefund func(refund payment.Refund)) (*ReconcileRefundHandler, error) {
	if bookings == nil || payments == nil {
		return nil, ErrHandlerMissing
	}

	return &ReconcileRefundHandler{bookings: bookings, payments: payments, onRefund: onRefund}, nil
}

// Handle refunds one booking and closes it.
//
// The order is refund first, close second, and it is not interchangeable. A
// booking closed before the money moved would look settled while the parent is
// still out of pocket, and nothing would ever come back to fix it.
//
// That order is also what makes a replay safe. The status is read first, and a
// booking that is no longer in `refund_required` has already been through here,
// so the provider is never asked twice. The payment service cannot guard this
// on its own, because there is no refund row for it to look at.
//
// Return:
//   - nil when the booking is closed, or was already closed by an earlier run
//   - the failure otherwise, which hands the job back for another attempt. A
//     provider that could not be reached lands here, which is the only safe
//     answer when nobody knows whether the money moved
func (handler *ReconcileRefundHandler) Handle(ctx context.Context, job queue.Job) error {
	payload, err := queue.DecodeBookingPayload(job.Payload)
	if err != nil {
		return err
	}

	held, err := handler.bookings.Booking(ctx, payload.BookingID)
	if err != nil {
		return err
	}

	if held.Status != booking.StatusRefundRequired {
		return nil
	}

	// ErrNothingToRefund lands here with everything else, and it is worth being
	// clear about why. It means the booking says money moved and the attempts
	// say it did not. Closing the booking would erase that disagreement, so the
	// job is handed back instead and eventually parks, where somebody has to
	// look at it.
	sentBack, err := handler.payments.Refund(ctx, payment.RefundCommand{BookingID: payload.BookingID})
	if err != nil {
		return err
	}

	handler.record(sentBack)

	// The money is back and the booking still says it is owed. A retry comes
	// straight back to the refund above, which is exactly the case the refund
	// key exists for: the provider recognises it and moves nothing a second
	// time.
	if _, err := handler.bookings.Cancel(ctx, payload.BookingID, booking.ActorPayment, refundReason); err != nil {
		return err
	}

	return nil
}

// record hands one settled refund to whoever is keeping the reference.
func (handler *ReconcileRefundHandler) record(refund payment.Refund) {
	if handler.onRefund == nil {
		return
	}

	handler.onRefund(refund)
}
