package booking

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording. The http layer is what turns each one
// into a status and a typed code for the client, which is why no message here
// is written for a parent to read.
//
// No message carries an identifier or a name. These strings reach a log, and a
// log gets pasted into a chat window.
var (
	// ErrInvalidRequest means the request was refused before anything was
	// read or written.
	ErrInvalidRequest = errors.New("booking: the request is missing something it needs")

	// ErrClassNotFound means no class carries that id.
	ErrClassNotFound = errors.New("booking: no such class")

	// ErrStudentNotFound means no student carries that id.
	ErrStudentNotFound = errors.New("booking: no such student")

	// ErrBookingNotFound means no booking carries that id.
	ErrBookingNotFound = errors.New("booking: no such booking")

	// ErrAlreadyBooked means this child already has a live booking for this
	// class. It mirrors the uq_booking_active index.
	ErrAlreadyBooked = errors.New("booking: this child already has a live booking for this class")

	// ErrTooManyHolds means this parent is already at the cap for holds that
	// are still standing.
	ErrTooManyHolds = errors.New("booking: this parent is at the concurrent hold cap")

	// ErrClassFull means capacity plus allowance is already taken, so no
	// further hold can be granted.
	ErrClassFull = errors.New("booking: the class has no room for another holder")

	// ErrSeatLost means the payment settled but every seat went to someone
	// else. The booking is left in refund_required, so money that moved has a
	// record telling an operator to move it back.
	ErrSeatLost = errors.New("booking: no seat was free when this booking reached the front")

	// ErrNotHolding means the booking is not in pending_payment, so there is
	// nothing to confirm. It covers a hold the worker already expired, a
	// booking someone cancelled, and a double confirm.
	ErrNotHolding = errors.New("booking: this booking is not holding a place")

	// ErrHoldStillLive means the deadline has not passed, so there is nothing
	// to expire yet. It exists to stop the worker taking a seat away from a
	// parent who is still on the payment screen, which is the one mistake a
	// hold expiry job must never make.
	ErrHoldStillLive = errors.New("booking: this hold has not run out yet")

	// ErrInvalidTransition means the move is not in the state machine.
	ErrInvalidTransition = errors.New("booking: that status change is not allowed")
)
