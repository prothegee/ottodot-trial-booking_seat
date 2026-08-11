package payment_test

import (
    "context"
    "errors"
    "fmt"
    "testing"

    "ottodot-trial-booking/backend/internal/payment"
)

// chargeRequestFor builds a charge for one booking at one price.
func chargeRequestFor(t *testing.T, cents int32) payment.ChargeRequest {
    t.Helper()

    return payment.ChargeRequest{
        AttemptID:      newAttemptID(t),
        Reference:      bookingOne,
        Amount:         payment.Amount{Cents: cents, Currency: payment.DefaultCurrency},
        IdempotencyKey: "key-mock",
    }
}

func TestTheMockProviderDecidesFromTheRequestAlone(t *testing.T) {
    ctx := context.Background()

    t.Run("unit: the amount rule answers the same way every time", func(t *testing.T) {
        cases := []struct {
            cents   int32
            outcome payment.Outcome
        }{
            {contractPriceCents, payment.OutcomeSettled},
            {contractPriceCents + 1, payment.OutcomeDeclined},
            {contractPriceCents + 2, payment.OutcomeProviderError},
            {1, payment.OutcomeDeclined},
            {2, payment.OutcomeProviderError},
            {99, payment.OutcomeSettled},
        }

        for _, expected := range cases {
            amount := payment.Amount{Cents: expected.cents, Currency: payment.DefaultCurrency}

            for attempt := 0; attempt < 3; attempt++ {
                if decided := payment.DecideOutcome(amount); decided != expected.outcome {
                    t.Fatalf("expected %s for %d cents, got %s", expected.outcome, expected.cents, decided)
                }
            }
        }
    })

    t.Run("behaviour: a settled charge carries a provider reference and nothing else", func(t *testing.T) {
        provider := payment.NewMockProvider()
        request := chargeRequestFor(t, contractPriceCents)

        result, err := provider.Charge(ctx, request)
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        if result.Outcome != payment.OutcomeSettled || result.ProviderRef == "" {
            t.Fatalf("a settled charge names itself: %+v", result)
        }

        if result.FailureReason != "" {
            t.Fatalf("a settled charge has no failure to report, got %q", result.FailureReason)
        }
    })

    t.Run("behaviour: a decline is an answer, not an error", func(t *testing.T) {
        provider := payment.NewMockProvider()

        result, err := provider.Charge(ctx, chargeRequestFor(t, contractPriceCents+1))
        if err != nil {
            t.Fatalf("a decline is a business answer and must not be reported as an error, got: %v", err)
        }

        if result.Outcome != payment.OutcomeDeclined || result.ProviderRef != "" {
            t.Fatalf("a decline moved no money, so it names no charge: %+v", result)
        }

        if result.FailureReason == "" {
            t.Fatal("a decline has to say why, for an operator")
        }
    })

    t.Run("behaviour: an unreachable provider is an error and not a decline", func(t *testing.T) {
        // This is the seam phase 7 arms as payment.provider_error. Nobody knows
        // whether money moved, so the booking must keep the status it had.
        provider := payment.NewMockProvider()

        result, err := provider.Charge(ctx, chargeRequestFor(t, contractPriceCents+2))
        if !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable, got: %v", err)
        }

        if result.Outcome != payment.OutcomeProviderError {
            t.Fatalf("the outcome has to name itself for a metric, got %s", result.Outcome)
        }
    })

    t.Run("behaviour: a forced outcome overrides the amount rule", func(t *testing.T) {
        // A seeded class has a fixed price, so forcing the reference is how a
        // scenario gets a decline without changing what the class costs.
        provider := payment.NewMockProvider()

        if err := provider.ForceOutcome(bookingOne, payment.OutcomeDeclined); err != nil {
            t.Fatalf("expected the outcome to be pinned, got: %v", err)
        }

        result, err := provider.Charge(ctx, chargeRequestFor(t, contractPriceCents))
        if err != nil {
            t.Fatalf("a decline is not an error, got: %v", err)
        }

        if result.Outcome != payment.OutcomeDeclined {
            t.Fatalf("expected the pinned decline to win over the amount, got %s", result.Outcome)
        }
    })

    t.Run("edge: an outcome the provider cannot give is refused", func(t *testing.T) {
        provider := payment.NewMockProvider()

        if err := provider.ForceOutcome(bookingOne, payment.Outcome("refunded")); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }

        if err := provider.ForceOutcome("", payment.OutcomeDeclined); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest for an empty reference, got: %v", err)
        }
    })

    t.Run("edge: a charge of nothing never reaches the provider's decision", func(t *testing.T) {
        provider := payment.NewMockProvider()

        if _, err := provider.Charge(ctx, chargeRequestFor(t, 0)); !errors.Is(err, payment.ErrInvalidAmount) {
            t.Fatalf("expected ErrInvalidAmount, got: %v", err)
        }

        if provider.Charges() != 0 {
            t.Fatalf("a refused request is not a charge, got %d", provider.Charges())
        }
    })

    t.Run("unit: every charge is counted", func(t *testing.T) {
        provider := payment.NewMockProvider()

        for i := 0; i < 3; i++ {
            if _, err := provider.Charge(ctx, chargeRequestFor(t, contractPriceCents)); err != nil {
                t.Fatalf("expected the charge to settle, got: %v", err)
            }
        }

        if provider.Charges() != 3 {
            t.Fatalf("expected three charges, got %d", provider.Charges())
        }
    })
}

