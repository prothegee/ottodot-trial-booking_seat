package payment_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/payment"
)

// settingsAt pins the clock, so a settled instant is exact rather than
// approximately right and no test has to sleep.
func settingsAt(moment time.Time) payment.Settings {
    return payment.Settings{Clock: func() time.Time { return moment }}
}

// newServiceFor builds a service over a fake repository holding one booking.
func newServiceFor(t *testing.T, bookingID string) (*payment.Service, *payment.MemoryRepository, *payment.MockProvider) {
    t.Helper()

    repository := payment.NewMemoryRepository()
    repository.AddBooking(bookingID)

    provider := payment.NewMockProvider()

    service, err := payment.NewService(repository, provider, settingsAt(contractMoment))
    if err != nil {
        t.Fatalf("expected the service to build, got: %v", err)
    }

    return service, repository, provider
}

// payCommandFor builds the ordinary charge for one booking.
func payCommandFor(bookingID string, key string, cents int32) payment.PayCommand {
    return payment.PayCommand{
        BookingID:      bookingID,
        Amount:         payment.Amount{Cents: cents, Currency: payment.DefaultCurrency},
        IdempotencyKey: key,
    }
}

func TestTheServiceRefusesWhatItCannotCharge(t *testing.T) {
    ctx := context.Background()

    t.Run("edge: a missing dependency is refused at construction", func(t *testing.T) {
        if _, err := payment.NewService(nil, payment.NewMockProvider(), payment.Settings{}); err == nil {
            t.Fatal("a service with no repository must not build")
        }

        if _, err := payment.NewService(payment.NewMemoryRepository(), nil, payment.Settings{}); err == nil {
            t.Fatal("a service with no provider must not build")
        }
    })

    t.Run("edge: nothing is written when the request cannot be charged", func(t *testing.T) {
        service, repository, provider := newServiceFor(t, bookingOne)

        refusals := []struct {
            name    string
            command payment.PayCommand
            want    error
        }{
            {"no booking", payment.PayCommand{Amount: contractAmount(), IdempotencyKey: "key-a"}, payment.ErrInvalidRequest},
            {"zero amount", payCommandFor(bookingOne, "key-b", 0), payment.ErrInvalidAmount},
            {"negative amount", payCommandFor(bookingOne, "key-c", -contractPriceCents), payment.ErrInvalidAmount},
            {"empty key", payCommandFor(bookingOne, "", contractPriceCents), payment.ErrInvalidIdempotencyKey},
            {"key with a space", payCommandFor(bookingOne, "not a key", contractPriceCents), payment.ErrInvalidIdempotencyKey},
        }

        for _, refusal := range refusals {
            if _, err := service.Pay(ctx, refusal.command); !errors.Is(err, refusal.want) {
                t.Fatalf("%s: expected %v, got: %v", refusal.name, refusal.want, err)
            }
        }

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 0 {
            t.Fatalf("a refused request writes nothing, got %d rows", len(stored))
        }

        if provider.Charges() != 0 {
            t.Fatalf("a refused request never reaches the provider, got %d charges", provider.Charges())
        }
    })

    t.Run("edge: a charge against a booking that does not exist is refused", func(t *testing.T) {
        service, _, provider := newServiceFor(t, bookingOne)

        _, err := service.Pay(ctx, payCommandFor(unknownIdentifier, "key-orphan", contractPriceCents))
        if !errors.Is(err, payment.ErrBookingNotFound) {
            t.Fatalf("expected ErrBookingNotFound, got: %v", err)
        }

        if provider.Charges() != 0 {
            t.Fatalf("the row is written before the provider is called, so nothing was charged: %d", provider.Charges())
        }
    })
}

