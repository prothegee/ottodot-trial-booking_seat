package worker_test

import (
    "context"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// Simulation 7: hold expiry by the worker.
//
//	worker -> queue: claim expire_hold with for update skip locked
//	queue  -> worker: the job row
//	worker -> repository: status expired, hold released, event row
//	worker -> queue: mark the job done
//	note: the slot is available again to other parents
//
// Asserts: the booking becomes `expired`, the slot frees up, the job is not
// claimable twice, and a second worker polling alongside claims nothing.
//
// The job is scheduled for the deadline itself, which is the ordinary shape:
// whoever grants a hold knows when it runs out, so the queue does the waiting
// rather than a timer inside a request path.

const (
    expiryParent  = "0192e007-0000-7000-8000-000000000001"
    expiryStudent = "0192e007-0000-7000-8000-000000000011"
    expiryRival   = "0192e007-0000-7000-8000-000000000012"
    expiryClass   = "0192e007-0000-7000-8000-000000000021"
)

// expiryMoment is when the hold is granted. The deadline follows from the
// service settings rather than being written twice.
var expiryMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// clockAt is a clock a test moves by hand, so a deadline is crossed exactly
// rather than by sleeping.
type clockAt struct {
    now time.Time
}

func (clock *clockAt) Read() time.Time {
    return clock.now
}

// singleSeatClass is a class where one holder fills it, so a slot being
// released is visible rather than inferred.
func singleSeatClass() booking.Class {
    return booking.Class{
        ID: expiryClass, Subject: "science", Title: "Science Discovery",
        StartsAt: expiryMoment.Add(72 * time.Hour), DurationMinutes: 60,
        Capacity: 1, HoldAllowance: 0,
    }
}

func TestSimulation07HoldExpiryByTheWorker(t *testing.T) {
    ctx := context.Background()

    t.Run("behaviour: a lapsed hold is released and the slot goes back to everyone else", func(t *testing.T) {
        clock := &clockAt{now: expiryMoment}

        bookings := booking.NewMemoryRepository()
        bookings.AddStudent(expiryStudent, expiryParent)
        bookings.AddStudent(expiryRival, expiryParent)
        bookings.AddClass(singleSeatClass())

        bookingSettings := booking.DefaultSettings()
        bookingSettings.Clock = clock.Read

        bookingService, err := booking.NewService(bookings, bookingSettings)
        if err != nil {
            t.Fatalf("expected the booking service to build, got: %v", err)
        }

        held, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: expiryStudent, ClassID: expiryClass})
        if err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }

        // With one seat and no allowance, the class is full while that hold
        // stands. This is the state the expiry has to undo.
        if _, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: expiryRival, ClassID: expiryClass}); err == nil {
            t.Fatal("expected the class to be full while the first hold stands")
        }

        jobs := queue.NewMemoryQueue()

        payload, err := queue.EncodeBookingPayload(held.ID)
        if err != nil {
            t.Fatalf("cannot encode the payload: %v", err)
        }

        if _, err := jobs.Enqueue(ctx, queue.EnqueueRequest{
            JobID:    newIdentifier(t),
            Kind:     queue.KindExpireHold,
            Payload:  payload,
            RunAfter: held.HoldExpiresAt,
            Now:      expiryMoment,
        }); err != nil {
            t.Fatalf("cannot schedule the expiry, got: %v", err)
        }

        runner := newExpiryRunner(t, jobs, bookingService, clock)

        // Before the deadline the job is not due, and the worker leaves the
        // parent on the payment screen alone.
        if claimed, err := runner.RunOnce(ctx); err != nil || claimed != 0 {
            t.Fatalf("expected nothing claimed before the deadline, got %d and %v", claimed, err)
        }

        clock.now = held.HoldExpiresAt

        claimed, err := runner.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if claimed != 1 {
            t.Fatalf("expected the expiry job claimed once the deadline passed, got %d", claimed)
        }

        lapsed, err := bookingService.Booking(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected to read the booking back, got: %v", err)
        }

        if lapsed.Status != booking.StatusExpired || lapsed.HasSeat() {
            t.Fatalf("expected an expired booking with no seat, got %s seat %d", lapsed.Status, lapsed.SeatNo)
        }

        // The point of the whole job: somebody else can now take the slot.
        if _, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: expiryRival, ClassID: expiryClass}); err != nil {
            t.Fatalf("expected the released slot to be bookable, got: %v", err)
        }

        trail, err := bookings.Events(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected the audit trail, got: %v", err)
        }

        if len(trail) != 2 || trail[1].ToStatus != booking.StatusExpired || trail[1].Actor != booking.ActorSystem {
            t.Fatalf("expected the expiry recorded as the system's doing, got %+v", trail)
        }
    })

    t.Run("behaviour: the job is claimable once, and a second worker gets nothing", func(t *testing.T) {
        // The queue is shared, the workers are not. Whichever polls first takes
        // the job, and the other finds an empty queue rather than doing the
        // same work twice.
        //
        // The memory queue serializes the two polls with a mutex, so this
        // proves the runner and not the sql. The transaction level version of
        // the same story runs against Postgres in internal/queue.
        clock := &clockAt{now: expiryMoment}

        bookings := booking.NewMemoryRepository()
        bookings.AddStudent(expiryStudent, expiryParent)
        bookings.AddClass(singleSeatClass())

        bookingSettings := booking.DefaultSettings()
        bookingSettings.Clock = clock.Read

        bookingService, err := booking.NewService(bookings, bookingSettings)
        if err != nil {
            t.Fatalf("expected the booking service to build, got: %v", err)
        }

        held, err := bookingService.Hold(ctx, booking.HoldCommand{StudentID: expiryStudent, ClassID: expiryClass})
        if err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }

        jobs := queue.NewMemoryQueue()

        payload, err := queue.EncodeBookingPayload(held.ID)
        if err != nil {
            t.Fatalf("cannot encode the payload: %v", err)
        }

        if _, err := jobs.Enqueue(ctx, queue.EnqueueRequest{
            JobID:    newIdentifier(t),
            Kind:     queue.KindExpireHold,
            Payload:  payload,
            RunAfter: held.HoldExpiresAt,
            Now:      expiryMoment,
        }); err != nil {
            t.Fatalf("cannot schedule the expiry, got: %v", err)
        }

        clock.now = held.HoldExpiresAt

        first := newExpiryRunner(t, jobs, bookingService, clock)
        second := newExpiryRunner(t, jobs, bookingService, clock)

        claimedByFirst, err := first.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the first poll to answer, got: %v", err)
        }

        claimedBySecond, err := second.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the second poll to answer, got: %v", err)
        }

        if claimedByFirst != 1 || claimedBySecond != 0 {
            t.Fatalf("expected the job handed out once, got %d then %d", claimedByFirst, claimedBySecond)
        }

        if second.Counters().Snapshot().Failed != 0 {
            t.Fatal("a worker that found nothing has failed at nothing")
        }

        trail, err := bookings.Events(ctx, held.ID)
        if err != nil {
            t.Fatalf("expected the audit trail, got: %v", err)
        }

        // Two lines, not three. A second expiry would have written another.
        if len(trail) != 2 {
            t.Fatalf("expected the hold expired exactly once, got %d lines in the trail", len(trail))
        }
    })
}

// newExpiryRunner wires a runner whose expiry handler is real and whose refund
// handler is a stand-in, because this simulation is about one kind of job.
func newExpiryRunner(t *testing.T, jobs queue.Queue, bookingService *booking.Service, clock *clockAt) *worker.Runner {
    t.Helper()

    expireHold, err := worker.NewExpireHoldHandler(bookingService)
    if err != nil {
        t.Fatalf("expected the expiry handler to build, got: %v", err)
    }

    registry := worker.Registry{}

    if err := registry.Register(queue.KindExpireHold, expireHold); err != nil {
        t.Fatalf("cannot register the expiry handler: %v", err)
    }

    if err := registry.Register(queue.KindReconcileRefund, noopHandler()); err != nil {
        t.Fatalf("cannot register the refund handler: %v", err)
    }

    settings := worker.DefaultSettings()
    settings.Clock = clock.Read

    runner, err := worker.NewRunner(jobs, registry, settings)
    if err != nil {
        t.Fatalf("expected the runner to build, got: %v", err)
    }

    return runner
}
