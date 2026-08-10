package worker

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording.
//
// No message carries an identifier. These strings reach a log, and a log gets
// pasted into a chat window.
var (
	// ErrUnknownKind means the kind is not one this service runs.
	ErrUnknownKind = errors.New("worker: that job kind is not one this service runs")

	// ErrHandlerMissing means a kind was registered with nothing to run it, or
	// a job arrived for a kind nothing was registered for.
	ErrHandlerMissing = errors.New("worker: no handler is registered for that job kind")

	// ErrHandlerAlreadyRegistered means a kind was registered twice. The second
	// call is refused rather than replacing the first, because a replaced
	// handler is a change nobody can see.
	ErrHandlerAlreadyRegistered = errors.New("worker: that job kind already has a handler")

	// ErrInvalidSettings means the runner was built with a value it cannot work
	// with. It is refused at construction rather than at the first poll.
	ErrInvalidSettings = errors.New("worker: the runner cannot work with those settings")
)
