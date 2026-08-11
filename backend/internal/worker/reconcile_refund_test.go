package worker_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// closerStub stands in for the booking service. It records the order it was
// called in, because the order is what this handler is about.
type closerStub struct {
    held     booking.Booking
    readErr  error
    closeErr error

    steps []string
}

func (stub *closerStub) Booking(_ context.Context, _ string) (booking.Booking, error) {
    stub.steps = append(stub.steps, "read")

    return stub.held, stub.readErr
}

func (stub *closerStub) Cancel(_ context.Context, _ string, actor booking.Actor, reason string) (booking.Booking, error) {
    stub.steps = append(stub.steps, "close:"+string(actor)+":"+reason)

    return booking.Booking{Status: booking.StatusCancelled}, stub.closeErr
}

// reconcilerStub stands in for the payment service.
type reconcilerStub struct {
    answer payment.Refund
    err    error
    calls  int
}

func (stub *reconcilerStub) Refund(_ context.Context, _ payment.RefundCommand) (payment.Refund, error) {
    stub.calls++

    return stub.answer, stub.err
}

// owedBooking is a booking sitting in refund_required, which is the only state
// this handler acts on.
func owedBooking() booking.Booking {
    return booking.Booking{Status: booking.StatusRefundRequired}
}

func TestTheReconciliationHandlerRefundsBeforeItCloses(t *testing.T) {
    ctx := context.Background()

    owed := newIdentifier(t)

    t.Run("integration: the money goes back first and the booking closes second", func(t *testing.T) {
        // The order is not interchangeable. A booking closed before the money
        // moved would look settled while the parent is still out of pocket, and
        // nothing would ever come back to fix it.
        closer := &closerStub{held: owedBooking()}
        reconciler := &reconcilerStub{answer: payment.Refund{RefundRef: "mock_refund_1"}}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); err != nil {
            t.Fatalf("expected the job to finish, got: %v", err)
        }

        if reconciler.calls != 1 {
            t.Fatalf("expected one refund, got %d", reconciler.calls)
        }

        if len(closer.steps) != 2 || closer.steps[0] != "read" {
            t.Fatalf("expected a read then a close, got %v", closer.steps)
        }

        if closer.steps[1] != "close:payment:refund settled, booking closed by the worker" {
            t.Fatalf("expected the payment actor and a reason with no identifier in it, got %q", closer.steps[1])
        }
    })

    t.Run("integration: the refund reference reaches the recorder", func(t *testing.T) {
        // It has no column of its own, so this is the one place it is written
        // down. An operator asked where a parent's money is needs it.
        closer := &closerStub{held: owedBooking()}
        reconciler := &reconcilerStub{answer: payment.Refund{RefundRef: "mock_refund_2"}}

        var recorded []payment.Refund

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, func(refund payment.Refund) {
            recorded = append(recorded, refund)
        })
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); err != nil {
            t.Fatalf("expected the job to finish, got: %v", err)
        }

        if len(recorded) != 1 || recorded[0].RefundRef != "mock_refund_2" {
            t.Fatalf("expected the refund reference recorded once, got %v", recorded)
        }
    })

    t.Run("edge: a booking that is not owed a refund is left alone", func(t *testing.T) {
        // This is the replay guard. A booking that already went through here is
        // cancelled, so the provider is never asked a second time.
        closer := &closerStub{held: booking.Booking{Status: booking.StatusCancelled}}
        reconciler := &reconcilerStub{}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); err != nil {
            t.Fatalf("expected a job with nothing to do to finish, got: %v", err)
        }

        if reconciler.calls != 0 {
            t.Fatalf("expected the provider to be left alone, got %d calls", reconciler.calls)
        }

        if len(closer.steps) != 1 {
            t.Fatalf("expected only the read, got %v", closer.steps)
        }
    })

    t.Run("edge: a provider that cannot be reached leaves the booking as it was", func(t *testing.T) {
        // Nobody knows whether the money moved, so closing the booking would be
        // a guess written into the record.
        closer := &closerStub{held: owedBooking()}
        reconciler := &reconcilerStub{err: payment.ErrProviderUnavailable}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable to reach the runner, got: %v", err)
        }

        if len(closer.steps) != 1 {
            t.Fatalf("expected the booking untouched after the read, got %v", closer.steps)
        }
    })

    t.Run("edge: a booking owed money with no settled charge is handed back, not closed", func(t *testing.T) {
        // The booking says money moved and the attempts say it did not. Closing
        // it would erase the disagreement.
        closer := &closerStub{held: owedBooking()}
        reconciler := &reconcilerStub{err: payment.ErrNothingToRefund}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); !errors.Is(err, payment.ErrNothingToRefund) {
            t.Fatalf("expected ErrNothingToRefund to reach the runner, got: %v", err)
        }

        if len(closer.steps) != 1 {
            t.Fatalf("expected the booking untouched, got %v", closer.steps)
        }
    })

    t.Run("edge: a close that fails hands the job back after the money has gone", func(t *testing.T) {
        // The awkward case, and the reason the refund carries an idempotency
        // key: the retry comes straight back to the refund, and the provider is
        // what recognises it.
        closer := &closerStub{held: owedBooking(), closeErr: booking.ErrInvalidTransition}
        reconciler := &reconcilerStub{answer: payment.Refund{RefundRef: "mock_refund_3"}}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); !errors.Is(err, booking.ErrInvalidTransition) {
            t.Fatalf("expected the close failure to reach the runner, got: %v", err)
        }
    })

    t.Run("edge: a booking that cannot be read hands the job back", func(t *testing.T) {
        closer := &closerStub{readErr: booking.ErrBookingNotFound}
        reconciler := &reconcilerStub{}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        if err := handler.Handle(ctx, jobFor(t, queue.KindReconcileRefund, owed)); !errors.Is(err, booking.ErrBookingNotFound) {
            t.Fatalf("expected ErrBookingNotFound to reach the runner, got: %v", err)
        }

        if reconciler.calls != 0 {
            t.Fatalf("expected the provider to be left alone, got %d calls", reconciler.calls)
        }
    })

    t.Run("edge: a payload nobody can act on never reaches either service", func(t *testing.T) {
        closer := &closerStub{held: owedBooking()}
        reconciler := &reconcilerStub{}

        handler, err := worker.NewReconcileRefundHandler(closer, reconciler, nil)
        if err != nil {
            t.Fatalf("expected the handler to build, got: %v", err)
        }

        unreadable := queue.Job{ID: newIdentifier(t), Kind: queue.KindReconcileRefund, Payload: []byte(`{}`)}

        if err := handler.Handle(ctx, unreadable); !errors.Is(err, queue.ErrInvalidPayload) {
            t.Fatalf("expected ErrInvalidPayload, got: %v", err)
        }

        if len(closer.steps) != 0 || reconciler.calls != 0 {
            t.Fatalf("an unreadable job must not reach storage, got %v and %d calls", closer.steps, reconciler.calls)
        }
    })

    t.Run("edge: a handler missing either service is refused at construction", func(t *testing.T) {
        if _, err := worker.NewReconcileRefundHandler(nil, &reconcilerStub{}, nil); !errors.Is(err, worker.ErrHandlerMissing) {
            t.Fatalf("expected ErrHandlerMissing with no booking service, got: %v", err)
        }

        if _, err := worker.NewReconcileRefundHandler(&closerStub{}, nil, nil); !errors.Is(err, worker.ErrHandlerMissing) {
            t.Fatalf("expected ErrHandlerMissing with no payment service, got: %v", err)
        }
    })
}
