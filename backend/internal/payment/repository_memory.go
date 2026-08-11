package payment

import (
    "context"
    "sync"
)

// MemoryRepository is the fake every fast test runs against.
//
// It holds the same invariants the database holds, under one mutex instead of a
// unique index. That is the point of it: the four fast tiers can prove a replay
// charges once, in a second, with nothing running.
//
// What it cannot prove is that two real transactions racing on one idempotency
// key produce one row, because there is no transaction here. That is why the
// same behaviour suite also runs against Postgres in the proof tier.
type MemoryRepository struct {
    mutex sync.Mutex

    // bookings stands in for the foreign key. Without it the fake would accept
    // a charge against a booking that does not exist and the sql would not.
    bookings map[string]struct{}

    attempts map[string]Attempt

    // order keeps the insertion sequence, so AttemptsFor answers oldest first
    // the way the sql does.
    order []string
}

// NewMemoryRepository builds an empty repository. Bookings are put in with
// AddBooking, because a fake with rows already in it hides what a test depends
// on.
func NewMemoryRepository() *MemoryRepository {
    return &MemoryRepository{
        bookings: make(map[string]struct{}),
        attempts: make(map[string]Attempt),
    }
}

// AddBooking puts a booking in front of the repository. It stands in for the
// row the booking_id foreign key points at, and it is the only thing this
// repository knows about a booking: no status, no seat, no class.
func (repository *MemoryRepository) AddBooking(bookingID string) {
    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    repository.bookings[bookingID] = struct{}{}
}

// Attempt reads one attempt.
func (repository *MemoryRepository) Attempt(_ context.Context, attemptID string) (Attempt, error) {
    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    stored, found := repository.attempts[attemptID]
    if !found {
        return Attempt{}, ErrAttemptNotFound
    }

    return stored, nil
}

// AttemptByKey reads the attempt an idempotency key already produced.
func (repository *MemoryRepository) AttemptByKey(_ context.Context, bookingID string, idempotencyKey string) (Attempt, error) {
    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    stored, found := repository.attemptByKeyLocked(bookingID, idempotencyKey)
    if !found {
        return Attempt{}, ErrAttemptNotFound
    }

    return stored, nil
}

// AttemptsFor lists every attempt against one booking, oldest first.
func (repository *MemoryRepository) AttemptsFor(_ context.Context, bookingID string) ([]Attempt, error) {
    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    var found []Attempt

    for _, attemptID := range repository.order {
        stored := repository.attempts[attemptID]

        if stored.BookingID == bookingID {
            found = append(found, stored)
        }
    }

    return found, nil
}

// Begin writes a new attempt, or reports the one this key already produced.
//
// The key is checked under the same lock the write happens under, which is the
// fake's stand-in for the unique index doing it inside one transaction.
func (repository *MemoryRepository) Begin(_ context.Context, request BeginRequest) (Attempt, bool, error) {
    if request.AttemptID == "" || request.BookingID == "" {
        return Attempt{}, false, ErrInvalidRequest
    }

    if err := ValidateIdempotencyKey(request.IdempotencyKey); err != nil {
        return Attempt{}, false, err
    }

    // Mirrors `check (amount_cents > 0)`. The service refuses this earlier, and
    // both refusals exist for the same reason the database check does: a path
    // that skips the service must not be able to write a charge of nothing.
    if err := request.Amount.Validate(); err != nil {
        return Attempt{}, false, err
    }

    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    if _, known := repository.bookings[request.BookingID]; !known {
        return Attempt{}, false, ErrBookingNotFound
    }

    if stored, found := repository.attemptByKeyLocked(request.BookingID, request.IdempotencyKey); found {
        if !stored.Amount.SameAs(request.Amount) {
            return Attempt{}, false, ErrIdempotencyConflict
        }

        return stored, true, nil
    }

    opened := Attempt{
        ID:             request.AttemptID,
        BookingID:      request.BookingID,
        IdempotencyKey: request.IdempotencyKey,
        Amount:         request.Amount.Normalised(),
        Status:         StatusInitiated,
        CreatedAt:      request.Now,
    }

    repository.attempts[opened.ID] = opened
    repository.order = append(repository.order, opened.ID)

    return opened, false, nil
}

// Settle records the provider's answer. It is the second and last write an
// attempt row ever takes.
func (repository *MemoryRepository) Settle(_ context.Context, request SettleRequest) (Attempt, error) {
    if err := request.Validate(); err != nil {
        return Attempt{}, err
    }

    repository.mutex.Lock()
    defer repository.mutex.Unlock()

    stored, found := repository.attempts[request.AttemptID]
    if !found {
        return Attempt{}, ErrAttemptNotFound
    }

    if stored.Status.IsSettled() {
        return stored, ErrAlreadySettled
    }

    settled := stored
    settled.Status = request.Status
    settled.ProviderRef = request.ProviderRef
    settled.FailureReason = request.FailureReason
    settled.SettledAt = request.Now

    repository.attempts[settled.ID] = settled

    return settled, nil
}

// attemptByKeyLocked mirrors the uq_payment_idempotency index: one attempt per
// booking per key.
func (repository *MemoryRepository) attemptByKeyLocked(bookingID string, idempotencyKey string) (Attempt, bool) {
    for _, attemptID := range repository.order {
        stored := repository.attempts[attemptID]

        if stored.BookingID == bookingID && stored.IdempotencyKey == idempotencyKey {
            return stored, true
        }
    }

    return Attempt{}, false
}
