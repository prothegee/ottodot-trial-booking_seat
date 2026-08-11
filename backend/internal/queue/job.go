// Package queue owns background work and nothing else.
//
// It is deliberately empty of domain knowledge. A job here is an identifier, a
// kind, and an opaque payload, and this package never learns what a booking or
// a refund is. That is what lets the worker wire it to anything, and what stops
// a queue bug from being a booking bug.
//
// The rules it does own are the ones a queue has to get right while two workers
// are running:
//
//	a claimed job is invisible to the other worker until its lease runs out
//	a job that keeps failing stops being handed out rather than looping forever
//	a job is completed once, and completing it removes the row
package queue

import "time"

// Kind names what a job is for. The values match the job_queue_kind_allowed
// check constraint in the migration exactly, because a mismatch between the two
// would only be discovered by an insert failing, which is a poor place to learn
// about a typo.
type Kind string

const (
    // KindExpireHold releases a hold whose deadline passed, so the seat behind
    // a parent who walked away comes back.
    KindExpireHold Kind = "expire_hold"

    // KindReconcileRefund sends money back to a parent who paid and lost the
    // seat, and closes the booking once it has gone.
    KindReconcileRefund Kind = "reconcile_refund"
)

// IsKnown reports whether this is one of the two kinds the constraint allows.
// Anything else came from outside this service and is not to be trusted.
func (kind Kind) IsKnown() bool {
    switch kind {
    case KindExpireHold, KindReconcileRefund:
        return true
    }

    return false
}

// AllKinds lists the two kinds, so a test can walk every one without repeating
// the list and drifting from it.
func AllKinds() []Kind {
    return []Kind{KindExpireHold, KindReconcileRefund}
}

// Job is one piece of background work.
//
// LockedUntil is carried as a zero time rather than a pointer, because a job
// nobody holds has no instant to report and every caller would otherwise need a
// nil check to ask the one question that matters: is it claimed right now.
type Job struct {
    ID   string
    Kind Kind

    // Payload is the job's own arguments, stored as jsonb. This package never
    // reads inside it. The handler that understands the kind is the only thing
    // that decodes it.
    Payload []byte

    // RunAfter is the earliest instant this job may be claimed. It is how a
    // hold expiry is scheduled for a deadline that has not arrived yet, and how
    // a failed job backs off before its next try.
    RunAfter time.Time

    // Attempts counts how many times this job has been handed to a worker,
    // including the try in progress. It is incremented by the claim rather than
    // by the outcome, so a worker that dies mid-job still spends one.
    Attempts int16

    // LockedUntil is when the current claim lapses. Zero means nobody holds it.
    LockedUntil time.Time

    CreatedAt time.Time
}

// IsClaimed reports whether another worker currently holds this job.
//
// The lease is compared rather than trusted as a flag, because a worker that
// dies holding a job never clears anything. The claim simply stops being
// believed once its instant passes, which is what makes a crashed worker's work
// recoverable without anybody noticing it crashed.
func (job Job) IsClaimed(now time.Time) bool {
    return !job.LockedUntil.IsZero() && job.LockedUntil.After(now)
}

// IsDue reports whether this job's scheduled instant has arrived.
func (job Job) IsDue(now time.Time) bool {
    return !job.RunAfter.After(now)
}

// IsParked reports whether this job has spent its attempts and will not be
// handed out again.
//
// A parked job is left in the table on purpose. Deleting it would hide the
// failure, and the operator queue view in phase 6 is what surfaces these.
func (job Job) IsParked(maxAttempts int) bool {
    return int(job.Attempts) >= maxAttempts
}
