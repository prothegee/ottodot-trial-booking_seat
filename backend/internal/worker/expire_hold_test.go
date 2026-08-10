package worker_test

import (
	"context"
	"errors"
	"testing"

	"ottodot-trial-booking/backend/internal/booking"
	"ottodot-trial-booking/backend/internal/queue"
	"ottodot-trial-booking/backend/internal/worker"
)

// expirerStub stands in for the booking service, so a case about the handler
// needs no class, no student, and no seat.
type expirerStub struct {
	answer  error
	asked   []string
	timesUp booking.Booking
}

func (stub *expirerStub) Expire(_ context.Context, bookingID string) (booking.Booking, error) {
	stub.asked = append(stub.asked, bookingID)

	return stub.timesUp, stub.answer
}

// jobFor builds a job of one kind carrying one booking.
func jobFor(t *testing.T, kind queue.Kind, bookingID string) queue.Job {
	t.Helper()

	payload, err := queue.EncodeBookingPayload(bookingID)
	if err != nil {
		t.Fatalf("cannot encode a payload: %v", err)
	}

	return queue.Job{ID: newIdentifier(t), Kind: kind, Payload: payload, RunAfter: runnerMoment}
}

func TestTheExpiryHandlerReadsThreeAnswersDifferently(t *testing.T) {
	ctx := context.Background()

	held := newIdentifier(t)

	t.Run("integration: a lapsed hold is released and the job is finished", func(t *testing.T) {
		stub := &expirerStub{timesUp: booking.Booking{Status: booking.StatusExpired}}

		handler, err := worker.NewExpireHoldHandler(stub)
		if err != nil {
			t.Fatalf("expected the handler to build, got: %v", err)
		}

		if err := handler.Handle(ctx, jobFor(t, queue.KindExpireHold, held)); err != nil {
			t.Fatalf("expected the job to finish, got: %v", err)
		}

		if len(stub.asked) != 1 || stub.asked[0] != held {
			t.Fatalf("expected the booking in the payload, got %v", stub.asked)
		}
	})

	t.Run("edge: a booking that already moved on finishes the job", func(t *testing.T) {
		// The ordinary case for a job that arrives after the parent paid.
		// Nothing is wrong, and retrying it would never change the answer.
		stub := &expirerStub{answer: booking.ErrNotHolding}

		handler, err := worker.NewExpireHoldHandler(stub)
		if err != nil {
			t.Fatalf("expected the handler to build, got: %v", err)
		}

		if err := handler.Handle(ctx, jobFor(t, queue.KindExpireHold, held)); err != nil {
			t.Fatalf("expected a job that has nothing to do to finish, got: %v", err)
		}
	})

	t.Run("edge: a hold still standing hands the job back rather than taking the seat", func(t *testing.T) {
		// The one mistake this job must never make.
		stub := &expirerStub{answer: booking.ErrHoldStillLive}

		handler, err := worker.NewExpireHoldHandler(stub)
		if err != nil {
			t.Fatalf("expected the handler to build, got: %v", err)
		}

		if err := handler.Handle(ctx, jobFor(t, queue.KindExpireHold, held)); !errors.Is(err, booking.ErrHoldStillLive) {
			t.Fatalf("expected ErrHoldStillLive to reach the runner, got: %v", err)
		}
	})

	t.Run("edge: a booking that is not there hands the job back so it parks", func(t *testing.T) {
		// job_queue hangs from no foreign key on purpose, so a job can outlive
		// what it refers to. Swallowing that would hide it. Parking it puts it
		// where somebody has to look.
		stub := &expirerStub{answer: booking.ErrBookingNotFound}

		handler, err := worker.NewExpireHoldHandler(stub)
		if err != nil {
			t.Fatalf("expected the handler to build, got: %v", err)
		}

		if err := handler.Handle(ctx, jobFor(t, queue.KindExpireHold, held)); !errors.Is(err, booking.ErrBookingNotFound) {
			t.Fatalf("expected ErrBookingNotFound to reach the runner, got: %v", err)
		}
	})

	t.Run("edge: a payload nobody can act on never reaches the booking service", func(t *testing.T) {
		stub := &expirerStub{}

		handler, err := worker.NewExpireHoldHandler(stub)
		if err != nil {
			t.Fatalf("expected the handler to build, got: %v", err)
		}

		unreadable := queue.Job{ID: newIdentifier(t), Kind: queue.KindExpireHold, Payload: []byte("expire it")}

		if err := handler.Handle(ctx, unreadable); !errors.Is(err, queue.ErrInvalidPayload) {
			t.Fatalf("expected ErrInvalidPayload, got: %v", err)
		}

		if len(stub.asked) != 0 {
			t.Fatalf("an unreadable job must not reach storage, got %v", stub.asked)
		}
	})

	t.Run("edge: a handler with nothing to call is refused at construction", func(t *testing.T) {
		if _, err := worker.NewExpireHoldHandler(nil); !errors.Is(err, worker.ErrHandlerMissing) {
			t.Fatalf("expected ErrHandlerMissing, got: %v", err)
		}
	})
}
