package payment

import (
    "context"
    "time"
)

// Repository is the only way this package reaches storage.
//
// It exists as an interface for one reason: the four fast test tiers run
// against a fake, and the same behaviour suite runs against real Postgres in
// the proof tier. A fake that quietly disagrees with the sql is the risk the
// shared suite exists to catch.
//
// The table is append only after settlement. A row is written once by Begin and
// finished once by Settle, and nothing writes to it again.
type Repository interface {
    // Attempt reads one attempt. It reports ErrAttemptNotFound when there is
    // none.
    Attempt(ctx context.Context, attemptID string) (Attempt, error)

    // AttemptByKey reads the attempt an idempotency key already produced for a
    // booking. It reports ErrAttemptNotFound when the key is unused, which is
    // the ordinary first-call answer rather than a failure.
    AttemptByKey(ctx context.Context, bookingID string, idempotencyKey string) (Attempt, error)

    // AttemptsFor lists every attempt against one booking, oldest first. It is
    // what proves a replay produced one row rather than two.
    AttemptsFor(ctx context.Context, bookingID string) ([]Attempt, error)

    // Begin writes a new attempt in initiated, or reports the one an earlier
    // call with the same key already wrote.
    //
    // The write is what the uq_payment_idempotency index guards, so two calls
    // racing with one key produce one row and the loser is handed the winner's.
    //
    // Return:
    //   - the attempt, freshly written or the one already there
    //   - true when this is a replay, so the caller must not charge again
    //   - ErrIdempotencyConflict when the key was used for a different amount
    Begin(ctx context.Context, request BeginRequest) (Attempt, bool, error)

    // Settle records the provider's answer against an initiated attempt.
    //
    // Return:
    //   - the settled attempt
    //   - ErrAlreadySettled when the provider already answered for it, because
    //     the row is append only from that point
    Settle(ctx context.Context, request SettleRequest) (Attempt, error)
}

// BeginRequest is everything the opening write needs.
//
// The instant and the identifier are handed in rather than read inside the
// repository, so both implementations answer identically and a test can pin the
// clock instead of sleeping.
type BeginRequest struct {
    // AttemptID is minted by the service, so the caller knows the id even if
    // the write then loses the idempotency race.
    AttemptID string

    BookingID string

    // IdempotencyKey is what makes a replay recognisable, and what the unique
    // index is on.
    IdempotencyKey string

    Amount Amount

    // Now stamps created_at.
    Now time.Time
}

// SettleRequest is everything the closing write needs.
type SettleRequest struct {
    AttemptID string

    // Status is succeeded or failed. Initiated is refused, because settling is
    // what leaves that status behind.
    Status Status

    // ProviderRef is set when the charge settled, and empty otherwise.
    ProviderRef string

    // FailureReason is set when the charge was declined, and empty otherwise.
    // It is written for an operator, so it carries no identifier and no name.
    FailureReason string

    // Now stamps settled_at.
    Now time.Time
}

// Validate refuses a settle request that would write a row nobody can read
// honestly, before either implementation touches storage.
//
// Return:
//   - nil when the request describes one of the two settled shapes
//   - ErrInvalidRequest otherwise
func (request SettleRequest) Validate() error {
    if request.AttemptID == "" || !request.Status.IsSettled() {
        return ErrInvalidRequest
    }

    if request.Status == StatusSucceeded && request.ProviderRef == "" {
        return ErrInvalidRequest
    }

    if request.Status == StatusFailed && request.ProviderRef != "" {
        return ErrInvalidRequest
    }

    return nil
}
