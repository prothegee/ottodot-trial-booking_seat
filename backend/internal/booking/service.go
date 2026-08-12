package booking

import (
    "context"
    "errors"
    "time"

    "ottodot-trial-booking/backend/internal/identifier"
)

// Settings are the policy values the service applies. They are values rather
// than constants because a class with a different pace, or a load test with a
// shorter countdown, should not need a rebuild.
type Settings struct {
    // HoldTTL is how long a parent has on the payment screen before the hold
    // lapses.
    HoldTTL time.Duration

    // MaxHoldsPerParent caps how many holds one parent may have standing at
    // once, across every class. It is what stops one account from parking
    // every seat in the catalogue.
    MaxHoldsPerParent int

    // Clock is where every stamped time comes from. Nil means the real clock.
    // A test sets it so a deadline is exact rather than approximately right.
    Clock func() time.Time

    // NewBookingID mints the identifier for a new booking. Nil means UUIDv7.
    NewBookingID func() (string, error)
}

// DefaultSettings are the values the service runs with when nothing overrides
// them.
//
// Ten minutes is long enough to find a card and short enough that a seat behind
// someone who walked away is released while the class still matters. Three
// concurrent holds is a parent with three children, all mid-booking.
func DefaultSettings() Settings {
    return Settings{
        HoldTTL:           10 * time.Minute,
        MaxHoldsPerParent: 3,
    }
}

// Service is what the http layer talks to. It owns the policy, the clock, and
// the identifiers. It owns no invariant: every rule that must hold under
// concurrency lives in the repository, inside one transaction.
type Service struct {
    repository Repository
    settings   Settings
}

// HoldCommand is a parent asking for a place on the payment screen.
type HoldCommand struct {
    StudentID string
    ClassID   string
}

// NewService builds the service and fills in whatever the settings left out.
//
// Param:
// repository - Repository (the real one in production, the fake in the fast tiers)
// settings - Settings (policy, and the two injection points a test needs)
//
// Return:
//   - the service, ready to use
//   - an error when a policy value cannot work, refused at construction rather
//     than at the first booking
func NewService(repository Repository, settings Settings) (*Service, error) {
    if repository == nil {
        return nil, errors.New("booking: the service needs a repository")
    }

    if settings.HoldTTL <= 0 {
        return nil, errors.New("booking: the hold lifetime must be greater than zero")
    }

    if settings.MaxHoldsPerParent < 1 {
        return nil, errors.New("booking: a parent must be allowed at least one hold")
    }

    if settings.Clock == nil {
        settings.Clock = time.Now
    }

    if settings.NewBookingID == nil {
        settings.NewBookingID = identifier.NewUUIDv7
    }

    return &Service{repository: repository, settings: settings}, nil
}

// Hold asks for a place on the payment screen for one child in one class.
//
// Return:
//   - the booking in pending_payment, carrying the deadline the parent has
//   - ErrAlreadyBooked, ErrTooManyHolds, ErrClassFull, ErrClassNotFound, or
//     ErrStudentNotFound, each meaning exactly what it says
func (service *Service) Hold(ctx context.Context, command HoldCommand) (Booking, error) {
    if command.StudentID == "" || command.ClassID == "" {
        return Booking{}, ErrInvalidRequest
    }

    bookingID, err := service.settings.NewBookingID()
    if err != nil {
        return Booking{}, err
    }

    now := service.settings.Clock()

    return service.repository.Hold(ctx, HoldRequest{
        BookingID:         bookingID,
        StudentID:         command.StudentID,
        ClassID:           command.ClassID,
        Now:               now,
        ExpiresAt:         HoldDeadline(now, service.settings.HoldTTL),
        MaxHoldsPerParent: service.settings.MaxHoldsPerParent,
    })
}

// Confirm turns a paid hold into an owned seat.
//
// Note:
//   - the payment has already settled by the time this is called. That
//     ordering is why ErrSeatLost leaves the booking in refund_required rather
//     than simply failing: money moved and has to move back.
//
// Return:
//   - the confirmed booking with its seat number
//   - ErrSeatLost when every seat was gone, with the booking now in
//     refund_required
//   - ErrNotHolding when the booking is no longer pending_payment
func (service *Service) Confirm(ctx context.Context, bookingID string) (Booking, error) {
    if bookingID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.Confirm(ctx, ConfirmRequest{
        BookingID: bookingID,
        Now:       service.settings.Clock(),
    })
}

// Cancel withdraws a booking and releases any seat it held.
//
// Param:
// bookingID - string (which booking)
// actor - Actor (who withdrew it, written into the audit trail)
// reason - string (free text for an operator, never shown to a parent, so it
// must still carry no identifier and no name)
//
// Return:
//   - the cancelled booking
//   - ErrInvalidTransition when the booking is already finished
func (service *Service) Cancel(ctx context.Context, bookingID string, actor Actor, reason string) (Booking, error) {
    if bookingID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.Cancel(ctx, CancelRequest{
        BookingID: bookingID,
        Actor:     actor,
        Reason:    reason,
        Now:       service.settings.Clock(),
    })
}

