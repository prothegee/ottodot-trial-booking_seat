// Package catalogue is the advisory read side: what classes exist and roughly
// how many seats each has left.
//
// It is deliberately a separate package from booking, and the separation is the
// point rather than tidiness. Booking owns the seat and reads the primary
// because it decides. Nothing here decides anything: every number this package
// produces is a hint that saves a parent a wasted click, so it reads the
// replica, it is safe to cache, and it is allowed to be a second behind.
//
// The rule that makes that safe is enforced one layer up: the confirm
// transaction counts seats again under a lock, and a parent whose click loses
// that race is told so. A seat count on a screen has never been the decision.
package catalogue

import "time"

// Class is one trial class as a parent sees it.
//
// SeatsRemaining is in the same struct as the class rather than fetched
// separately, because a screen showing a class always shows the number and two
// reads would put them a moment apart for no gain.
type Class struct {
    ID              string
    Subject         string
    Title           string
    StartsAt        time.Time
    DurationMinutes int16
    Capacity        int16

    // SeatsRemaining is capacity minus the seats already owned. It is advisory,
    // never negative, and never the basis of a decision.
    SeatsRemaining int16
}

// IsFull reports whether a screen should show this class as having nothing left.
// It is what a client renders, and it is still only a hint.
func (class Class) IsFull() bool {
    return class.SeatsRemaining <= 0
}
