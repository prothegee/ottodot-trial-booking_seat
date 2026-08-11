package payment

import (
    "context"
    "sync"

    "ottodot-trial-booking/backend/internal/faults"
)

// How the mock decides, when nothing has been forced for a reference.
//
// The rule is the last two digits of the amount, so a reviewer can produce any
// of the three answers with a number instead of an account:
//
//	amount cents ending in 01   declined
//	amount cents ending in 02   the provider cannot be reached
//	anything else               settled
//
// The seeded price is 4500, which settles. Paying 4501 for the same class is a
// decline that anyone can reproduce, in the demo or in a test, with no external
// service involved and no randomness to make it a different story next time.
const (
    outcomeAmountModulus    = 100
    declinedAmountEnding    = 1
    unreachableAmountEnding = 2
)

// mockProviderRefPrefix marks a reference as this service's own invention, so a
// row carrying one is never mistaken for something a real provider returned.
const mockProviderRefPrefix = "mock_"

// mockRefundRefPrefix does the same for a refund, and is a different prefix so
// the two references can never be read as each other.
const mockRefundRefPrefix = "mock_refund_"

// declineReason is what an operator sees on a declined attempt. Like every
// string in this package it carries no identifier, no name, and no amount.
const declineReason = "the provider declined this charge"

// MockProvider is the provider every test and every demo runs against.
//
// It is deterministic on purpose. A provider that fails at random makes a
// failing test a coin toss and makes a recorded demonstration a matter of luck,
// so this one decides from the request alone and answers the same way every
// time.
type MockProvider struct {
    mutex  sync.Mutex
    forced map[string]Outcome
    fault  Fault

    charges int
    refunds int

    // settledRefunds is the mock's own idempotency store, keyed the way a real
    // provider keys one. A refund has no unique index on this side, so this map
    // is what a replayed reconciliation job runs into.
    settledRefunds map[string]RefundResult
}

// NewMockProvider builds a provider that follows the amount rule above.
func NewMockProvider() *MockProvider {
    return &MockProvider{
        forced:         make(map[string]Outcome),
        settledRefunds: make(map[string]RefundResult),
    }
}

// InjectFaults points this provider at a fault source.
//
// It is separate from ForceOutcome, which pins an answer for one reference. This
// one is armed from outside the process, over the guarded fault route, and it
// applies to whichever charge happens to arrive next. That difference is the
// whole reason both exist: a test knows which booking it is about to charge, and
// somebody recording a demonstration does not.
func (provider *MockProvider) InjectFaults(fault Fault) {
    provider.mutex.Lock()
    defer provider.mutex.Unlock()

    provider.fault = fault
}

// ForceOutcome pins the answer for one reference, whatever its amount.
//
// It is what a seeded scenario uses when the amount is fixed by the class and
// the outcome still has to be a decline. The pin lasts until it is overwritten,
// and it is still deterministic: the same reference gets the same answer every
// time it is charged.
//
// Param:
// reference - string (the booking the charge is for)
// outcome - Outcome (which of the three answers this reference gets)
//
// Return:
//   - nil once the answer is pinned
//   - ErrInvalidRequest for an outcome the provider cannot give
func (provider *MockProvider) ForceOutcome(reference string, outcome Outcome) error {
    if reference == "" || !outcome.IsKnown() {
        return ErrInvalidRequest
    }

    provider.mutex.Lock()
    defer provider.mutex.Unlock()

    provider.forced[reference] = outcome

    return nil
}

// Charges is how many times this provider was asked to move money.
//
// It is the mock's own record of what it did, and it is what proves a replayed
// idempotency key produced one charge rather than two.
func (provider *MockProvider) Charges() int {
    provider.mutex.Lock()
    defer provider.mutex.Unlock()

    return provider.charges
}

// Refunds is how many times this provider actually sent money back.
//
// It counts movements, not calls. A replayed key is answered from the store and
// is not counted, which is what makes this number the thing that proves a
// reconciliation job run twice refunded once.
func (provider *MockProvider) Refunds() int {
    provider.mutex.Lock()
    defer provider.mutex.Unlock()

    return provider.refunds
}

