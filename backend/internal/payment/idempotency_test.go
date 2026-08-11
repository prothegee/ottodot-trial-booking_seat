package payment_test

import (
    "errors"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/payment"
)

func TestAnIdempotencyKeyIsCheckedBeforeAnyWrite(t *testing.T) {
    t.Run("unit: an ordinary key is accepted", func(t *testing.T) {
        for _, key := range []string{"k", "0192d000-0000-7000-8000-000000000001", strings.Repeat("k", payment.MaxIdempotencyKeyLength)} {
            if err := payment.ValidateIdempotencyKey(key); err != nil {
                t.Fatalf("expected %q to be usable, got: %v", key, err)
            }
        }
    })

    t.Run("edge: an empty or oversized key is refused", func(t *testing.T) {
        refused := []string{"", strings.Repeat("k", payment.MaxIdempotencyKeyLength+1)}

        for _, key := range refused {
            if err := payment.ValidateIdempotencyKey(key); !errors.Is(err, payment.ErrInvalidIdempotencyKey) {
                t.Fatalf("expected ErrInvalidIdempotencyKey for a key of length %d, got: %v", len(key), err)
            }
        }
    })

    t.Run("edge: a key that cannot travel in a header is refused", func(t *testing.T) {
        // The value arrives in an Idempotency-Key header, so anything that
        // could split a header line, or that no header may carry, is refused
        // rather than stored and matched later.
        refused := []string{" ", "with space", "with\ttab", "with\nnewline", "with\rreturn", "café"}

        for _, key := range refused {
            if err := payment.ValidateIdempotencyKey(key); !errors.Is(err, payment.ErrInvalidIdempotencyKey) {
                t.Fatalf("expected ErrInvalidIdempotencyKey for %q, got: %v", key, err)
            }
        }
    })
}
