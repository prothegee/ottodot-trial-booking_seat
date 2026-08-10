package payment

import "context"

// Provider is the only way this package reaches money.
//
// It is an interface for two reasons. The obvious one is that the fast test
// tiers need something to run against. The other is that the shape here is the
// shape a real provider has, so swapping the mock for one touches this file and
// nothing else: an amount, a reference, an idempotency key, and one of three
// answers back.
type Provider interface {
	// Charge asks the provider to move money once.
	//
	// The three answers are deliberately distinct, and a caller that collapses
	// them will get the ledger wrong:
	//
	//   settled       money moved, ProviderRef is set
	//   declined      a business answer, no money moved, no error
	//   provider error nobody knows, ErrProviderUnavailable is returned
	//
	// A decline is not an error. It is the provider saying no, which is an
	// answer, and the booking can be told it failed. An unreachable provider is
	// an error, and the booking must keep the status it had, because a charge
	// may or may not have happened.
	Charge(ctx context.Context, request ChargeRequest) (ChargeResult, error)
}

// Outcome is what the provider decided. It is kept alongside the error rather
// than folded into it, so a metric can count outcomes by name without parsing
// anything.
type Outcome string

const (
	// OutcomeSettled means money moved.
	OutcomeSettled Outcome = "settled"

	// OutcomeDeclined means the provider said no and no money moved.
	OutcomeDeclined Outcome = "declined"

	// OutcomeProviderError means the provider could not be reached. The name
	// matches the `payment.provider_error` fault point that phase 7 arms at
	// this seam, so the injected failure and the reported outcome read the
	// same.
	OutcomeProviderError Outcome = "provider_error"
)

// IsKnown reports whether this is one of the three answers a provider may give.
// Anything else came from outside and is not to be trusted.
func (outcome Outcome) IsKnown() bool {
	switch outcome {
	case OutcomeSettled, OutcomeDeclined, OutcomeProviderError:
		return true
	}

	return false
}

// AllOutcomes lists the three answers, so a test can walk every one without
// repeating the list and drifting from it.
func AllOutcomes() []Outcome {
	return []Outcome{OutcomeSettled, OutcomeDeclined, OutcomeProviderError}
}

// ChargeRequest is everything a provider needs and nothing more.
//
// No card data, no parent, no child, no class. This service never holds a
// payment instrument, and the reference it hands over is an identifier rather
// than anything about a person.
type ChargeRequest struct {
	// AttemptID is this attempt's own identifier. It is what the provider
	// reference is derived from, so one attempt maps to one charge.
	AttemptID string

	// Reference is the booking the money is for. A provider shows it on a
	// statement, which is why it is an identifier and never a name.
	Reference string

	Amount Amount

	// IdempotencyKey is passed through, so a provider that supports one can
	// refuse a second charge on its own side too. The unique index here is the
	// guarantee, this is the belt as well as the braces.
	IdempotencyKey string
}

// ChargeResult is the provider's answer.
//
// ProviderRef is set only when the charge settled, and FailureReason only when
// it was declined. Both being empty means the provider never answered, and that
// case always arrives with an error beside it.
type ChargeResult struct {
	Outcome       Outcome
	ProviderRef   string
	FailureReason string
}
