// Package booking owns the seat.
//
// Everything here exists to answer one question correctly while several parents
// are asking it at once: who holds seat N of class X. The answer is decided in
// exactly one place, the confirm transaction on the primary, and every other
// number this package produces is advisory.
//
// The package is split so that the rules can be read without a database in
// front of them. The state machine, the seat picker, and the hold rules are
// pure functions. The repository is the only part that touches storage, and it
// has two implementations behind one interface so the same behaviour suite runs
// against both.
package booking

import "time"

// Actor is who caused a transition. It is written into the audit trail and its
// values match the check constraint on booking_events.
type Actor string

const (
	// ActorParent is the parent acting for their own child.
	ActorParent Actor = "parent"

	// ActorSystem is this service acting on its own, inside a transaction or
	// from the worker.
	ActorSystem Actor = "system"

	// ActorAdmin is an operator acting on someone else's booking.
	ActorAdmin Actor = "admin"

	// ActorPayment is the payment path acting on the outcome of a charge.
	ActorPayment Actor = "payment"
)

// Class is a trial class as this package needs it. Capacity and allowance are
// the only two fields any rule reads, the rest is what a screen shows.
type Class struct {
	ID              string
	Subject         string
	Title           string
	StartsAt        time.Time
	DurationMinutes int16
	Capacity        int16
	HoldAllowance   int16
}

// Booking is one child's place in one class.
//
// Three columns are nullable in the database and are carried here as zero
// values rather than pointers, because each has a zero that cannot be a real
// value: seat numbers start at 1, and a booking that never confirmed has no
// instant to report. That keeps every caller free of a nil check.
type Booking struct {
	ID            string
	StudentID     string
	ClassID       string
	Status        Status
	SeatNo        int16
	HoldExpiresAt time.Time
	ConfirmedAt   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// HasSeat reports whether this booking owns a seat number. A booking without
// one is either still paying or already finished.
func (booking Booking) HasSeat() bool {
	return booking.SeatNo >= 1
}

// Event is one line of the audit trail. The trail is append only, so an event
// is never updated once written.
type Event struct {
	ID         string
	BookingID  string
	FromStatus Status
	ToStatus   Status
	Actor      Actor
	Reason     string
	CreatedAt  time.Time
}
