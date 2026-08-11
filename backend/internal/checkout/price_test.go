package checkout_test

import (
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/payment"
)

func TestAcceptingAnAmount(t *testing.T) {
    t.Run("unit: the price this service owns is accepted", func(t *testing.T) {
        if err := checkout.AcceptAmount(checkout.TrialPrice(), false); err != nil {
            t.Fatalf("the trial price was refused: %v", err)
        }
    })

    t.Run("unit: an amount with no currency is read as this service's own", func(t *testing.T) {
        if err := checkout.AcceptAmount(payment.Amount{Cents: checkout.TrialPriceCents}, false); err != nil {
            t.Fatalf("an amount with no currency was refused: %v", err)
        }
    })

    t.Run("edge: paying less than the price is refused", func(t *testing.T) {
        err := checkout.AcceptAmount(payment.Amount{Cents: 1, Currency: "SGD"}, false)

        if !errors.Is(err, payment.ErrInvalidAmount) {
            t.Fatalf("underpaying answered %v", err)
        }
    })

    t.Run("edge: paying more than the price is refused just as firmly", func(t *testing.T) {
        err := checkout.AcceptAmount(payment.Amount{Cents: checkout.TrialPriceCents * 2, Currency: "SGD"}, false)

        if !errors.Is(err, payment.ErrInvalidAmount) {
            t.Fatalf("overpaying answered %v, and an amount that is not the price is a client deciding for us", err)
        }
    })

    t.Run("edge: another currency is refused", func(t *testing.T) {
        err := checkout.AcceptAmount(payment.Amount{Cents: checkout.TrialPriceCents, Currency: "USD"}, false)

        if !errors.Is(err, payment.ErrInvalidCurrency) {
            t.Fatalf("a foreign currency answered %v", err)
        }
    })

    t.Run("edge: zero is refused before anything is charged", func(t *testing.T) {
        if err := checkout.AcceptAmount(payment.Amount{}, false); !errors.Is(err, payment.ErrInvalidAmount) {
            t.Fatalf("an empty amount answered %v", err)
        }
    })

    t.Run("behaviour: the demonstration amounts work locally and nowhere else", func(t *testing.T) {
        decline := payment.Amount{Cents: checkout.TrialPriceCents + 1, Currency: "SGD"}
        unreachable := payment.Amount{Cents: checkout.TrialPriceCents + 2, Currency: "SGD"}

        for _, amount := range []payment.Amount{decline, unreachable} {
            if err := checkout.AcceptAmount(amount, true); err != nil {
                t.Fatalf("%d was refused in development, so a decline cannot be demonstrated: %v", amount.Cents, err)
            }

            if err := checkout.AcceptAmount(amount, false); !errors.Is(err, payment.ErrInvalidAmount) {
                t.Fatalf("%d was accepted outside development, answered %v", amount.Cents, err)
            }
        }
    })
}
