package payment

import (
	"context"
	"time"
)

// Refund is one charge sent back.
//
// It has no table. The reason is worth stating rather than leaving as a gap: a
// refund here reverses exactly one settled charge, and the row it reverses
// already records the amount, the currency, and the provider reference. A
// second table would hold a copy of all three and one new field, and two places
// holding the same numbers is how they come to disagree.
//
// What the refund adds is written where an operator looks for it: a line in the
// booking's audit trail, carrying the refund reference, put there by the caller
// that also moves the booking to cancelled.
type Refund struct {
	// AttemptID is the charge that was reversed.
	AttemptID string

	// ProviderRef is the provider's identifier for that charge.
	ProviderRef string

	// RefundRef is the provider's identifier for the refund itself. It is what
	// an operator quotes when a parent asks where their money is.
	RefundRef string

	Amount Amount

	RefundedAt time.Time
}

// RefundCommand asks for the settled charge on one booking to be sent back.
//
// It names a booking rather than an attempt on purpose. The caller is a
// reconciliation job that knows a parent paid and lost the seat, and finding
// which of that booking's attempts actually settled is this package's job, not
// the worker's.
type RefundCommand struct {
	BookingID string
}

// Refund sends back the settled charge against one booking.
//
// Note:
//   - the key is what makes this safe to call twice. There is no refund row to
//     check, so a second call does reach the provider, and the provider is what
//     recognises it as the same refund and moves nothing. That is weaker than
//     the index guarding a charge, and it is stated here rather than left to be
//     discovered: this path depends on the provider honouring the key.
//   - the case that makes it matter is not a parent clicking twice. It is the
//     reconciliation job refunding, failing to close the booking, and being
//     retried. The booking still says refund_required, so the retry comes
//     straight back here, and without the key it would send the money twice.
//   - a provider that cannot be reached returns ErrProviderUnavailable and
//     nothing is written. The job is released and tried again, which is the
//     only safe answer when nobody knows whether the money moved.
//
// Param:
// ctx - context.Context (cancelling it abandons the call to the provider)
// command - RefundCommand (which booking is being made whole)
//
// Return:
//   - the refund, carrying the provider's reference for it
//   - ErrNothingToRefund when no attempt against this booking ever settled
//   - ErrProviderUnavailable when the provider could not be reached
func (service *Service) Refund(ctx context.Context, command RefundCommand) (Refund, error) {
	if command.BookingID == "" {
		return Refund{}, ErrInvalidRequest
	}

	settled, err := service.settledAttempt(ctx, command.BookingID)
	if err != nil {
		return Refund{}, err
	}

	answer, err := service.provider.Refund(ctx, RefundRequest{
		ProviderRef:    settled.ProviderRef,
		Reference:      settled.BookingID,
		Amount:         settled.Amount,
		IdempotencyKey: RefundKeyFor(settled.ID),
	})
	if err != nil {
		return Refund{}, err
	}

	return Refund{
		AttemptID:   settled.ID,
		ProviderRef: settled.ProviderRef,
		RefundRef:   answer.RefundRef,
		Amount:      settled.Amount,
		RefundedAt:  service.settings.Clock(),
	}, nil
}

// refundKeyPrefix keeps a refund key from ever being mistaken for the charge
// key it was derived from.
const refundKeyPrefix = "refund_"

// RefundKeyFor is the key a given attempt's refund always carries.
//
// It is a pure function of the attempt, which is the whole point: the job that
// reverses a charge can run any number of times, on any worker, after any
// restart, and it produces the same key every time. Anything with randomness or
// a clock in it would produce a new key per run and guard nothing.
//
// It is exported so a test can assert the key without repeating the rule.
//
// Param:
// attemptID - string (the settled charge being reversed)
//
// Return:
//   - the key, prefixed so it cannot collide with a charge key
func RefundKeyFor(attemptID string) string {
	return refundKeyPrefix + attemptID
}

// settledAttempt finds the one charge against a booking that moved money.
//
// There can only be one, because a booking is paid once and the idempotency
// index stops a key charging twice. The loop still walks every attempt, since a
// booking that was declined before it settled carries more than one row and
// only the last of them is the charge.
func (service *Service) settledAttempt(ctx context.Context, bookingID string) (Attempt, error) {
	attempts, err := service.repository.AttemptsFor(ctx, bookingID)
	if err != nil {
		return Attempt{}, err
	}

	for _, attempt := range attempts {
		if attempt.Status == StatusSucceeded && attempt.ProviderRef != "" {
			return attempt, nil
		}
	}

	return Attempt{}, ErrNothingToRefund
}
