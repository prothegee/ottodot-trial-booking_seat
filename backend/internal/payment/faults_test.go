package payment_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/payment"
)

// oneShot is a fault source that fires once for the named point.
func oneShot(point string) payment.Fault {
    spent := false

    return func(reached string) bool {
        if spent || reached != point {
            return false
        }

        spent = true

        return true
    }
}

func TestChargeUnderInjectedFaults(t *testing.T) {
    t.Run("integration: an armed provider error is not a decline", func(t *testing.T) {
        // The two mean opposite things. A decline is an answer and no money
        // moved. This is no answer at all, and nobody knows whether it did.
        provider := payment.NewMockProvider()
        provider.InjectFaults(oneShot(faults.PointPaymentProviderError))

        _, err := provider.Charge(context.Background(), payment.ChargeRequest{
            AttemptID: "0192c000-0000-7000-8000-000000000001",
            Reference: "0192c000-0000-7000-8000-000000000002",
            Amount:    payment.Amount{Cents: 4500, Currency: payment.DefaultCurrency},
        })

        if !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("the armed charge answered %v", err)
        }
    })

    t.Run("edge: the call is counted even though it produced no answer", func(t *testing.T) {
        // A charge that left this service and got nothing back is exactly the
        // case where money may have moved. Not counting it would make the one
        // dangerous outcome the one with no record.
        provider := payment.NewMockProvider()
        provider.InjectFaults(oneShot(faults.PointPaymentProviderError))

        _, _ = provider.Charge(context.Background(), payment.ChargeRequest{
            AttemptID: "0192c000-0000-7000-8000-000000000001",
            Reference: "0192c000-0000-7000-8000-000000000002",
            Amount:    payment.Amount{Cents: 4500, Currency: payment.DefaultCurrency},
        })

        if provider.Charges() != 1 {
            t.Fatalf("the provider recorded %d charges for one attempt that reached it", provider.Charges())
        }
    })

    t.Run("behaviour: a one shot spends itself and the next charge settles", func(t *testing.T) {
        provider := payment.NewMockProvider()
        provider.InjectFaults(oneShot(faults.PointPaymentProviderError))

        request := payment.ChargeRequest{
            AttemptID: "0192c000-0000-7000-8000-000000000001",
            Reference: "0192c000-0000-7000-8000-000000000002",
            Amount:    payment.Amount{Cents: 4500, Currency: payment.DefaultCurrency},
        }

        if _, err := provider.Charge(context.Background(), request); err == nil {
            t.Fatal("the armed charge succeeded")
        }

        result, err := provider.Charge(context.Background(), request)
        if err != nil {
            t.Fatalf("the second charge answered %v", err)
        }

        if result.Outcome != payment.OutcomeSettled {
            t.Fatalf("the second charge produced %s", result.Outcome)
        }
    })

    t.Run("unit: a provider with no fault source follows the amount rule alone", func(t *testing.T) {
        provider := payment.NewMockProvider()

        result, err := provider.Charge(context.Background(), payment.ChargeRequest{
            AttemptID: "0192c000-0000-7000-8000-000000000001",
            Reference: "0192c000-0000-7000-8000-000000000002",
            Amount:    payment.Amount{Cents: 4500, Currency: payment.DefaultCurrency},
        })
        if err != nil {
            t.Fatalf("an unarmed charge answered %v", err)
        }

        if result.Outcome != payment.OutcomeSettled {
            t.Fatalf("an unarmed charge produced %s", result.Outcome)
        }
    })
}
