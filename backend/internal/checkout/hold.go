package checkout

import (
    "context"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/queue"
)

// HoldResult is what a granted hold produced.
type HoldResult struct {
    Booking booking.Booking

    // ExpiryScheduled is whether the release job was written.
    //
    // It is reported rather than hidden because a false here is worth a log line
    // and a metric: the hold still stands, the parent is unaffected, and the
    // seat comes back a few minutes later than it should when the same booking
    // is next touched. Turning that into a failure would refuse a parent a hold
    // that was actually granted, which is worse in every direction.
    ExpiryScheduled bool
}

// Hold grants a place on the payment screen and schedules its release.
//
// The order is deliberate and it is not the obvious one. The hold is granted
// first and the job is written second, which means a crash in between leaves a
// hold nobody scheduled a release for. That is survivable: the deadline is on
// the row, so the class stops counting the holder the moment it passes, and the
// only cost is that the row sits in pending_payment until something looks at it.
//
// The other order is not survivable. Scheduling first would write a job for a
// booking that may never exist, and a worker would then be handed an id it can
// never resolve, on every attempt, until it parks.
//
// Param:
// command - booking.HoldCommand (which child, which class)
//
// Return:
//   - the hold, and whether its release was scheduled
//   - whatever the booking service refused with, unchanged, so the http layer
//     maps one set of failures rather than two
func (service *Service) Hold(ctx context.Context, command booking.HoldCommand) (HoldResult, error) {
    granted, err := service.bookings.Hold(ctx, command)

    service.recordHold(err)

    if err != nil {
        return HoldResult{}, err
    }

    scheduleErr := service.schedule(ctx, queue.KindExpireHold, granted.ID,
        granted.HoldExpiresAt.Add(service.settings.ExpiryGrace))

    return HoldResult{Booking: granted, ExpiryScheduled: scheduleErr == nil}, nil
}
