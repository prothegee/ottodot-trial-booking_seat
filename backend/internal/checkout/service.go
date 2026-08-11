// Package checkout is where the seat, the money, and the background queue meet.
//
// It exists because that meeting has an order, and the order is the design.
// Booking knows nothing about money. Payment knows nothing about seats. Queue
// knows nothing about either. Each of those is deliberate, and it leaves exactly
// one thing nobody owns: what happens, in what sequence, when a parent presses
// pay. That is this package, and it is the only place in the service that
// imports all three.
//
// It is a package rather than a handler method for one reason: the sequence is
// the part most likely to be wrong, and it has to be testable without an http
// request, a router, or a cookie anywhere near it.
package checkout

import (
    "context"
    "errors"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/identifier"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
)

// Settings are the two things this package needs handed to it rather than
// reaching for, plus the one policy value it owns.
type Settings struct {
    // Clock is where every stamped time comes from. Nil means the real clock.
    Clock func() time.Time

    // NewJobID mints the identifier for a scheduled job. Nil means UUIDv7.
    NewJobID func() (string, error)

    // ExpiryGrace is how long after a hold's deadline the expiry job is
    // scheduled for.
    //
    // It is not zero, and the reason is worth stating: a job that runs at the
    // exact instant a deadline passes races the parent who is pressing pay at
    // that instant. The grace makes the worker lose that race on purpose, and
    // the booking side refuses an early expiry anyway, so this is the second of
    // two defences rather than the only one.
    ExpiryGrace time.Duration
}

// DefaultSettings are the values this package runs with when nothing overrides
// them.
func DefaultSettings() Settings {
    return Settings{ExpiryGrace: 30 * time.Second}
}

// withDefaults fills in whatever the caller left out.
func (settings Settings) withDefaults() Settings {
    if settings.Clock == nil {
        settings.Clock = time.Now
    }

    if settings.NewJobID == nil {
        settings.NewJobID = identifier.NewUUIDv7
    }

    if settings.ExpiryGrace < 0 {
        settings.ExpiryGrace = 0
    }

    return settings
}

// Service owns the order of a checkout.
type Service struct {
    bookings *booking.Service
    payments *payment.Service
    jobs     queue.Queue
    settings Settings
}

// NewService wires the three domains together.
//
// Param:
// bookings - *booking.Service (the seat)
// payments - *payment.Service (the money)
// jobs - queue.Queue (what runs later, and survives a restart)
// settings - Settings (the clock, the job identifier, and the expiry grace)
//
// Return:
//   - the service
//   - an error naming what is missing, refused at construction rather than at
//     the first parent who presses pay
func NewService(bookings *booking.Service, payments *payment.Service, jobs queue.Queue, settings Settings) (*Service, error) {
    if bookings == nil {
        return nil, errors.New("checkout: the service needs the booking service")
    }

    if payments == nil {
        return nil, errors.New("checkout: the service needs the payment service")
    }

    if jobs == nil {
        return nil, errors.New("checkout: the service needs the job queue")
    }

    return &Service{
        bookings: bookings,
        payments: payments,
        jobs:     jobs,
        settings: settings.withDefaults(),
    }, nil
}

// schedule writes one job.
//
// Failing to schedule is never allowed to undo work that already committed. The
// two callers here both hand the failure back as a value they decide about,
// rather than as an error that replaces what actually happened to the booking.
func (service *Service) schedule(ctx context.Context, kind queue.Kind, bookingID string, runAfter time.Time) error {
    jobID, err := service.settings.NewJobID()
    if err != nil {
        return err
    }

    payload, err := queue.EncodeBookingPayload(bookingID)
    if err != nil {
        return err
    }

    _, err = service.jobs.Enqueue(ctx, queue.EnqueueRequest{
        JobID:    jobID,
        Kind:     kind,
        Payload:  payload,
        RunAfter: runAfter,
        Now:      service.settings.Clock(),
    })

    return err
}
