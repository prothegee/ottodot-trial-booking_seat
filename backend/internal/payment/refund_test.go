package payment_test

import (
    "context"
    "errors"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/payment"
)

// The booking every case here refunds. It is its own identifier so a failure in
// this file never reads as a failure in the pay path.
const refundBooking = "0192d000-0000-7000-8000-000000000201"

func TestARefundSendsBackTheChargeThatSettled(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: the settled attempt is the one reversed", func(t *testing.T) {
        service, _, provider := newServiceFor(t, refundBooking)

        paid, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-settled", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        sentBack, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the refund to go through, got: %v", err)
        }

        if sentBack.AttemptID != paid.ID || sentBack.ProviderRef != paid.ProviderRef {
            t.Fatalf("expected the settled attempt reversed, got %+v", sentBack)
        }

        if !sentBack.Amount.SameAs(paid.Amount) {
            t.Fatalf("expected the amount that was charged, got %+v", sentBack.Amount)
        }

        if provider.Refunds() != 1 {
            t.Fatalf("expected exactly one refund at the provider, got %d", provider.Refunds())
        }
    })

    t.Run("unit: the refund carries its own reference, not the charge's", func(t *testing.T) {
        // An operator chasing a parent's bank needs the identifier for the
        // refund. Handing back the charge reference would send them looking for
        // the wrong record.
        service, _, _ := newServiceFor(t, refundBooking)

        paid, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-reference", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        sentBack, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the refund to go through, got: %v", err)
        }

        if sentBack.RefundRef == "" || sentBack.RefundRef == paid.ProviderRef {
            t.Fatalf("expected a reference of its own, got %q", sentBack.RefundRef)
        }

        if !strings.Contains(sentBack.RefundRef, "refund") {
            t.Fatalf("a mock reference has to read as one, got %q", sentBack.RefundRef)
        }
    })

    t.Run("unit: the refund is stamped from the service clock", func(t *testing.T) {
        service, _, _ := newServiceFor(t, refundBooking)

        if _, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-stamp", contractPriceCents)); err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        sentBack, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the refund to go through, got: %v", err)
        }

        if !sentBack.RefundedAt.Equal(contractMoment) {
            t.Fatalf("expected the pinned clock, got %v", sentBack.RefundedAt)
        }
    })

    t.Run("edge: a booking whose only charge was declined has nothing to send back", func(t *testing.T) {
        service, _, provider := newServiceFor(t, refundBooking)

        if _, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-declined", contractPriceCents+1)); !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("expected the charge to be declined, got: %v", err)
        }

        _, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if !errors.Is(err, payment.ErrNothingToRefund) {
            t.Fatalf("expected ErrNothingToRefund, got: %v", err)
        }

        if provider.Refunds() != 0 {
            t.Fatalf("no money moved, so the provider must not be asked, got %d calls", provider.Refunds())
        }
    })

    t.Run("edge: a booking with no attempt at all has nothing to send back", func(t *testing.T) {
        service, _, _ := newServiceFor(t, refundBooking)

        _, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if !errors.Is(err, payment.ErrNothingToRefund) {
            t.Fatalf("expected ErrNothingToRefund, got: %v", err)
        }
    })

    t.Run("edge: a decline followed by a settled charge refunds the settled one", func(t *testing.T) {
        // The ordinary shape of a parent whose first card was refused. Two
        // rows, one of them real, and picking the wrong one would leave the
        // money where it is.
        service, _, _ := newServiceFor(t, refundBooking)

        if _, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-first", contractPriceCents+1)); !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("expected the first charge to be declined, got: %v", err)
        }

        paid, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-second", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the second charge to settle, got: %v", err)
        }

        sentBack, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the refund to go through, got: %v", err)
        }

        if sentBack.AttemptID != paid.ID {
            t.Fatalf("expected the settled attempt, got %s", sentBack.AttemptID)
        }
    })

    t.Run("edge: refunding nothing is refused before the provider is called", func(t *testing.T) {
        service, _, provider := newServiceFor(t, refundBooking)

        if _, err := service.Refund(ctx, payment.RefundCommand{}); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }

        if provider.Refunds() != 0 {
            t.Fatalf("an incomplete request must not reach the provider, got %d calls", provider.Refunds())
        }
    })

    t.Run("edge: a provider that cannot be reached reports it rather than claiming success", func(t *testing.T) {
        // Nobody knows whether the money moved, so the only honest answer is
        // the failure. The caller releases the job and tries again.
        service, _, provider := newServiceFor(t, refundBooking)

        if _, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-unreachable", contractPriceCents)); err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        if err := provider.ForceOutcome(refundBooking, payment.OutcomeProviderError); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        _, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable, got: %v", err)
        }
    })

    t.Run("integration: calling it twice moves money once", func(t *testing.T) {
        // The case that matters is not a parent clicking twice. It is the
        // reconciliation job refunding, failing to close the booking, and being
        // retried into exactly this call.
        service, _, provider := newServiceFor(t, refundBooking)

        if _, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-twice", contractPriceCents)); err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        first, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the first refund to go through, got: %v", err)
        }

        second, err := service.Refund(ctx, payment.RefundCommand{BookingID: refundBooking})
        if err != nil {
            t.Fatalf("expected the replay to be answered, got: %v", err)
        }

        if provider.Refunds() != 1 {
            t.Fatalf("expected the money to move once, got %d refunds", provider.Refunds())
        }

        if second.RefundRef != first.RefundRef {
            t.Fatalf("a replay must report the original refund, got %q then %q", first.RefundRef, second.RefundRef)
        }
    })

    t.Run("unit: the key is derived from the attempt, so it survives a restart", func(t *testing.T) {
        // A key with a clock or a random value in it would be new on every run
        // and guard nothing, which is the failure this asserts against.
        service, _, _ := newServiceFor(t, refundBooking)

        paid, err := service.Pay(ctx, payCommandFor(refundBooking, "key-refund-derived", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        first := payment.RefundKeyFor(paid.ID)

        if first == "" || first == paid.ID {
            t.Fatalf("expected a key of its own, got %q", first)
        }

        if payment.RefundKeyFor(paid.ID) != first {
            t.Fatal("the same attempt must always produce the same key")
        }
    })
}