func TestTheServiceRecordsWhatTheProviderAnswered(t *testing.T) {
    ctx := context.Background()

    t.Run("behaviour: a settled charge leaves one succeeded attempt", func(t *testing.T) {
        service, repository, provider := newServiceFor(t, bookingOne)

        settled, err := service.Pay(ctx, payCommandFor(bookingOne, "key-settled", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        if settled.Status != payment.StatusSucceeded || settled.ProviderRef == "" {
            t.Fatalf("a settled attempt names its charge: %+v", settled)
        }

        if !settled.SettledAt.Equal(contractMoment) {
            t.Fatalf("expected the settled instant to be the pinned clock, got %s", settled.SettledAt)
        }

        stored, err := repository.AttemptsFor(ctx, bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 1 {
            t.Fatalf("expected one row for one charge, got %d", len(stored))
        }

        if provider.Charges() != 1 {
            t.Fatalf("expected exactly one charge, got %d", provider.Charges())
        }
    })

    t.Run("behaviour: a decline is reported as ErrDeclined with the attempt beside it", func(t *testing.T) {
        service, _, _ := newServiceFor(t, bookingOne)

        declined, err := service.Pay(ctx, payCommandFor(bookingOne, "key-declined", contractPriceCents+1))
        if !errors.Is(err, payment.ErrDeclined) {
            t.Fatalf("expected ErrDeclined, got: %v", err)
        }

        if declined.Status != payment.StatusFailed {
            t.Fatalf("expected the attempt to be failed, got %s", declined.Status)
        }

        if declined.ProviderRef != "" {
            t.Fatalf("no money moved, so there is no charge to name: %q", declined.ProviderRef)
        }
    })

    t.Run("behaviour: an unreachable provider leaves the attempt initiated", func(t *testing.T) {
        // A decline and an unreachable provider are opposite answers. Writing
        // failed here would be a guess about money that may already have moved.
        service, repository, _ := newServiceFor(t, bookingOne)

        opened, err := service.Pay(ctx, payCommandFor(bookingOne, "key-unreachable", contractPriceCents+2))
        if !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected ErrProviderUnavailable, got: %v", err)
        }

        stored, err := repository.Attempt(ctx, opened.ID)
        if err != nil {
            t.Fatalf("expected the attempt to have been written before the call, got: %v", err)
        }

        if stored.Status != payment.StatusInitiated {
            t.Fatalf("expected the attempt to stay initiated, got %s", stored.Status)
        }
    })

    t.Run("edge: replaying a key whose first call never answered is refused", func(t *testing.T) {
        // The row is there and nobody knows what happened to the money.
        // Charging again could charge twice, so the unfinished attempt is
        // reported instead of guessed at.
        service, _, provider := newServiceFor(t, bookingOne)

        if _, err := service.Pay(ctx, payCommandFor(bookingOne, "key-stuck", contractPriceCents+2)); !errors.Is(err, payment.ErrProviderUnavailable) {
            t.Fatalf("expected the first call to fail on the provider, got: %v", err)
        }

        _, err := service.Pay(ctx, payCommandFor(bookingOne, "key-stuck", contractPriceCents+2))
        if !errors.Is(err, payment.ErrAttemptPending) {
            t.Fatalf("expected ErrAttemptPending, got: %v", err)
        }

        if provider.Charges() != 1 {
            t.Fatalf("the replay must not reach the provider, got %d charges", provider.Charges())
        }
    })

    t.Run("edge: a key already used for a different amount is refused", func(t *testing.T) {
        service, _, _ := newServiceFor(t, bookingOne)

        if _, err := service.Pay(ctx, payCommandFor(bookingOne, "key-conflict", contractPriceCents)); err != nil {
            t.Fatalf("expected the first charge to settle, got: %v", err)
        }

        _, err := service.Pay(ctx, payCommandFor(bookingOne, "key-conflict", contractPriceCents*2))
        if !errors.Is(err, payment.ErrIdempotencyConflict) {
            t.Fatalf("expected ErrIdempotencyConflict, got: %v", err)
        }
    })

    t.Run("integration: an attempt can be read back through the service", func(t *testing.T) {
        service, _, _ := newServiceFor(t, bookingOne)

        settled, err := service.Pay(ctx, payCommandFor(bookingOne, "key-readable", contractPriceCents))
        if err != nil {
            t.Fatalf("expected the charge to settle, got: %v", err)
        }

        stored, err := service.Attempt(ctx, settled.ID)
        if err != nil {
            t.Fatalf("expected to read the attempt back, got: %v", err)
        }

        if stored.ID != settled.ID {
            t.Fatalf("expected %s, got %s", settled.ID, stored.ID)
        }

        if _, err := service.Attempt(ctx, ""); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest for an empty id, got: %v", err)
        }

        if _, err := service.AttemptsFor(ctx, ""); !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest for an empty booking, got: %v", err)
        }
    })
}
