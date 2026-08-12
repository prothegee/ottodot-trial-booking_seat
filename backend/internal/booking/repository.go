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

    // LiveBooking finds the booking that stands between this child and a second
    // one for this class, if there is one.
    //
    // It exists so a duplicate can be answered with the booking the parent
    // already has rather than only with the word "duplicate". A screen that
    // says "you already booked this" and cannot link to it is a screen that
    // sends the parent looking.
    LiveBooking(ctx context.Context, studentID string, classID string) (Booking, error)

    // SeatsTaken lists the seat numbers currently held in a class, ascending.
    // It is advisory: by the time a caller reads the answer, another
    // transaction may have taken one more.
    SeatsTaken(ctx context.Context, classID string) ([]int16, error)

    // Events reads the audit trail for one booking, oldest first.
    Events(ctx context.Context, bookingID string) ([]Event, error)

    // Worklist lists bookings for an operator, newest first.
    //
    // It is the one read here that answers about many parents at once, which is
    // why it is capped rather than open ended and why the http layer puts it
    // behind the admin role.
    Worklist(ctx context.Context, request WorklistRequest) ([]Booking, error)

    // ParentBookings lists one parent's own bookings, newest first.
    //
    // It is scoped in the query rather than filtered after the read, so a
    // caller cannot be handed a row belonging to somebody else and then be
    // trusted to drop it.
    ParentBookings(ctx context.Context, request ParentBookingsRequest) ([]Booking, error)

    // Hold grants a place on the payment screen, in one transaction.
    Hold(ctx context.Context, request HoldRequest) (Booking, error)

    // Confirm runs the last-seat transaction. It is the only authority on who
    // owns a seat.
    Confirm(ctx context.Context, request ConfirmRequest) (Booking, error)

    // Cancel withdraws a booking and releases any seat it held.
    Cancel(ctx context.Context, request CancelRequest) (Booking, error)

    // Fail ends a booking whose payment was declined, in one transaction.
    //
    // It is separate from Cancel because the two mean different things to
    // whoever reads the row later: nothing was ever charged here, so nothing
    // has to move back, and the audit trail says the payment path caused it
    // rather than a person.
    Fail(ctx context.Context, request FailRequest) (Booking, error)

    // Expire releases a hold whose deadline has passed, in one transaction.
    //
    // It is the worker's method. It refuses a hold that is still standing, so
    // a job that runs early takes nothing away from a parent who is still
    // paying.
    Expire(ctx context.Context, request ExpireRequest) (Booking, error)
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

// ExpireRequest is everything the expiry transaction needs.
type ExpireRequest struct {
    BookingID string

    // Now is both the instant the deadline is judged against and the stamp on
    // the row. Handing it in rather than reading a clock inside the repository
    // is what lets a test place a hold either side of its deadline exactly.
    Now time.Time
}

// FailRequest is everything the decline transaction needs.
type FailRequest struct {
    BookingID string

    // Reason is free text for an operator, taken from what the provider said.
    // It never reaches a parent, so it must still carry no identifier, no name,
    // and no amount.
    Reason string

    Now time.Time
}

// WorklistRequest is what an operator asked to see.
type WorklistRequest struct {
    // Status narrows the list. An empty value means every status, which is the
    // view an operator opens with.
    Status Status

    // Limit caps how many rows come back. It exists so one screen cannot ask
    // for the whole table, and it is required rather than defaulted, because a
    // caller that forgot it would otherwise get an unbounded read.
    Limit int
}

// Validate refuses a worklist that would read the whole table or filter on a
// status this service does not have.
//
// Return:
//   - nil when the request can be served
//   - ErrInvalidRequest otherwise
func (request WorklistRequest) Validate() error {
    if request.Limit < 1 {
        return ErrInvalidRequest
    }

    if request.Status != "" && !request.Status.IsKnown() {
        return ErrInvalidRequest
    }

    return nil
}

// ParentBookingsRequest is what one parent asked to see of their own.
type ParentBookingsRequest struct {
    // ParentID is whose bookings these are. It comes from the token and never
    // from the request body or the query string, because it is the only thing
    // separating one family's bookings from another's.
    ParentID string

    // Limit caps how many rows come back, for the same reason the worklist has
    // one: a read with no ceiling is a read that grows with the table.
    Limit int
}

// Validate refuses a list that names nobody or that would read without a cap.
//
// Return:
//   - nil when the request can be served
//   - ErrInvalidRequest otherwise
func (request ParentBookingsRequest) Validate() error {
    if request.ParentID == "" {
        return ErrInvalidRequest
    }

    if request.Limit < 1 {
        return ErrInvalidRequest
    }

    return nil
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
