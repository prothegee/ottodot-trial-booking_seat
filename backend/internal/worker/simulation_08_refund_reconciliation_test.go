package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/booking"
	"ottodot-trial-booking/backend/internal/payment"
	"ottodot-trial-booking/backend/internal/queue"
	"ottodot-trial-booking/backend/internal/worker"
)

// Simulation 8: refund reconciliation.
//
//	worker   -> queue: claim reconcile_refund
//	queue    -> worker: the job row
//	worker   -> repository: the booking is in refund_required
//	worker   -> provider: refund the settled attempt
//	provider -> worker: refunded
//	worker   -> repository: status cancelled, event row
//	worker   -> queue: mark the job done
//
// Asserts: the booking moves to `cancelled`, the refund is recorded, and a
// replay of the same job does not refund twice.
//
// The parent who is refunded here got there the way the brief describes: they
// paid for a seat that was gone by the time their confirm ran. That is the
// whole reason `refund_required` exists, and this job is what clears it.

const (
	refundParent    = "0192e008-0000-7000-8000-000000000001"
	refundWinner    = "0192e008-0000-7000-8000-000000000011"
	refundLoser     = "0192e008-0000-7000-8000-000000000012"
	refundClassID   = "0192e008-0000-7000-8000-000000000021"
	refundPriceCent = 4500
)

// refundMoment is the instant the whole simulation runs at.
var refundMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// lastSeatStage is everything wired together: two parents, one seat, and both
// services over the same fakes.
type lastSeatStage struct {
	bookings       *booking.MemoryRepository
	bookingService *booking.Service
	attempts       *payment.MemoryRepository
	provider       *payment.MockProvider
	paymentService *payment.Service
	jobs           *queue.MemoryQueue
	refunded       []payment.Refund
}

// newLastSeatStage builds a class with one seat and room for two holders, which
// is the shape that produces a paid parent with no seat.
func newLastSeatStage(t *testing.T) *lastSeatStage {
	t.Helper()

	bookings := booking.NewMemoryRepository()
	bookings.AddStudent(refundWinner, refundParent)
	bookings.AddStudent(refundLoser, refundParent)
	bookings.AddClass(booking.Class{
		ID: refundClassID, Subject: "math", Title: "Math Challenge",
		StartsAt: refundMoment.Add(72 * time.Hour), DurationMinutes: 60,
		Capacity: 1, HoldAllowance: 1,
	})

	bookingSettings := booking.DefaultSettings()
	bookingSettings.Clock = func() time.Time { return refundMoment }

	bookingService, err := booking.NewService(bookings, bookingSettings)
	if err != nil {
		t.Fatalf("expected the booking service to build, got: %v", err)
	}

	attempts := payment.NewMemoryRepository()
	provider := payment.NewMockProvider()

	paymentService, err := payment.NewService(attempts, provider, payment.Settings{
		Clock: func() time.Time { return refundMoment },
	})
	if err != nil {
		t.Fatalf("expected the payment service to build, got: %v", err)
	}

	return &lastSeatStage{
		bookings:       bookings,
		bookingService: bookingService,
		attempts:       attempts,
		provider:       provider,
		paymentService: paymentService,
		jobs:           queue.NewMemoryQueue(),
	}
}

