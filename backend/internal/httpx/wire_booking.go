package httpx

import (
    "time"

    "ottodot-trial-booking/backend/internal/booking"
)

// bookingResponse is one booking as the client reads it.
//
// Three fields are pointers so they can be null rather than absent. A booking
// with no seat has no seat number, a booking that is not holding has no
// deadline, and a class that could not be read has no start time, and all three
// are facts the client renders differently from zero. Sending 0 and the zero
// instant would make "no seat" indistinguishable from "seat zero".
type bookingResponse struct {
    ID        string `json:"id"`
    StudentID string `json:"student_id"`
    ClassID   string `json:"class_id"`

    // The class this booking is for, in the words the class list uses. A
    // booking that carried only an identifier left a parent with a reference
    // and a seat number and no way to tell one of their bookings from another.
    ClassSubject  string     `json:"class_subject"`
    ClassTitle    string     `json:"class_title"`
    ClassStartsAt *time.Time `json:"class_starts_at"`

    Status        string     `json:"status"`
    SeatNo        *int16     `json:"seat_no"`
    HoldExpiresAt *time.Time `json:"hold_expires_at"`
}

// bookingFrom shapes a booking for the wire.
//
// Nothing about the parent or the child is added. The client already knows which
// child it asked for, and a booking that carried a name would put one in every
// response body and every proxy log along the way. The class is different: it is
// the same public description the class list already sends to anybody signed in,
// and without it a booking says nothing about what was booked.
//
// Param:
// stored - booking.Booking (the booking)
// class - classNaming (what it was for, empty when that could not be read)
//
// Return:
//   - the wire shape
func bookingFrom(stored booking.Booking, class classNaming) bookingResponse {
    shaped := bookingResponse{
        ID:           stored.ID,
        StudentID:    stored.StudentID,
        ClassID:      stored.ClassID,
        ClassSubject: class.Subject,
        ClassTitle:   class.Title,
        Status:       string(stored.Status),
    }

    if !class.StartsAt.IsZero() {
        startsAt := class.StartsAt
        shaped.ClassStartsAt = &startsAt
    }

    if stored.HasSeat() {
        seat := stored.SeatNo
        shaped.SeatNo = &seat
    }

    if !stored.HoldExpiresAt.IsZero() {
        deadline := stored.HoldExpiresAt
        shaped.HoldExpiresAt = &deadline
    }

    return shaped
}

// bookingsFrom shapes a list.
//
// The slice is built empty rather than left nil, so an operator with nothing to
// look at gets [] and the screen renders an empty list instead of guarding
// against null.
//
// Param:
// stored - []booking.Booking (the bookings)
// classes - map[string]classNaming (what each was for, keyed by class identifier)
//
// Return:
//   - the wire shapes, in the order given
func bookingsFrom(stored []booking.Booking, classes map[string]classNaming) []bookingResponse {
    shaped := make([]bookingResponse, 0, len(stored))

    for _, one := range stored {
        shaped = append(shaped, bookingFrom(one, classes[one.ClassID]))
    }

    return shaped
}
