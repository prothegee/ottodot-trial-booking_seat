package httpx

import (
    "context"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/catalogue"
)

// ClassLookup is what naming a booking's class needs, and nothing more.
//
// It is an interface rather than the catalogue service itself, so this file
// states the two reads it makes and a test can answer them without a database.
// Neither read decides anything.
type ClassLookup interface {
    Class(ctx context.Context, classID string) (catalogue.Class, error)
    Classes(ctx context.Context) ([]catalogue.Class, error)
}

// classNaming is what a booking carries about the class it is for.
//
// The public description and nothing else. Capacity and seats remaining are
// deliberately absent: both move, and a booking is a record of what happened
// rather than a second place to read a count from.
type classNaming struct {
    Subject  string
    Title    string
    StartsAt time.Time
}

// ClassNames answers what a booking was for.
//
// A booking on the wire is an identifier, a status, and a seat number, and none
// of the three says what was booked. A parent looking at their own list had a
// reference and a seat number with no way to tell one booking from another.
type ClassNames struct {
    classes ClassLookup
}

// NewClassNames wires the lookup.
//
// Param:
// classes - ClassLookup (where a class description is read from)
//
// Return:
//   - the resolver
//   - catalogue.ErrInvalidRequest when there is nothing to read from
func NewClassNames(classes ClassLookup) (*ClassNames, error) {
    if classes == nil {
        return nil, catalogue.ErrInvalidRequest
    }

    return &ClassNames{classes: classes}, nil
}

// For names the class one booking is for.
//
// Note:
//   - a lookup that fails is not an error for the caller. The booking is what
//     was asked for, and refusing the whole read because a description was
//     unavailable would hide a confirmed seat behind a query that decides
//     nothing. What comes back is empty, and the client leaves the line out.
//
// Param:
// stored - booking.Booking (the booking being shaped)
//
// Return:
//   - the class description
//   - the empty description when it could not be read
func (names *ClassNames) For(ctx context.Context, stored booking.Booking) classNaming {
    class, err := names.classes.Class(ctx, stored.ClassID)
    if err != nil {
        return classNaming{}
    }

    return namingOf(class)
}

// ForAll names every class a list of bookings is for, in one read.
//
// It reads the whole catalogue rather than one class per booking. The catalogue
// is a handful of rows and a parent's list is capped at fifty, so the one read
// is both fewer round trips and a bound that does not grow with the list.
//
// Param:
// stored - []booking.Booking (the bookings being shaped)
//
// Return:
//   - the description of each class named, keyed by class identifier
//   - an empty map when there was nothing to name, or nothing could be read
func (names *ClassNames) ForAll(ctx context.Context, stored []booking.Booking) map[string]classNaming {
    named := make(map[string]classNaming, len(stored))

    if len(stored) == 0 {
        return named
    }

    listed, err := names.classes.Classes(ctx)
    if err != nil {
        return named
    }

    wanted := make(map[string]struct{}, len(stored))

    for _, one := range stored {
        wanted[one.ClassID] = struct{}{}
    }

    for _, class := range listed {
        if _, asked := wanted[class.ID]; asked {
            named[class.ID] = namingOf(class)
        }
    }

    return named
}

// namingOf keeps the copying in one place, so the list path and the single read
// path can never disagree about which fields a booking carries.
func namingOf(class catalogue.Class) classNaming {
    return classNaming{
        Subject:  class.Subject,
        Title:    class.Title,
        StartsAt: class.StartsAt,
    }
}
