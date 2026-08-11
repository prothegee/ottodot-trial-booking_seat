package httpx

import (
    "time"

    "ottodot-trial-booking/backend/internal/booking"
)

// bookingResponse is one booking as the client reads it.
//
// Two fields are pointers so they can be null rather than absent. A booking with
// no seat has no seat number, and a booking that is not holding has no deadline,
// and both are facts the client renders differently from zero. Sending 0 and the
// zero instant would make "no seat" indistinguishable from "seat zero".
type bookingResponse struct {
    ID            string     `json:"id"`
    StudentID     string     `json:"student_id"`
    ClassID       string     `json:"class_id"`
    Status        string     `json:"status"`
    SeatNo        *int16     `json:"seat_no"`
    HoldExpiresAt *time.Time `json:"hold_expires_at"`
}

// bookingFrom shapes a booking for the wire.
//
// Nothing about the parent, the child, or the class is added. The client already
// knows which child it asked for, and a booking that carried a name would put
// one in every response body and every proxy log along the way.
func bookingFrom(stored booking.Booking) bookingResponse {
    shaped := bookingResponse{
        ID:        stored.ID,
        StudentID: stored.StudentID,
        ClassID:   stored.ClassID,
        Status:    string(stored.Status),
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
func bookingsFrom(stored []booking.Booking) []bookingResponse {
    shaped := make([]bookingResponse, 0, len(stored))

    for _, one := range stored {
        shaped = append(shaped, bookingFrom(one))
    }

    return shaped
}
