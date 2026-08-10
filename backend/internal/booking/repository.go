package booking

import (
	"context"
	"time"
)

// Repository is the only way this package reaches storage.
//
// It exists as an interface for one reason: the four fast test tiers run
// against a fake, and the same behaviour suite runs against real Postgres in
// the proof tier. A fake that quietly disagrees with the sql is the risk the
// shared suite exists to catch.
//
// Every method that changes something is one transaction. The caller never
// composes two calls and hopes they are atomic, because that is precisely how
// the last seat gets sold twice.
type Repository interface {
	// Class reads one class. It reports ErrClassNotFound when there is none.
	Class(ctx context.Context, classID string) (Class, error)

	// Booking reads one booking. It reports ErrBookingNotFound when there is
	// none.
	Booking(ctx context.Context, bookingID string) (Booking, error)

	// SeatsTaken lists the seat numbers currently held in a class, ascending.
	// It is advisory: by the time a caller reads the answer, another
	// transaction may have taken one more.
	SeatsTaken(ctx context.Context, classID string) ([]int16, error)

	// Events reads the audit trail for one booking, oldest first.
	Events(ctx context.Context, bookingID string) ([]Event, error)

	// Hold grants a place on the payment screen, in one transaction.
	Hold(ctx context.Context, request HoldRequest) (Booking, error)

	// Confirm runs the last-seat transaction. It is the only authority on who
	// owns a seat.
	Confirm(ctx context.Context, request ConfirmRequest) (Booking, error)

	// Cancel withdraws a booking and releases any seat it held.
	Cancel(ctx context.Context, request CancelRequest) (Booking, error)
}

// HoldRequest is everything the hold transaction needs.
//
// The instant and the limits are handed in rather than read inside the
// repository, so both implementations answer identically and a test can pin the
// clock instead of sleeping.
type HoldRequest struct {
	// BookingID is minted by the service, so the caller knows the id even if
	// the transaction then fails.
	BookingID string

	// StudentID is the child. The parent is derived from it, never trusted
	// from the request, because that is the value the hold cap is counted on.
	StudentID string

	ClassID string

	// Now is the instant the request is judged at. It decides which holds are
	// still standing.
	Now time.Time

	// ExpiresAt is when this hold will run out.
	ExpiresAt time.Time

	// MaxHoldsPerParent is the cap on holds that are still standing across
	// every class, for the parent this child belongs to.
	MaxHoldsPerParent int
}

// ConfirmRequest is everything the confirm transaction needs.
type ConfirmRequest struct {
	BookingID string

	// Now stamps confirmed_at and updated_at.
	Now time.Time
}

// CancelRequest is everything the cancel transaction needs.
type CancelRequest struct {
	BookingID string

	// Actor is who withdrew it, written into the audit trail.
	Actor Actor

	// Reason is free text for an operator. It never reaches a parent, so it
	// must still carry no identifier and no name.
	Reason string

	Now time.Time
}
