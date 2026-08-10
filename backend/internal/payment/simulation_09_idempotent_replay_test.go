package payment_test

import (
	"context"
	"errors"
	"testing"

	"ottodot-trial-booking/backend/internal/payment"
)

// Simulation 9: idempotent payment replay.
//
//	parent -> api: pay with idempotency key K
//	api    -> repository: write the attempt under key K, charge, settled
//	parent -> api: retry with the same key K
//	api    -> repository: the key is taken, uq_payment_idempotency holds
//	api    -> parent: the original result again, no second charge
//
// Asserts: exactly one payment_attempts row, exactly one charge at the
// provider, and an identical answer from both calls.

const replayBooking = "0192c009-0000-7000-8000-000000000001"

func TestSimulation09IdempotentPaymentReplay(t *testing.T) {
	t.Run("behaviour: the same key twice charges once and answers the same", func(t *testing.T) {
		ctx := context.Background()

		service, repository, provider := newServiceFor(t, replayBooking)

		first, err := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9", contractPriceCents))
		if err != nil {
			t.Fatalf("expected the first charge to settle, got: %v", err)
		}

		second, err := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9", contractPriceCents))
		if err != nil {
			t.Fatalf("a replay is not a failure, got: %v", err)
		}

		if first != second {
			t.Fatalf("both calls must answer identically:\nfirst  %+v\nsecond %+v", first, second)
		}

		if provider.Charges() != 1 {
			t.Fatalf("expected exactly one charge, got %d", provider.Charges())
		}

		stored, err := repository.AttemptsFor(ctx, replayBooking)
		if err != nil {
			t.Fatalf("expected the attempts for the booking, got: %v", err)
		}

		if len(stored) != 1 {
			t.Fatalf("expected exactly one payment_attempts row, got %d", len(stored))
		}

		if stored[0].ID != first.ID {
			t.Fatalf("the surviving row must be the one the first call wrote, got %s", stored[0].ID)
		}
	})

	t.Run("behaviour: a replayed decline replays the decline, and still charges once", func(t *testing.T) {
		// The rule is that a replay repeats the original answer, whatever it
		// was. A decline that turned into a success on retry would be a second
		// charge wearing the first one's key.
		ctx := context.Background()

		service, repository, provider := newServiceFor(t, replayBooking)

		first, firstErr := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9-declined", declinedPriceCents))
		if !errors.Is(firstErr, payment.ErrDeclined) {
			t.Fatalf("expected the first charge to be declined, got: %v", firstErr)
		}

		second, secondErr := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9-declined", declinedPriceCents))
		if !errors.Is(secondErr, payment.ErrDeclined) {
			t.Fatalf("expected the replay to report the same decline, got: %v", secondErr)
		}

		if first != second {
			t.Fatalf("both calls must answer identically:\nfirst  %+v\nsecond %+v", first, second)
		}

		if provider.Charges() != 1 {
			t.Fatalf("expected exactly one charge, got %d", provider.Charges())
		}

		stored, err := repository.AttemptsFor(ctx, replayBooking)
		if err != nil {
			t.Fatalf("expected the attempts for the booking, got: %v", err)
		}

		if len(stored) != 1 {
			t.Fatalf("expected exactly one payment_attempts row, got %d", len(stored))
		}
	})

	t.Run("edge: a different key against the same booking is a second charge", func(t *testing.T) {
		// Idempotency is per key, not per booking. A parent who genuinely pays
		// twice with two keys gets two attempts, and that is the honest answer.
		ctx := context.Background()

		service, repository, provider := newServiceFor(t, replayBooking)

		if _, err := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9-a", contractPriceCents)); err != nil {
			t.Fatalf("expected the first charge to settle, got: %v", err)
		}

		if _, err := service.Pay(ctx, payCommandFor(replayBooking, "key-simulation-9-b", contractPriceCents)); err != nil {
			t.Fatalf("expected the second charge to settle, got: %v", err)
		}

		if provider.Charges() != 2 {
			t.Fatalf("expected two charges for two keys, got %d", provider.Charges())
		}

		stored, err := repository.AttemptsFor(ctx, replayBooking)
		if err != nil {
			t.Fatalf("expected the attempts for the booking, got: %v", err)
		}

		if len(stored) != 2 {
			t.Fatalf("expected two rows for two keys, got %d", len(stored))
		}
	})
}