// Fail ends a booking whose payment was declined.
//
// Note:
//   - no money moved, which is the whole reason this is not the refund path. The
//     booking finishes here, the hold is released, and nothing is queued.
//
// Param:
// bookingID - string (which booking)
// reason - string (what the provider said, for an operator. It never reaches a
// parent, so it must still carry no identifier and no name)
//
// Return:
//   - the booking in payment_failed
//   - ErrNotHolding when the booking already moved on, which is the ordinary
//     answer when a hold expired while the provider was thinking
func (service *Service) Fail(ctx context.Context, bookingID string, reason string) (Booking, error) {
    if bookingID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.Fail(ctx, FailRequest{
        BookingID: bookingID,
        Reason:    reason,
        Now:       service.settings.Clock(),
    })
}

// Worklist lists bookings for an operator, newest first.
//
// Note:
//   - this is the one read in this package that crosses parents, so the http
//     layer puts it behind the admin role. Nothing here checks that, because a
//     service that both listed and authorised would be two responsibilities in
//     one method.
//
// Param:
// status - Status (narrow to one status, or empty for every one)
// limit - int (how many rows at most)
//
// Return:
//   - the matching bookings, newest first
//   - ErrInvalidRequest for a limit below one or a status this service has never
//     had
func (service *Service) Worklist(ctx context.Context, status Status, limit int) ([]Booking, error) {
    request := WorklistRequest{Status: status, Limit: limit}

    if err := request.Validate(); err != nil {
        return nil, err
    }

    return service.repository.Worklist(ctx, request)
}

// ParentBookings lists one parent's own bookings, newest first.
//
// Note:
//   - this is the read that lets a parent find a booking again after the screen
//     that made it has gone. Without it the only way back to a booking is the
//     address it was created at, which nobody keeps.
//   - the parent is the whole of the authorisation here, so it has to come from
//     the token. Nothing in this package checks where the caller got it, which
//     is the same split the worklist follows: listing and authorising are two
//     responsibilities.
//
// Param:
// parentID - string (whose bookings, taken from the authenticated identity)
// limit - int (how many rows at most)
//
// Return:
//   - that parent's bookings, newest first, empty when they have none
//   - ErrInvalidRequest for an empty parent or a limit below one
func (service *Service) ParentBookings(ctx context.Context, parentID string, limit int) ([]Booking, error) {
    request := ParentBookingsRequest{ParentID: parentID, Limit: limit}

    if err := request.Validate(); err != nil {
        return nil, err
    }

    return service.repository.ParentBookings(ctx, request)
}

// Expire releases a hold whose deadline has passed.
//
// Note:
//   - this is the worker's method, not a request path. Nothing a parent does
//     reaches it, which is why it takes no actor: the only thing that expires a
//     hold is time, and the audit trail records the system as the cause.
//
// Param:
// bookingID - string (which booking)
//
// Return:
//   - the expired booking, with its deadline cleared
//   - ErrHoldStillLive when the deadline has not passed, so a job that ran
//     early takes nothing away from a parent still on the payment screen
//   - ErrNotHolding when the booking already moved on, which is the ordinary
//     answer for a job that arrives after the parent paid
func (service *Service) Expire(ctx context.Context, bookingID string) (Booking, error) {
    if bookingID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.Expire(ctx, ExpireRequest{
        BookingID: bookingID,
        Now:       service.settings.Clock(),
    })
}

// Booking reads one booking.
func (service *Service) Booking(ctx context.Context, bookingID string) (Booking, error) {
    if bookingID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.Booking(ctx, bookingID)
}

// Events reads the audit trail for one booking, oldest first.
//
// It is what a parent sees when they ask what happened to a booking, and what an
// operator reads when they are asked the same question. Every line in it was
// written inside the transaction that made the change, so the trail cannot
// disagree with the row.
func (service *Service) Events(ctx context.Context, bookingID string) ([]Event, error) {
    if bookingID == "" {
        return nil, ErrInvalidRequest
    }

    return service.repository.Events(ctx, bookingID)
}

// LiveBooking finds the booking that stands between this child and a second one
// for this class.
//
// Note:
//   - this is what turns a refused duplicate into a useful answer. The http
//     layer calls it after ErrAlreadyBooked so the refusal can name the booking
//     the parent already has.
//
// Return:
//   - the live booking
//   - ErrBookingNotFound when there is none, which is the ordinary answer for a
//     child who has not booked this class
func (service *Service) LiveBooking(ctx context.Context, studentID string, classID string) (Booking, error) {
    if studentID == "" || classID == "" {
        return Booking{}, ErrInvalidRequest
    }

    return service.repository.LiveBooking(ctx, studentID, classID)
}

// Class reads one class.
func (service *Service) Class(ctx context.Context, classID string) (Class, error) {
    if classID == "" {
        return Class{}, ErrInvalidRequest
    }

    return service.repository.Class(ctx, classID)
}

// SeatsRemaining is how many seats a class still shows as free.
//
// Note:
//   - this number is advisory and nothing may decide on it. It is what a class
//     list puts on screen to save a parent a wasted click. The only decision is
//     the confirm transaction, which counts again under the lock.
func (service *Service) SeatsRemaining(ctx context.Context, classID string) (int, error) {
    class, err := service.Class(ctx, classID)
    if err != nil {
        return 0, err
    }

    taken, err := service.repository.SeatsTaken(ctx, classID)
    if err != nil {
        return 0, err
    }

    remaining := int(class.Capacity) - len(taken)
    if remaining < 0 {
        return 0, nil
    }

    return remaining, nil
}