// pay grants a hold for one child and settles the charge against it, which is
// everything that happens before the seat is decided.
func (stage *lastSeatStage) pay(t *testing.T, studentID string, key string) booking.Booking {
	t.Helper()

	ctx := context.Background()

	held, err := stage.bookingService.Hold(ctx, booking.HoldCommand{StudentID: studentID, ClassID: refundClassID})
	if err != nil {
		t.Fatalf("expected the hold to be granted, got: %v", err)
	}

	stage.attempts.AddBooking(held.ID)

	_, err = stage.paymentService.Pay(ctx, payment.PayCommand{
		BookingID:      held.ID,
		Amount:         payment.Amount{Cents: refundPriceCent, Currency: payment.DefaultCurrency},
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("expected the charge to settle, got: %v", err)
	}

	return held
}

// runner wires both handlers for real and records every refund that settles.
func (stage *lastSeatStage) runner(t *testing.T) *worker.Runner {
	t.Helper()

	expireHold, err := worker.NewExpireHoldHandler(stage.bookingService)
	if err != nil {
		t.Fatalf("expected the expiry handler to build, got: %v", err)
	}

	reconcile, err := worker.NewReconcileRefundHandler(stage.bookingService, stage.paymentService, func(refund payment.Refund) {
		stage.refunded = append(stage.refunded, refund)
	})
	if err != nil {
		t.Fatalf("expected the reconciliation handler to build, got: %v", err)
	}

	registry := worker.Registry{}

	if err := registry.Register(queue.KindExpireHold, expireHold); err != nil {
		t.Fatalf("cannot register the expiry handler: %v", err)
	}

	if err := registry.Register(queue.KindReconcileRefund, reconcile); err != nil {
		t.Fatalf("cannot register the reconciliation handler: %v", err)
	}

	settings := worker.DefaultSettings()
	settings.Clock = func() time.Time { return refundMoment }

	built, err := worker.NewRunner(stage.jobs, registry, settings)
	if err != nil {
		t.Fatalf("expected the runner to build, got: %v", err)
	}

	return built
}

// scheduleReconciliation puts one reconcile_refund job on the queue.
func (stage *lastSeatStage) scheduleReconciliation(t *testing.T, bookingID string) {
	t.Helper()

	payload, err := queue.EncodeBookingPayload(bookingID)
	if err != nil {
		t.Fatalf("cannot encode the payload: %v", err)
	}

	if _, err := stage.jobs.Enqueue(context.Background(), queue.EnqueueRequest{
		JobID: newIdentifier(t), Kind: queue.KindReconcileRefund, Payload: payload, Now: refundMoment,
	}); err != nil {
		t.Fatalf("cannot schedule the reconciliation, got: %v", err)
	}
}

func TestSimulation08RefundReconciliation(t *testing.T) {
	ctx := context.Background()

	t.Run("behaviour: the parent who lost the seat is refunded and their booking is closed", func(t *testing.T) {
		stage := newLastSeatStage(t)

		winner := stage.pay(t, refundWinner, "key-simulation-8-winner")
		loser := stage.pay(t, refundLoser, "key-simulation-8-loser")

		if _, err := stage.bookingService.Confirm(ctx, winner.ID); err != nil {
			t.Fatalf("expected the first parent to win the seat, got: %v", err)
		}

		// The second parent's money already moved, and now there is no seat.
		// This is exactly the case the brief asks about.
		if _, err := stage.bookingService.Confirm(ctx, loser.ID); !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected ErrSeatLost, got: %v", err)
		}

		owed, err := stage.bookingService.Booking(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected to read the booking back, got: %v", err)
		}

		if owed.Status != booking.StatusRefundRequired {
			t.Fatalf("expected refund_required, got %s", owed.Status)
		}

		stage.scheduleReconciliation(t, loser.ID)

		runner := stage.runner(t)

		claimed, err := runner.RunOnce(ctx)
		if err != nil {
			t.Fatalf("expected the poll to answer, got: %v", err)
		}

		if claimed != 1 {
			t.Fatalf("expected the reconciliation job claimed, got %d", claimed)
		}

		closed, err := stage.bookingService.Booking(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected to read the booking back, got: %v", err)
		}

		if closed.Status != booking.StatusCancelled || closed.HasSeat() {
			t.Fatalf("expected a cancelled booking with no seat, got %s seat %d", closed.Status, closed.SeatNo)
		}

		if stage.provider.Refunds() != 1 {
			t.Fatalf("expected the money sent back once, got %d refunds", stage.provider.Refunds())
		}

		if len(stage.refunded) != 1 || stage.refunded[0].RefundRef == "" {
			t.Fatalf("expected the refund reference written down, got %+v", stage.refunded)
		}

		if !stage.refunded[0].Amount.SameAs(payment.Amount{Cents: refundPriceCent, Currency: payment.DefaultCurrency}) {
			t.Fatalf("expected the whole charge sent back, got %+v", stage.refunded[0].Amount)
		}

		trail, err := stage.bookings.Events(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected the audit trail, got: %v", err)
		}

		// Hold granted, seat lost, refund settled. Three lines, and the last
		// one names the payment path as the actor.
		if len(trail) != 3 || trail[2].ToStatus != booking.StatusCancelled || trail[2].Actor != booking.ActorPayment {
			t.Fatalf("expected the refund recorded in the trail, got %+v", trail)
		}

		// The winner is untouched by any of this.
		confirmed, err := stage.bookingService.Booking(ctx, winner.ID)
		if err != nil {
			t.Fatalf("expected to read the winner back, got: %v", err)
		}

		if confirmed.Status != booking.StatusConfirmed || confirmed.SeatNo != 1 {
			t.Fatalf("expected the winner to keep seat 1, got %s seat %d", confirmed.Status, confirmed.SeatNo)
		}
	})

	t.Run("behaviour: the same job run twice refunds once", func(t *testing.T) {
		// Two workers can hold the same job when a lease lapses mid-run, and a
		// job that failed after the money moved comes back for another try.
		// Neither may send the money twice.
		stage := newLastSeatStage(t)

		winner := stage.pay(t, refundWinner, "key-simulation-8-replay-winner")
		loser := stage.pay(t, refundLoser, "key-simulation-8-replay-loser")

		if _, err := stage.bookingService.Confirm(ctx, winner.ID); err != nil {
			t.Fatalf("expected the first parent to win the seat, got: %v", err)
		}

		if _, err := stage.bookingService.Confirm(ctx, loser.ID); !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected ErrSeatLost, got: %v", err)
		}

		runner := stage.runner(t)

		for run := 0; run < 2; run++ {
			stage.scheduleReconciliation(t, loser.ID)

			if _, err := runner.RunOnce(ctx); err != nil {
				t.Fatalf("expected run %d to answer, got: %v", run+1, err)
			}
		}

		if stage.provider.Refunds() != 1 {
			t.Fatalf("expected the money sent back once, got %d refunds", stage.provider.Refunds())
		}

		if len(stage.refunded) != 1 {
			t.Fatalf("expected one refund recorded, got %d", len(stage.refunded))
		}

		if counted := runner.Counters().Snapshot(); counted.Failed != 0 || counted.Completed != 2 {
			t.Fatalf("expected both jobs to finish cleanly, got %+v", counted)
		}

		trail, err := stage.bookings.Events(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected the audit trail, got: %v", err)
		}

		if len(trail) != 3 {
			t.Fatalf("expected the booking closed exactly once, got %d lines in the trail", len(trail))
		}
	})

	t.Run("behaviour: a provider that cannot be reached leaves the job for another try", func(t *testing.T) {
		// The money may or may not have moved, so the booking keeps saying it
		// is owed, and the job comes back after the backoff.
		stage := newLastSeatStage(t)

		winner := stage.pay(t, refundWinner, "key-simulation-8-retry-winner")
		loser := stage.pay(t, refundLoser, "key-simulation-8-retry-loser")

		if _, err := stage.bookingService.Confirm(ctx, winner.ID); err != nil {
			t.Fatalf("expected the first parent to win the seat, got: %v", err)
		}

		if _, err := stage.bookingService.Confirm(ctx, loser.ID); !errors.Is(err, booking.ErrSeatLost) {
			t.Fatalf("expected ErrSeatLost, got: %v", err)
		}

		if err := stage.provider.ForceOutcome(loser.ID, payment.OutcomeProviderError); err != nil {
			t.Fatalf("cannot pin the outcome: %v", err)
		}

		stage.scheduleReconciliation(t, loser.ID)

		runner := stage.runner(t)

		if _, err := runner.RunOnce(ctx); err != nil {
			t.Fatalf("expected the poll to answer, got: %v", err)
		}

		stillOwed, err := stage.bookingService.Booking(ctx, loser.ID)
		if err != nil {
			t.Fatalf("expected to read the booking back, got: %v", err)
		}

		if stillOwed.Status != booking.StatusRefundRequired {
			t.Fatalf("expected the booking to keep saying it is owed, got %s", stillOwed.Status)
		}

		if counted := runner.Counters().Snapshot(); counted.Failed != 1 || counted.Completed != 0 {
			t.Fatalf("expected the job handed back, got %+v", counted)
		}

		if len(stage.refunded) != 0 {
			t.Fatalf("nothing settled, so nothing is recorded, got %+v", stage.refunded)
		}
	})
}
