package payment_test

import (
	"context"
	"errors"
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
