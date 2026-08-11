package payment_test

import (
    "context"
    "errors"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/identifier"
    "ottodot-trial-booking/backend/internal/payment"
)

// The contract every payment repository has to satisfy.
//
// The risk this file exists to remove: the fake reimplements the idempotency
// rule correctly while the sql is wrong, and every fast test stays green. One
// suite pointed at both implementations is the only thing that catches that, so
// this file is run twice, once against the memory repository in the fast tiers
// and once against real Postgres behind the containers tag.

// Identifiers used across the suite. They are fixed rather than minted so a
// failure names the same booking every time it is read.
const (
    bookingOne = "0192d000-0000-7000-8000-000000000001"
    bookingTwo = "0192d000-0000-7000-8000-000000000002"

    unknownIdentifier = "0192d000-0000-7000-8000-0000000000ff"
)

// The price used everywhere the amount is not the subject of the case. It ends
// in 00, so the mock provider settles it.
const contractPriceCents = 4500

// contractMoment is the instant every case starts from.
var contractMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// contractAmount is the ordinary charge, in the currency the column defaults to.
func contractAmount() payment.Amount {
    return payment.Amount{Cents: contractPriceCents, Currency: payment.DefaultCurrency}
}

// repositoryFixture is one repository with a way to put rows in front of it.
// The memory version writes into maps, the Postgres version inserts into a
// throwaway schema.
type repositoryFixture interface {
    Repository() payment.Repository
    AddBooking(t *testing.T, bookingID string)
}

// seedContractFixture puts the same two bookings in front of either
// implementation. Nothing here knows what class they are for, because this
// package never looks at a seat.
func seedContractFixture(t *testing.T, fixture repositoryFixture) {
    t.Helper()

    fixture.AddBooking(t, bookingOne)
    fixture.AddBooking(t, bookingTwo)
}

// newAttemptID mints an identifier in the same format the service uses, so the
// Postgres uuid columns accept it.
func newAttemptID(t *testing.T) string {
    t.Helper()

    minted, err := identifier.NewUUIDv7()
    if err != nil {
        t.Fatalf("cannot mint an identifier: %v", err)
    }

    return minted
}

// beginRequestFor builds an opening write with the suite's price already
// applied.
func beginRequestFor(t *testing.T, bookingID string, key string) payment.BeginRequest {
    t.Helper()

    return payment.BeginRequest{
        AttemptID:      newAttemptID(t),
        BookingID:      bookingID,
        IdempotencyKey: key,
        Amount:         contractAmount(),
        Now:            contractMoment,
    }
}

// mustBegin opens an attempt and fails the test if it was refused or if it
// turned out to be a replay when the case expected a new row.
func mustBegin(t *testing.T, repository payment.Repository, bookingID string, key string) payment.Attempt {
    t.Helper()

    opened, replayed, err := repository.Begin(context.Background(), beginRequestFor(t, bookingID, key))
    if err != nil {
        t.Fatalf("expected an attempt for booking %s, got: %v", bookingID, err)
    }

    if replayed {
        t.Fatalf("expected a new attempt for key %s, got a replay", key)
    }

    return opened
}

