package booking

import "time"

// MaxHolders is how many parents may sit on the payment screen for one class at
// the same time.
//
//	max holders = capacity + hold_allowance
//
// Allowance 0 means nobody is ever charged for a seat they cannot get, at the
// cost of a seat sitting idle behind a parent who walked away. An allowance
// above 0 fills seats reliably and accepts that a few parents will pay and be
// refunded. The column is per class, so the strict behaviour is a data change
// rather than a code change.
func MaxHolders(class Class) int {
    return int(class.Capacity) + int(class.HoldAllowance)
}

// HoldIsLive reports whether a hold is still standing at the given instant.
//
// Note:
//   - a hold whose deadline is exactly now is expired, not live. The boundary
//     has to fall one way, and expiring is the safe direction: it releases a
//     slot rather than holding one open on a tie.
//   - a zero deadline is not a hold at all, so it is never live. A confirmed or
//     finished booking carries no deadline.
//
// Param:
// deadline - time.Time (when the hold runs out)
// now - time.Time (the instant being asked about)
//
// Return:
//   - true while the hold still stands
//   - false at the deadline and after it
func HoldIsLive(deadline time.Time, now time.Time) bool {
    if deadline.IsZero() {
        return false
    }

    return deadline.After(now)
}

// HoldDeadline stamps when a new hold runs out.
func HoldDeadline(now time.Time, ttl time.Duration) time.Time {
    return now.Add(ttl)
}
