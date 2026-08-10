package payment

import (
	"context"
	"errors"
	"time"

	"ottodot-trial-booking/backend/internal/identifier"
)

// Settings are the two things the service needs handed to it rather than
// reaching for. Both default to the real ones.
type Settings struct {
	// Clock is where every stamped time comes from. Nil means the real clock. A
	// test sets it so a settled instant is exact rather than approximately
	// right.
	Clock func() time.Time

	// NewAttemptID mints the identifier for a new attempt. Nil means UUIDv7.
	NewAttemptID func() (string, error)
}

// Service turns a request to pay into at most one charge.
//
// It owns the ordering and nothing else. The invariant that a key charges once
// lives in the repository, on a unique index, because that is the only place it
// can hold while two requests arrive together.
//
// This package knows nothing about seats. The charge settles here first, and
// the seat is decided afterwards by the booking package, wired together one
// layer up. That ordering is why a parent can be charged and still lose a seat,
// and why refund_required exists over there rather than here.
type Service struct {
	repository Repository
	provider   Provider
	settings   Settings
}

// PayCommand is a parent settling one booking.
type PayCommand struct {
	BookingID string
	Amount    Amount

	// IdempotencyKey comes from the request header. The same key twice is a
	// retry, and it must produce the first answer again rather than a second
	// charge.
	IdempotencyKey string
}

// NewService builds the service and fills in whatever the settings left out.
//
// Param:
// repository - Repository (the real one in production, the fake in the fast tiers)
// provider - Provider (the mock everywhere, until a real one exists)
// settings - Settings (the two injection points a test needs)
//
// Return:
//   - the service, ready to use
//   - an error when a dependency is missing, refused at construction rather
//     than at the first charge
func NewService(repository Repository, provider Provider, settings Settings) (*Service, error) {
	if repository == nil {
		return nil, errors.New("payment: the service needs a repository")
	}

	if provider == nil {
		return nil, errors.New("payment: the service needs a provider")
	}

	if settings.Clock == nil {
		settings.Clock = time.Now
	}

	if settings.NewAttemptID == nil {
		settings.NewAttemptID = identifier.NewUUIDv7
	}

	return &Service{repository: repository, provider: provider, settings: settings}, nil
}

// Pay settles one booking, once.
//
// The order is deliberate and each step exists to stop one thing:
//
//	validate       a charge of nothing never reaches the driver
//	begin          the row is written before the provider is called, so a
//	               replay has something to find even if this process dies
//	charge         the provider answers, or does not
//	settle         the answer is recorded, and the row is closed
//
// Note:
//   - a replay never reaches the provider. The stored answer is returned again,
//     which is what makes two identical calls produce one charge and the same
//     result.
//   - an unreachable provider leaves the attempt initiated on purpose. Nobody
//     knows whether money moved, so writing failed would be a guess, and the
//     booking must keep the status it had.
//
// Return:
//   - the settled attempt and nil, when money moved
//   - the failed attempt and ErrDeclined, when the provider said no. No money
//     moved, so the booking is free to move to payment_failed
//   - the initiated attempt and ErrProviderUnavailable, when the provider never
//     answered
//   - ErrInvalidAmount, ErrInvalidCurrency, ErrInvalidIdempotencyKey, or
//     ErrInvalidRequest, each refused before anything is written
func (service *Service) Pay(ctx context.Context, command PayCommand) (Attempt, error) {
	if command.BookingID == "" {
		return Attempt{}, ErrInvalidRequest
	}

	if err := command.Amount.Validate(); err != nil {
		return Attempt{}, err
	}

	if err := ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return Attempt{}, err
	}

	attemptID, err := service.settings.NewAttemptID()
	if err != nil {
		return Attempt{}, err
	}

	opened, replayed, err := service.repository.Begin(ctx, BeginRequest{
		AttemptID:      attemptID,
		BookingID:      command.BookingID,
		IdempotencyKey: command.IdempotencyKey,
		Amount:         command.Amount,
		Now:            service.settings.Clock(),
	})
	if err != nil {
		return Attempt{}, err
	}

	if replayed {
		return replayOf(opened)
	}

	result, err := service.provider.Charge(ctx, ChargeRequest{
		AttemptID:      opened.ID,
		Reference:      opened.BookingID,
		Amount:         opened.Amount,
		IdempotencyKey: opened.IdempotencyKey,
	})
	if err != nil {
		return opened, err
	}

	if result.Outcome == OutcomeDeclined {
		declined, settleErr := service.repository.Settle(ctx, SettleRequest{
			AttemptID:     opened.ID,
			Status:        StatusFailed,
			FailureReason: result.FailureReason,
			Now:           service.settings.Clock(),
		})
		if settleErr != nil {
			return Attempt{}, settleErr
		}

		return declined, ErrDeclined
	}

	return service.repository.Settle(ctx, SettleRequest{
		AttemptID:   opened.ID,
		Status:      StatusSucceeded,
		ProviderRef: result.ProviderRef,
		Now:         service.settings.Clock(),
	})
}

// Attempt reads one attempt back.
func (service *Service) Attempt(ctx context.Context, attemptID string) (Attempt, error) {
	if attemptID == "" {
		return Attempt{}, ErrInvalidRequest
	}

	return service.repository.Attempt(ctx, attemptID)
}

// AttemptsFor lists every attempt against one booking, oldest first. It is what
// an operator screen reads, and what proves a replay wrote one row.
func (service *Service) AttemptsFor(ctx context.Context, bookingID string) ([]Attempt, error) {
	if bookingID == "" {
		return nil, ErrInvalidRequest
	}

	return service.repository.AttemptsFor(ctx, bookingID)
}

// replayOf hands back the answer an earlier call already got, so the second
// request reads exactly like the first.
//
// An attempt still sitting in initiated is the one case a replay cannot answer:
// the first call wrote its row and never came back, so nobody knows whether
// money moved. Charging again could charge twice, so the unfinished attempt is
// reported instead of guessed at.
func replayOf(stored Attempt) (Attempt, error) {
	switch stored.Status {
	case StatusSucceeded:
		return stored, nil
	case StatusFailed:
		return stored, ErrDeclined
	}

	return stored, ErrAttemptPending
}