func TestTheMockProviderSendsMoneyBackWithoutDecliningIt(t *testing.T) {
    ctx := context.Background()

    // refundRequestFor builds a refund of one settled charge, with a key of its
    // own so each case is a fresh refund rather than a replay of the last one.
    refundRequestFor := func(providerRef string, key string) payment.RefundRequest {
        return payment.RefundRequest{
            ProviderRef:    providerRef,
            Reference:      bookingOne,
            Amount:         payment.Amount{Cents: contractPriceCents, Currency: payment.DefaultCurrency},
            IdempotencyKey: key,
        }
    }

    t.Run("unit: a settled charge is sent back and gets its own reference", func(t *testing.T) {
        provider := payment.NewMockProvider()

        charged, err := provider.Charge(ctx, chargeRequestFor(t, contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        sentBack, err := provider.Refund(ctx, refundRequestFor(charged.ProviderRef, "refund-settled"))
        if err != nil {
            t.Fatalf("expected the refund to go through, got: %v", err)
        }

        if sentBack.RefundRef == "" || sentBack.RefundRef == charged.ProviderRef {
            t.Fatalf("expected a reference of its own, got %q", sentBack.RefundRef)
        }
    })

    t.Run("unit: the amount rule does not decline a refund", func(t *testing.T) {
        // An amount ending in 01 is a decline on the way out. On the way back
        // there is no such answer, because a provider does not refuse to return
        // money it already took.
        provider := payment.NewMockProvider()

        request := refundRequestFor("mock_reference", "refund-amount-rule")
        request.Amount.Cents = contractPriceCents + 1

        if _, err := provider.Refund(ctx, request); err != nil {
            t.Fatalf("expected a refund to have no declined answer, got: %v", err)
        }
    })

    t.Run("edge: a reference pinned as unreachable cannot be refunded either", func(t *testing.T) {
        provider := payment.NewMockProvider()

        if err := provider.ForceOutcome(bookingOne, payment.OutcomeProviderError); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        if _, err := provider.Refund(ctx, refundRequestFor("mock_reference", "refund-unreachable")); !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable, got: %v", err)
        }
    })

    t.Run("edge: a refund of no charge is refused and never counted", func(t *testing.T) {
        provider := payment.NewMockProvider()

        if _, err := provider.Refund(ctx, refundRequestFor("", "refund-no-charge")); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }

        if provider.Refunds() != 0 {
            t.Fatalf("a refused request is not a refund, got %d", provider.Refunds())
        }
    })

    t.Run("unit: every refund with its own key is counted", func(t *testing.T) {
        provider := payment.NewMockProvider()

        for i := 0; i < 3; i++ {
            key := fmt.Sprintf("refund-counted-%d", i)

            if _, err := provider.Refund(ctx, refundRequestFor("mock_reference", key)); err != nil {
                t.Fatalf("expected the refund to go through, got: %v", err)
            }
        }

        if provider.Refunds() != 3 {
            t.Fatalf("expected three refunds, got %d", provider.Refunds())
        }
    })

    t.Run("edge: a repeated key is answered from the store and moves nothing", func(t *testing.T) {
        // The guard the whole refund path leans on, asserted at the one place
        // that implements it.
        provider := payment.NewMockProvider()

        first, err := provider.Refund(ctx, refundRequestFor("mock_reference", "refund-replayed"))
        if err != nil {
            t.Fatalf("expected the first refund to go through, got: %v", err)
        }

        second, err := provider.Refund(ctx, refundRequestFor("mock_reference", "refund-replayed"))
        if err != nil {
            t.Fatalf("expected the replay to be answered, got: %v", err)
        }

        if second.RefundRef != first.RefundRef {
            t.Fatalf("a replay must report the original refund, got %q then %q", first.RefundRef, second.RefundRef)
        }

        if provider.Refunds() != 1 {
            t.Fatalf("expected the money to move once, got %d refunds", provider.Refunds())
        }
    })

    t.Run("edge: a refund with no key is refused, because nothing would guard it", func(t *testing.T) {
        provider := payment.NewMockProvider()

        if _, err := provider.Refund(ctx, refundRequestFor("mock_reference", "")); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }
    })

    t.Run("edge: an unreachable provider stores nothing, so a later try is a fresh attempt", func(t *testing.T) {
        // Storing a refund that never happened would make the retry a replay of
        // nothing, and the money would stay where it is forever.
        provider := payment.NewMockProvider()

        if err := provider.ForceOutcome(bookingOne, payment.OutcomeProviderError); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        if _, err := provider.Refund(ctx, refundRequestFor("mock_reference", "refund-recovers")); !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable, got: %v", err)
        }

        if err := provider.ForceOutcome(bookingOne, payment.OutcomeSettled); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        if _, err := provider.Refund(ctx, refundRequestFor("mock_reference", "refund-recovers")); err != nil {
            t.Fatalf("expected the retry to go through, got: %v", err)
        }

        if provider.Refunds() != 1 {
            t.Fatalf("expected the money to move once, got %d refunds", provider.Refunds())
        }
    })
}
