package observability

import (
    "io"
    "log/slog"
)

// The field names every line in this service uses.
//
// They are constants so one state change reads the same wherever it was written
// from. A log that calls the same thing `booking_id` in one place and `booking`
// in another cannot be searched by anybody who was not there when it was
// written.
//
// Note:
//   - identifiers only. There is no field here for a parent's name, a child's
//     name, or an email address, and that absence is the design. Redaction
//     catches an address that arrives inside a message, and nothing can catch a
//     name, so the only defence for a name is that no field exists to put it in.
const (
    FieldRequestID = "request_id"
    FieldBookingID = "booking_id"
    FieldStudentID = "student_id"
    FieldClassID   = "class_id"
    FieldParentID  = "parent_id"
    FieldFrom      = "from"
    FieldTo        = "to"
    FieldReason    = "reason"
    FieldPoint     = "point"
    FieldOutcome   = "outcome"
    FieldSeatNo    = "seat_no"
)

// Logger is the structured log this service writes.
//
// It is a thin type over slog rather than an interface, because there is exactly
// one implementation and there is no test in this project that needs a different
// one: a test reads the json the logger wrote, which is the same thing an
// operator does.
type Logger struct {
    inner *slog.Logger
}

// NewLogger builds a logger that writes json through the redaction rules.
//
// Param:
// writer - io.Writer (where the lines go, os.Stdout in a running service)
// level - slog.Level (the lowest level written)
//
// Return:
//   - the logger, ready to use
func NewLogger(writer io.Writer, level slog.Level) *Logger {
    handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})

    return &Logger{inner: slog.New(redactingHandler{inner: handler})}
}

// StateChange is one booking moving from one status to another.
//
// It is a struct rather than a list of arguments so a caller cannot swap two
// identifiers and produce a line that reads correctly and says the wrong thing.
type StateChange struct {
    RequestID string
    BookingID string
    StudentID string
    ClassID   string
    From      string
    To        string
    Reason    string
    SeatNo    int
}

// BookingStateChanged writes one line for a booking that moved.
//
// Every state change in this service goes through here, which is what makes the
// log answer the question it exists for: given a request id from a failed
// response, what happened to that booking and in what order.
//
// Param:
// change - StateChange (which booking moved, from what to what, and why)
func (logger *Logger) BookingStateChanged(change StateChange) {
    if logger == nil {
        return
    }

    attributes := []any{
        slog.String(FieldRequestID, change.RequestID),
        slog.String(FieldBookingID, change.BookingID),
        slog.String(FieldFrom, change.From),
        slog.String(FieldTo, change.To),
    }

    if change.StudentID != "" {
        attributes = append(attributes, slog.String(FieldStudentID, change.StudentID))
    }

    if change.ClassID != "" {
        attributes = append(attributes, slog.String(FieldClassID, change.ClassID))
    }

    if change.Reason != "" {
        attributes = append(attributes, slog.String(FieldReason, change.Reason))
    }

    if change.SeatNo > 0 {
        attributes = append(attributes, slog.Int(FieldSeatNo, change.SeatNo))
    }

    logger.inner.Info("booking state changed", attributes...)
}

// Info writes one ordinary line.
func (logger *Logger) Info(message string, fields ...any) {
    if logger == nil {
        return
    }

    logger.inner.Info(message, fields...)
}

// Warn writes one line that a person should eventually read. Arming a fault and
// triggering one both land here, because a stack with a fault armed is not a
// stack anybody should read as healthy.
func (logger *Logger) Warn(message string, fields ...any) {
    if logger == nil {
        return
    }

    logger.inner.Warn(message, fields...)
}

// Error writes one line for something that broke.
//
// The detail belongs here and nowhere else. A parent gets a code and a request
// id, and this line is what that request id leads to.
func (logger *Logger) Error(message string, fields ...any) {
    if logger == nil {
        return
    }

    logger.inner.Error(message, fields...)
}
