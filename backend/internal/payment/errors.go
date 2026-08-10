package payment

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording. The http layer is what turns each one
// into a status and a typed code for the client, which is why no message here
// is written for a parent to read.
//
// No message carries an identifier, a name, or an amount. These strings reach a
// log, and a log gets pasted into a chat window.
var (
	// ErrInvalidRequest means the request was refused before anything was read
	// or written.
	ErrInvalidRequest = errors.New("payment: the request is missing something it needs")

	// ErrInvalidAmount means the charge was zero or below. The schema check is
	// the backstop, this is the readable answer.
	ErrInvalidAmount = errors.New("payment: the amount must be greater than zero")

	// ErrInvalidCurrency means the code is not three capital letters.
	ErrInvalidCurrency = errors.New("payment: the currency must be three capital letters")

	// ErrInvalidIdempotencyKey means the key is empty, too long, or carries a
	// character that cannot travel in a header.
	ErrInvalidIdempotencyKey = errors.New("payment: the idempotency key is not a usable key")

	// ErrBookingNotFound means no booking carries that id. This package names
	// the failure itself rather than importing the booking package, because the
	// charge must not depend on the seat.
	ErrBookingNotFound = errors.New("payment: no such booking")

	// ErrAttemptNotFound means no attempt carries that id.
	ErrAttemptNotFound = errors.New("payment: no such payment attempt")

	// ErrAlreadySettled means the provider already answered for this attempt.
	// The table is append only after settlement, so there is nothing to write.
	ErrAlreadySettled = errors.New("payment: this attempt has already settled")

	// ErrAttemptPending means an earlier call with the same key wrote its row
	// and never came back with an answer. Charging again would risk a second
	// charge, so the replay reports the unfinished attempt instead.
	ErrAttemptPending = errors.New("payment: an earlier attempt with this key has not settled")

	// ErrIdempotencyConflict means the key was already used for a different
	// charge. Replaying the original answer would be a lie, so it is refused.
	ErrIdempotencyConflict = errors.New("payment: this key was already used for a different charge")

	// ErrDeclined means the provider gave a business answer and no money moved.
	// The booking is free to move to payment_failed, and nothing needs
	// refunding.
	ErrDeclined = errors.New("payment: the provider declined the charge")

	// ErrProviderUnavailable means the provider could not be reached, so
	// nobody knows whether money moved. It is not a decline: the booking keeps
	// the status it had, and the attempt stays initiated until something
	// resolves it.
	ErrProviderUnavailable = errors.New("payment: the provider could not be reached")
)