// Charge answers the way the rule above says, and counts the call.
//
// Note:
//   - the unreachable answer returns ErrProviderUnavailable rather than a
//     decline, because the two mean opposite things to a booking. This is the
//     seam phase 7 arms as `payment.provider_error`, so the path stays reachable
//     without a fault registry existing yet.
func (provider *MockProvider) Charge(_ context.Context, request ChargeRequest) (ChargeResult, error) {
    if request.AttemptID == "" || request.Reference == "" {
        return ChargeResult{}, ErrInvalidRequest
    }

    if err := request.Amount.Validate(); err != nil {
        return ChargeResult{}, err
    }

    provider.mutex.Lock()
    forced, pinned := provider.forced[request.Reference]
    fault := provider.fault
    provider.charges++
    provider.mutex.Unlock()

    outcome := DecideOutcome(request.Amount)
    if pinned {
        outcome = forced
    }

    // Injection point: the provider cannot be reached. The call is counted
    // before this, on purpose, because a charge that left this service and got
    // no answer is exactly the case where nobody knows whether money moved.
    if fault.triggered(faults.PointPaymentProviderError) {
        outcome = OutcomeProviderError
    }

    switch outcome {
    case OutcomeDeclined:
        return ChargeResult{Outcome: OutcomeDeclined, FailureReason: declineReason}, nil
    case OutcomeProviderError:
        return ChargeResult{Outcome: OutcomeProviderError}, ErrProviderUnavailable
    }

    return ChargeResult{
        Outcome:     OutcomeSettled,
        ProviderRef: mockProviderRefPrefix + request.AttemptID,
    }, nil
}

// Refund sends a settled charge back, once per key.
//
// Note:
//   - the key is honoured the way a real provider honours one. A second call
//     with the same key gets the first call's answer and moves no money, which
//     is what stops a reconciliation job that failed halfway from refunding
//     twice on its retry.
//   - the forced outcome applies here too, and only one of the three values
//     means anything: a reference pinned to OutcomeProviderError cannot be
//     refunded either, which is how a test reaches the retry path. A pinned
//     decline is ignored, because a refund has no declined answer to give.
func (provider *MockProvider) Refund(_ context.Context, request RefundRequest) (RefundResult, error) {
    if request.ProviderRef == "" || request.Reference == "" || request.IdempotencyKey == "" {
        return RefundResult{}, ErrInvalidRequest
    }

    if err := request.Amount.Validate(); err != nil {
        return RefundResult{}, err
    }

    provider.mutex.Lock()
    defer provider.mutex.Unlock()

    if already, replayed := provider.settledRefunds[request.IdempotencyKey]; replayed {
        return already, nil
    }

    if forced, pinned := provider.forced[request.Reference]; pinned && forced == OutcomeProviderError {
        // Nothing is stored, so a later call with the same key is a fresh
        // attempt rather than a replay of a refund that never happened.
        return RefundResult{}, ErrProviderUnavailable
    }

    settled := RefundResult{RefundRef: mockRefundRefPrefix + request.ProviderRef}

    provider.settledRefunds[request.IdempotencyKey] = settled
    provider.refunds++

    return settled, nil
}

// DecideOutcome is the amount rule as a pure function.
//
// It is exported so the rule can be read, and tested, without a provider or a
// context. Everything the mock does follows from this one answer.
//
// Param:
// amount - Amount (the charge being asked for)
//
// Return:
//   - OutcomeDeclined for an amount ending in 01
//   - OutcomeProviderError for an amount ending in 02
//   - OutcomeSettled for every other amount
func DecideOutcome(amount Amount) Outcome {
    switch amount.Cents % outcomeAmountModulus {
    case declinedAmountEnding:
        return OutcomeDeclined
    case unreachableAmountEnding:
        return OutcomeProviderError
    }

    return OutcomeSettled
}