// runRepositoryContract is the suite itself. Every case gets its own fixture,
// so nothing leaks from one to the next.
func runRepositoryContract(t *testing.T, newFixture func(t *testing.T) repositoryFixture) {
    t.Helper()

    ctx := context.Background()

    t.Run("integration: the first call writes one initiated attempt", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-first")

        if opened.Status != payment.StatusInitiated {
            t.Fatalf("expected initiated, got %s", opened.Status)
        }

        if !opened.SettledAt.IsZero() {
            t.Fatalf("an attempt nobody answered carries no settled instant, got %s", opened.SettledAt)
        }

        if opened.Amount.Cents != contractPriceCents || opened.Amount.Currency != payment.DefaultCurrency {
            t.Fatalf("the amount was not stored as it was asked for: %+v", opened.Amount)
        }
    })

    t.Run("integration: an attempt reads back by its id and by its key", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-readable")

        byID, err := repository.Attempt(ctx, opened.ID)
        if err != nil {
            t.Fatalf("expected to read the attempt back, got: %v", err)
        }

        byKey, err := repository.AttemptByKey(ctx, bookingOne, "key-readable")
        if err != nil {
            t.Fatalf("expected to find the attempt by its key, got: %v", err)
        }

        if byID.ID != opened.ID || byKey.ID != opened.ID {
            t.Fatalf("the two reads do not name the same attempt: %s and %s", byID.ID, byKey.ID)
        }
    })

    t.Run("edge: the same key returns the original attempt and reports a replay", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-replayed")

        second, replayed, err := repository.Begin(ctx, beginRequestFor(t, bookingOne, "key-replayed"))
        if err != nil {
            t.Fatalf("a replay is not a failure, got: %v", err)
        }

        if !replayed {
            t.Fatal("the second call with one key must be reported as a replay")
        }

        if second.ID != opened.ID {
            t.Fatalf("a replay must hand back the original attempt, got %s instead of %s", second.ID, opened.ID)
        }

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 1 {
            t.Fatalf("expected exactly one row after a replay, got %d", len(stored))
        }
    })

    t.Run("edge: the same key for a different amount is refused", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        mustBegin(t, repository, bookingOne, "key-conflict")

        different := beginRequestFor(t, bookingOne, "key-conflict")
        different.Amount = payment.Amount{Cents: contractPriceCents * 2, Currency: payment.DefaultCurrency}

        _, _, err := repository.Begin(ctx, different)
        if !errors.Is(err, payment.ErrIdempotencyConflict) {
            t.Fatalf("expected ErrIdempotencyConflict, got: %v", err)
        }
    })

    t.Run("edge: one key belongs to one booking, so another booking may reuse it", func(t *testing.T) {
        // The index is on (booking_id, idempotency_key), not on the key alone.
        // Two parents picking the same key must not collide.
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        mustBegin(t, repository, bookingOne, "key-shared")
        mustBegin(t, repository, bookingTwo, "key-shared")
    })

    t.Run("edge: a zero or negative amount never reaches a row", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        for _, cents := range []int32{0, -1, -contractPriceCents} {
            refused := beginRequestFor(t, bookingOne, "key-amount")
            refused.Amount = payment.Amount{Cents: cents, Currency: payment.DefaultCurrency}

            if _, _, err := repository.Begin(ctx, refused); !errors.Is(err, payment.ErrInvalidAmount) {
                t.Fatalf("expected ErrInvalidAmount for %d cents, got: %v", cents, err)
            }
        }

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 0 {
            t.Fatalf("a refused amount must leave no row behind, got %d", len(stored))
        }
    })

    t.Run("edge: a malformed idempotency key never reaches a row", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        for _, key := range []string{"", " ", "with space", strings.Repeat("k", payment.MaxIdempotencyKeyLength+1)} {
            if _, _, err := repository.Begin(ctx, beginRequestFor(t, bookingOne, key)); !errors.Is(err, payment.ErrInvalidIdempotencyKey) {
                t.Fatalf("expected ErrInvalidIdempotencyKey for %q, got: %v", key, err)
            }
        }

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 0 {
            t.Fatalf("a refused key must leave no row behind, got %d", len(stored))
        }
    })

    t.Run("edge: a charge against a booking that does not exist is refused", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        _, _, err := repository.Begin(ctx, beginRequestFor(t, unknownIdentifier, "key-orphan"))
        if !errors.Is(err, payment.ErrBookingNotFound) {
            t.Fatalf("expected ErrBookingNotFound, got: %v", err)
        }
    })

    t.Run("integration: settling records the answer and closes the attempt", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-settled")

        settled, err := repository.Settle(ctx, payment.SettleRequest{
            AttemptID:   opened.ID,
            Status:      payment.StatusSucceeded,
            ProviderRef: "mock_" + opened.ID,
            Now:         contractMoment,
        })
        if err != nil {
            t.Fatalf("expected the attempt to settle, got: %v", err)
        }

        if settled.Status != payment.StatusSucceeded || settled.SettledAt.IsZero() {
            t.Fatalf("a settled attempt carries its status and its instant, got %+v", settled)
        }

        stored, err := repository.Attempt(ctx, opened.ID)
        if err != nil {
            t.Fatalf("expected to read the settled attempt back, got: %v", err)
        }

        if stored.ProviderRef == "" {
            t.Fatal("a settled attempt keeps the provider reference, and nothing else from the provider")
        }
    })

    t.Run("edge: a declined attempt keeps a reason and no provider reference", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-declined")

        declined, err := repository.Settle(ctx, payment.SettleRequest{
            AttemptID:     opened.ID,
            Status:        payment.StatusFailed,
            FailureReason: "the provider declined this charge",
            Now:           contractMoment,
        })
        if err != nil {
            t.Fatalf("expected the decline to be recorded, got: %v", err)
        }

        if declined.Status != payment.StatusFailed || declined.ProviderRef != "" {
            t.Fatalf("a decline moved no money, so it carries no provider reference: %+v", declined)
        }

        if declined.FailureReason == "" {
            t.Fatal("a decline has to say why, for an operator")
        }
    })

    t.Run("edge: an attempt that already settled cannot be written again", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-append-only")

        if _, err := repository.Settle(ctx, payment.SettleRequest{
            AttemptID: opened.ID, Status: payment.StatusSucceeded, ProviderRef: "mock_" + opened.ID, Now: contractMoment,
        }); err != nil {
            t.Fatalf("expected the first settle to succeed, got: %v", err)
        }

        _, err := repository.Settle(ctx, payment.SettleRequest{
            AttemptID: opened.ID, Status: payment.StatusFailed, FailureReason: "changed answer", Now: contractMoment,
        })
        if !errors.Is(err, payment.ErrAlreadySettled) {
            t.Fatalf("expected ErrAlreadySettled, got: %v", err)
        }

        stored, err := repository.Attempt(ctx, opened.ID)
        if err != nil {
            t.Fatalf("expected to read the attempt back, got: %v", err)
        }

        if stored.Status != payment.StatusSucceeded {
            t.Fatalf("the first answer must survive the second write, got %s", stored.Status)
        }
    })

    t.Run("edge: a settle that describes no real outcome is refused", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        opened := mustBegin(t, repository, bookingOne, "key-shapes")

        refusals := []payment.SettleRequest{
            {AttemptID: opened.ID, Status: payment.StatusInitiated, Now: contractMoment},
            {AttemptID: opened.ID, Status: payment.StatusSucceeded, Now: contractMoment},
            {AttemptID: opened.ID, Status: payment.StatusFailed, ProviderRef: "mock_paid", Now: contractMoment},
        }

        for _, refused := range refusals {
            if _, err := repository.Settle(ctx, refused); !errors.Is(err, payment.ErrInvalidRequest) {
                t.Fatalf("expected ErrInvalidRequest for %+v, got: %v", refused, err)
            }
        }
    })

    t.Run("edge: attempts for one booking come back oldest first", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        first := mustBegin(t, repository, bookingOne, "key-order-1")
        second := mustBegin(t, repository, bookingOne, "key-order-2")

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 2 || stored[0].ID != first.ID || stored[1].ID != second.ID {
            t.Fatalf("expected the two attempts in the order they were written, got %+v", stored)
        }
    })

    t.Run("edge: unknown identifiers are reported as not found", func(t *testing.T) {
        fixture := newFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        if _, err := repository.Attempt(ctx, unknownIdentifier); !errors.Is(err, payment.ErrAttemptNotFound) {
            t.Fatalf("expected ErrAttemptNotFound, got: %v", err)
        }

        if _, err := repository.AttemptByKey(ctx, bookingOne, "key-never-used"); !errors.Is(err, payment.ErrAttemptNotFound) {
            t.Fatalf("expected an unused key to be reported as not found, got: %v", err)
        }

        _, err := repository.Settle(ctx, payment.SettleRequest{
            AttemptID: unknownIdentifier, Status: payment.StatusSucceeded, ProviderRef: "mock_none", Now: contractMoment,
        })
        if !errors.Is(err, payment.ErrAttemptNotFound) {
            t.Fatalf("expected ErrAttemptNotFound when settling nothing, got: %v", err)
        }
    })
}
