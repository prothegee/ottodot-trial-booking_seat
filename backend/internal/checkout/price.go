package checkout

import "ottodot-trial-booking/backend/internal/payment"

// TrialPriceCents is what a trial class costs. One class, one country, one
// price, so it is a value here rather than a column nobody would ever vary.
//
// The client sends the amount it believes it is paying, and this service refuses
// anything that is not this. That is the whole reason this file exists: a
// charge whose size comes from the request body is a charge a caller sets for
// themselves.
const TrialPriceCents = 4500

// TrialPrice is that number as money.
func TrialPrice() payment.Amount {
    return payment.Amount{Cents: TrialPriceCents, Currency: payment.DefaultCurrency}
}

// The two amounts the mock provider reads as something other than a settlement.
//
// They are the price plus one and the price plus two, which is exactly the rule
// in provider_mock.go: an amount ending in 01 declines and one ending in 02
// makes the provider unreachable. They are accepted only in development, so a
// demonstration of a declined payment is one keystroke away locally and
// impossible anywhere else.
const (
    demonstrationDeclineCents     = TrialPriceCents + 1
    demonstrationUnreachableCents = TrialPriceCents + 2
)

// AcceptAmount decides whether a requested charge may reach the provider.
//
// Note:
//   - the check is on the exact value, not on a minimum. Overpaying is refused
//     as firmly as underpaying, because an amount that is not the price is a
//     client that has decided something this service owns.
//
// Param:
// requested - payment.Amount (what the client said it was paying)
// development - bool (whether the two demonstration amounts are allowed)
//
// Return:
//   - nil when this amount may be charged
//   - payment.ErrInvalidAmount when it is not the price, and not one of the two
//     amounts a local demonstration is allowed to use
//   - payment.ErrInvalidCurrency when the code is not this service's
func AcceptAmount(requested payment.Amount, development bool) error {
    normalised := requested.Normalised()

    if err := normalised.Validate(); err != nil {
        return err
    }

    if normalised.Currency != payment.DefaultCurrency {
        return payment.ErrInvalidCurrency
    }

    if normalised.Cents == TrialPriceCents {
        return nil
    }

    if development && (normalised.Cents == demonstrationDeclineCents || normalised.Cents == demonstrationUnreachableCents) {
        return nil
    }

    return payment.ErrInvalidAmount
}
