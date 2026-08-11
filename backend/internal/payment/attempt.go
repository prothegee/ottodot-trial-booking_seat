// Package payment owns the charge.
//
// It answers one question and refuses to answer any other: did money move for
// this booking, and exactly once. The seat is somebody else's problem, which is
// why nothing here imports the booking package. The ordering the service runs
// under is payment first, seat second, so this package finishes its work before
// anything looks at a seat, and the two are wired together one layer up.
//
// The package is split so the rules can be read without a database in front of
// them. The amount rules, the idempotency key rules, and the mock provider's
// decision are pure functions. The repository is the only part that touches
// storage, and it has two implementations behind one interface so the same
// behaviour suite runs against both.
package payment

import "time"

// Status is where one payment attempt sits.
//
// The values match the payment_status enum in the migration exactly. A mismatch
// between the two would only be discovered by a write failing, which is a poor
// place to learn about a typo.
type Status string

const (
    // StatusInitiated means the row exists and the provider has not answered
    // yet. An attempt that stays here is one whose call died mid-flight, and it
    // is the only status a replay cannot resolve on its own.
    StatusInitiated Status = "initiated"

    // StatusSucceeded means money moved.
    StatusSucceeded Status = "succeeded"

    // StatusFailed means the provider declined and no money moved. It is not
    // the same as the provider being unreachable, which leaves the attempt
    // initiated because nobody knows what happened.
    StatusFailed Status = "failed"
)

// IsKnown reports whether this is one of the three statuses the enum defines.
// Anything else came from outside the service and is not to be trusted.
func (status Status) IsKnown() bool {
    switch status {
    case StatusInitiated, StatusSucceeded, StatusFailed:
        return true
    }

    return false
}

// IsSettled reports whether the provider has answered. A settled attempt is
// never written to again, which is what makes the table append only in practice
// as well as on paper.
func (status Status) IsSettled() bool {
    return status == StatusSucceeded || status == StatusFailed
}

// AllStatuses lists the three statuses, so a test can walk every one without
// repeating the list and drifting from it.
func AllStatuses() []Status {
    return []Status{StatusInitiated, StatusSucceeded, StatusFailed}
}

// Attempt is one charge against one booking.
//
// Two columns are nullable in the database and are carried here as zero values
// rather than pointers, because each has a zero that cannot be a real value: a
// provider reference is never the empty string, and an attempt that has not
// settled has no instant to report. That keeps every caller free of a nil check.
type Attempt struct {
    ID        string
    BookingID string

    // IdempotencyKey is what makes a replay recognisable. It is unique per
    // booking, which is the uq_payment_idempotency index.
    IdempotencyKey string

    Amount Amount
    Status Status

    // ProviderRef is what the provider calls this charge. It is the only thing
    // kept from the provider, because card data never enters this system.
    ProviderRef string

    // FailureReason is why a decline happened, for an operator. It never
    // reaches a parent, so it still carries no identifier and no name.
    FailureReason string

    CreatedAt time.Time
    SettledAt time.Time
}
