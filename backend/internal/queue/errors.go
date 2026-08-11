package queue

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording.
//
// No message carries an identifier. These strings reach a log, and a log gets
// pasted into a chat window.
var (
    // ErrInvalidRequest means the request was refused before anything was read
    // or written.
    ErrInvalidRequest = errors.New("queue: the request is missing something it needs")

    // ErrUnknownKind means the kind is not one the check constraint allows.
    // Refusing it here turns a constraint violation nobody can read into a
    // typed failure at the boundary.
    ErrUnknownKind = errors.New("queue: that job kind is not one this service runs")

    // ErrInvalidPayload means the payload is not the json object the column
    // requires. An empty payload is refused too, because jsonb has no empty
    // value and a job with no arguments still carries an object.
    ErrInvalidPayload = errors.New("queue: the payload must be a json object")

    // ErrJobNotFound means no job carries that id. Completing a job twice
    // reports it, which is how a caller tells a replay from a lost write.
    ErrJobNotFound = errors.New("queue: no such job")

    // ErrDuplicateJob means a job already exists with that id.
    ErrDuplicateJob = errors.New("queue: a job already carries that id")
)
