package payment_test

import (
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/payment"
)

func TestAnAmountIsCheckedBeforeTheDriverSeesIt(t *testing.T) {
    t.Run("unit: an ordinary price in the default currency is accepted", func(t *testing.T) {
        if err := (payment.Amount{Cents: contractPriceCents}).Validate(); err != nil {
            t.Fatalf("expected the seeded price to be valid, got: %v", err)
        }
    })

    t.Run("edge: zero and negative amounts are refused", func(t *testing.T) {
        // The schema carries check (amount_cents > 0). That is the backstop.
        // This is the message, and it exists so a caller gets a reason rather
        // than a constraint name.
        for _, cents := range []int32{0, -1, -contractPriceCents} {
            if err := (payment.Amount{Cents: cents}).Validate(); !errors.Is(err, payment.ErrInvalidAmount) {
                t.Fatalf("expected ErrInvalidAmount for %d cents, got: %v", cents, err)
            }
        }
    })

    t.Run("edge: a currency that is not three capital letters is refused", func(t *testing.T) {
        for _, currency := range []string{"sgd", "SG", "SGDD", "S1D", "S D"} {
            amount := payment.Amount{Cents: contractPriceCents, Currency: currency}

            if err := amount.Validate(); !errors.Is(err, payment.ErrInvalidCurrency) {
                t.Fatalf("expected ErrInvalidCurrency for %q, got: %v", currency, err)
            }
        }
    })

    t.Run("unit: an empty currency becomes the default rather than a failure", func(t *testing.T) {
        normalised := payment.Amount{Cents: contractPriceCents}.Normalised()

        if normalised.Currency != payment.DefaultCurrency {
            t.Fatalf("expected %s, got %q", payment.DefaultCurrency, normalised.Currency)
        }
    })

    t.Run("unit: two amounts are the same charge when cents and currency agree", func(t *testing.T) {
        withDefault := payment.Amount{Cents: contractPriceCents}
        written := payment.Amount{Cents: contractPriceCents, Currency: payment.DefaultCurrency}

        if !withDefault.SameAs(written) {
            t.Fatal("an amount that took the default must match the one written with it")
        }

        if withDefault.SameAs(payment.Amount{Cents: contractPriceCents + 1}) {
            t.Fatal("a different price is a different charge")
        }

        if withDefault.SameAs(payment.Amount{Cents: contractPriceCents, Currency: "USD"}) {
            t.Fatal("the same number in another currency is a different charge")
        }
    })
}
