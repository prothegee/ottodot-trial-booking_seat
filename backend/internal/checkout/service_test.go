package checkout_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/identifier"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
)

// The identifiers every case in this package uses. They are fixed rather than
// minted, so a failure names the same child every time it is read.
const (
    classOpen  = "11111111-1111-7111-8111-111111111111"
    classOne   = "22222222-2222-7222-8222-222222222222"
    studentOne = "aaaaaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa"
    studentTwo = "bbbbbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb"
    parentOne  = "cccccccc-cccc-7ccc-8ccc-cccccccccccc"
)

// checkoutMoment is the instant every case starts from.
var checkoutMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// stage is everything a checkout needs, with the fakes reachable so a case can
// assert on what they hold.
type stage struct {
    checkout *checkout.Service
    bookings *booking.MemoryRepository
    payments *payment.MemoryRepository
    provider *payment.MockProvider
    jobs     *queue.MemoryQueue
}

// newStage wires the three domains against their fakes, on a pinned clock.
//
// Two classes: one with four seats, and one with a single seat, so a lost race
// takes two bookings rather than five.
func newStage(t *testing.T) *stage {
    t.Helper()

    bookings := booking.NewMemoryRepository()

    bookings.AddClass(booking.Class{
        ID: classOpen, Subject: "science", Title: "Science trial",
        StartsAt: checkoutMoment.Add(24 * time.Hour), Capacity: 4, HoldAllowance: 2,
    })

    bookings.AddClass(booking.Class{
        ID: classOne, Subject: "math", Title: "Math trial",
        StartsAt: checkoutMoment.Add(24 * time.Hour), Capacity: 1, HoldAllowance: 2,
    })

    bookings.AddStudent(studentOne, parentOne)
    bookings.AddStudent(studentTwo, parentOne)

    bookingSettings := booking.DefaultSettings()
    bookingSettings.Clock = func() time.Time { return checkoutMoment }

    bookingService, err := booking.NewService(bookings, bookingSettings)
    if err != nil {
        t.Fatalf("cannot build the booking service: %v", err)
    }

    payments := payment.NewMemoryRepository()
    provider := payment.NewMockProvider()

    paymentService, err := payment.NewService(payments, provider, payment.Settings{
        Clock: func() time.Time { return checkoutMoment },
    })
    if err != nil {
        t.Fatalf("cannot build the payment service: %v", err)
    }

    jobs := queue.NewMemoryQueue()

    settings := checkout.DefaultSettings()
    settings.Clock = func() time.Time { return checkoutMoment }

    service, err := checkout.NewService(bookingService, paymentService, jobs, settings)
    if err != nil {
        t.Fatalf("cannot build the checkout service: %v", err)
    }

    return &stage{
        checkout: service,
        bookings: bookings,
        payments: payments,
        provider: provider,
        jobs:     jobs,
    }
}

// hold grants a place on the payment screen and fails the test if it was
// refused.
func (fixture *stage) hold(t *testing.T, studentID string, classID string) checkout.HoldResult {
    t.Helper()

    granted, err := fixture.checkout.Hold(context.Background(), booking.HoldCommand{
        StudentID: studentID, ClassID: classID,
    })
    if err != nil {
        t.Fatalf("the hold was refused: %v", err)
    }

    // The payment fake stands in for the foreign key the real table has, so a
    // booking it has never heard of cannot be charged. In production the row
    // already exists by this point, which is exactly what the constraint says.
    fixture.payments.AddBooking(granted.Booking.ID)

    return granted
}

// newKey mints an idempotency key in the format the payment service accepts.
func newKey(t *testing.T) string {
    t.Helper()

    minted, err := identifier.NewUUIDv7()
    if err != nil {
        t.Fatalf("cannot mint a key: %v", err)
    }

    return minted
}

// claimed reads every job the queue is holding.
func (fixture *stage) claimed(t *testing.T) []queue.Job {
    t.Helper()

    jobs, err := fixture.jobs.Claim(context.Background(), queue.ClaimRequest{
        Now: checkoutMoment.Add(time.Hour), Lease: time.Minute, Limit: 20, MaxAttempts: 5,
    })
    if err != nil {
        t.Fatalf("cannot claim: %v", err)
    }

    return jobs
}

func TestBuildingTheCheckoutService(t *testing.T) {
    t.Run("edge: a service missing any of the three is refused at construction", func(t *testing.T) {
        fixture := newStage(t)

        if _, err := checkout.NewService(nil, nil, fixture.jobs, checkout.DefaultSettings()); err == nil {
            t.Fatal("a checkout with no booking service was built")
        }

        if _, err := checkout.NewService(nil, nil, nil, checkout.DefaultSettings()); err == nil {
            t.Fatal("a checkout with nothing at all was built")
        }
    })
}

func TestHoldingASeat(t *testing.T) {
    t.Run("integration: a hold is granted and its release is scheduled", func(t *testing.T) {
        fixture := newStage(t)

        granted := fixture.hold(t, studentOne, classOpen)

        if granted.Booking.Status != booking.StatusPendingPayment {
            t.Fatalf("the hold reads %s", granted.Booking.Status)
        }

        if !granted.ExpiryScheduled {
            t.Fatal("no release was scheduled, so an abandoned payment screen would hold the seat forever")
        }

        jobs := fixture.claimed(t)

        if len(jobs) != 1 || jobs[0].Kind != queue.KindExpireHold {
            t.Fatalf("the queue holds %d jobs: %+v", len(jobs), jobs)
        }
    })

    t.Run("behaviour: the release is scheduled after the deadline, never before it", func(t *testing.T) {
        fixture := newStage(t)

        granted := fixture.hold(t, studentOne, classOpen)
        jobs := fixture.claimed(t)

        if !jobs[0].RunAfter.After(granted.Booking.HoldExpiresAt) {
            t.Fatalf("the release runs at %s and the hold ends at %s, so the worker races the parent",
                jobs[0].RunAfter, granted.Booking.HoldExpiresAt)
        }
    })

    t.Run("behaviour: the job names the booking and carries nothing else", func(t *testing.T) {
        fixture := newStage(t)

        granted := fixture.hold(t, studentOne, classOpen)
        jobs := fixture.claimed(t)

        payload, err := queue.DecodeBookingPayload(jobs[0].Payload)
        if err != nil {
            t.Fatalf("the payload is not readable: %v", err)
        }

        if payload.BookingID != granted.Booking.ID {
            t.Fatalf("the job names %s, the hold is %s", payload.BookingID, granted.Booking.ID)
        }
    })

    t.Run("edge: a refused hold schedules nothing", func(t *testing.T) {
        fixture := newStage(t)

        fixture.hold(t, studentOne, classOpen)

        _, err := fixture.checkout.Hold(context.Background(), booking.HoldCommand{
            StudentID: studentOne, ClassID: classOpen,
        })

        if !errors.Is(err, booking.ErrAlreadyBooked) {
            t.Fatalf("a duplicate hold answered %v", err)
        }

        if jobs := fixture.claimed(t); len(jobs) != 1 {
            t.Fatalf("%d jobs were scheduled for one granted hold", len(jobs))
        }
    })
}
