package queue

import (
    "encoding/json"
    "errors"
)

// BookingPayload is what both job kinds carry today: which booking the work is
// about, and nothing else.
//
// Nothing else is deliberate. A payload written now is read by a worker running
// a different build later, so anything copied into it can be stale by the time
// it matters. The booking id is the one value that cannot go out of date, and
// the handler reads the current row for everything else.
type BookingPayload struct {
    BookingID string `json:"booking_id"`
}

// EncodeBookingPayload turns the payload into the bytes the column stores.
//
// Param:
// bookingID - string (which booking the job is about)
//
// Return:
//   - the json object, ready to hand to Enqueue
//   - ErrInvalidRequest when the identifier is empty, refused here rather than
//     leaving a job nobody can act on in the table
func EncodeBookingPayload(bookingID string) ([]byte, error) {
    if bookingID == "" {
        return nil, ErrInvalidRequest
    }

    return json.Marshal(BookingPayload{BookingID: bookingID})
}

// DecodeBookingPayload reads a payload back.
//
// Return:
//   - the payload, with a non-empty booking id
//   - ErrInvalidPayload when the bytes are not that object, or the identifier
//     inside is empty
func DecodeBookingPayload(payload []byte) (BookingPayload, error) {
    var decoded BookingPayload

    if err := json.Unmarshal(payload, &decoded); err != nil {
        return BookingPayload{}, errors.Join(ErrInvalidPayload, err)
    }

    if decoded.BookingID == "" {
        return BookingPayload{}, ErrInvalidPayload
    }

    return decoded, nil
}

// validPayload reports whether these bytes are a json object.
//
// The column is jsonb, so anything else is a write that fails at the database
// with a message about syntax. Checking the shape here is what turns that into
// ErrInvalidPayload before either implementation is reached, which is also what
// lets the fake refuse exactly what the sql refuses.
func validPayload(payload []byte) bool {
    if len(payload) == 0 {
        return false
    }

    var object map[string]json.RawMessage

    return json.Unmarshal(payload, &object) == nil
}
