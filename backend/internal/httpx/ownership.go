package httpx

import (
    "context"
    "net/http"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/observability"
)

// Owner decides whether the identity behind a request may act on a student or a
// booking.
//
// It is a type of its own rather than a helper on each handler for one reason:
// this is the check that stops one parent touching another parent's child, and a
// check copied into five places is a check that is missing from the sixth.
type Owner struct {
    directory auth.Directory
    bookings  *booking.Service
    counters  *Counters
}

// NewOwner wires the check.
//
// Param:
// directory - auth.Directory (who has which children)
// bookings - *booking.Service (which child a booking is for)
// counters - *Counters (where a refusal is recorded, nil for nowhere)
//
// Return:
//   - the check
//   - ErrNotYourChild when a collaborator is missing, refused here rather than
//     as a route that lets everything through
func NewOwner(directory auth.Directory, bookings *booking.Service, counters *Counters) (*Owner, error) {
    if directory == nil || bookings == nil {
        return nil, ErrNotYourChild
    }

    return &Owner{directory: directory, bookings: bookings, counters: counters}, nil
}

// Student refuses a request naming a child who is not on this account.
//
// The account is read on every check rather than cached with the token. A child
// list can change, and a stale copy here would either let a removed child be
// booked or refuse one that was just added.
//
// Return:
//   - nil when the child is on this account
//   - ErrNotYourChild otherwise, including when the account cannot be read,
//     because an ownership check that cannot answer must not answer yes
func (owner *Owner) Student(ctx context.Context, identity auth.Identity, studentID string) error {
    if studentID == "" {
        return booking.ErrInvalidRequest
    }

    account, err := owner.directory.Account(ctx, identity.ParentID)
    if err != nil {
        return owner.refuse()
    }

    for _, child := range account.Children {
        if child.ID == studentID {
            return nil
        }
    }

    return owner.refuse()
}

// Booking reads a booking and refuses one that belongs to somebody else.
//
// It reads the booking first and checks second, and hands the row back, so a
// caller does not read it twice. An admin does not pass through here: these are
// the parent facing routes, and an operator looking at somebody's booking uses
// the admin worklist, which is a different route with a different audience.
//
// Note:
//   - a booking that does not exist and a booking that is not yours produce the
//     same answer to the caller, by the time FailureFor is done with them. That
//     is deliberate: an api that tells them apart is an api that can be asked
//     which identifiers exist.
//
// Return:
//   - the booking when it belongs to this identity
//   - booking.ErrBookingNotFound when there is no such booking
//   - ErrNotYourChild when it belongs to another account
func (owner *Owner) Booking(ctx context.Context, identity auth.Identity, bookingID string) (booking.Booking, error) {
    stored, err := owner.bookings.Booking(ctx, bookingID)
    if err != nil {
        return booking.Booking{}, err
    }

    if err := owner.Student(ctx, identity, stored.StudentID); err != nil {
        return booking.Booking{}, err
    }

    return stored, nil
}

// refuse records the denial and hands the reason back, so the count and the
// answer can never disagree.
func (owner *Owner) refuse() error {
    if owner.counters != nil {
        owner.counters.AccessDenied(observability.ReasonNotYourChild)
    }

    return ErrNotYourChild
}

// identityOf reads the identity a request was authenticated with, or refuses it.
//
// Every business handler starts with this. A handler reached without an identity
// is a wiring mistake rather than an anonymous caller, and answering
// token_invalid is both truthful and the safest thing to do about it.
func identityOf(response http.ResponseWriter, request *http.Request) (auth.Identity, bool) {
    identity, carried := auth.IdentityFrom(request.Context())
    if !carried {
        Deny(response, request, auth.ErrNotAuthenticated)

        return auth.Identity{}, false
    }

    return identity, true
}
