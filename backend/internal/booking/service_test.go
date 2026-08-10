package booking_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/booking"
)

// spyRepository records what the service asked for, so a test can assert that
// a refusal happened before storage was touched at all.
type spyRepository struct {
	holds    []booking.HoldRequest
	confirms []booking.ConfirmRequest
	cancels  []booking.CancelRequest
	expiries []booking.ExpireRequest
	seats    []int16
	class    booking.Class
}

func (spy *spyRepository) Class(_ context.Context, _ string) (booking.Class, error) {
	return spy.class, nil
}

func (spy *spyRepository) Booking(_ context.Context, _ string) (booking.Booking, error) {
	return booking.Booking{}, nil
}

func (spy *spyRepository) SeatsTaken(_ context.Context, _ string) ([]int16, error) {
	return spy.seats, nil
}

func (spy *spyRepository) Events(_ context.Context, _ string) ([]booking.Event, error) {
	return nil, nil
}

func (spy *spyRepository) Hold(_ context.Context, request booking.HoldRequest) (booking.Booking, error) {
	spy.holds = append(spy.holds, request)

	return booking.Booking{ID: request.BookingID, Status: booking.StatusPendingPayment}, nil
}

func (spy *spyRepository) Confirm(_ context.Context, request booking.ConfirmRequest) (booking.Booking, error) {
	spy.confirms = append(spy.confirms, request)

	return booking.Booking{ID: request.BookingID, Status: booking.StatusConfirmed}, nil
}

func (spy *spyRepository) Cancel(_ context.Context, request booking.CancelRequest) (booking.Booking, error) {
	spy.cancels = append(spy.cancels, request)

	return booking.Booking{ID: request.BookingID, Status: booking.StatusCancelled}, nil
}

func (spy *spyRepository) Expire(_ context.Context, request booking.ExpireRequest) (booking.Booking, error) {
	spy.expiries = append(spy.expiries, request)

	return booking.Booking{ID: request.BookingID, Status: booking.StatusExpired}, nil
}

// settingsAt builds settings with the clock pinned, so a deadline assertion is
// exact rather than approximately right.
func settingsAt(moment time.Time) booking.Settings {
	settings := booking.DefaultSettings()
	settings.Clock = func() time.Time { return moment }

	return settings
}

func TestTheServiceRefusesSettingsItCannotWorkWith(t *testing.T) {
	t.Run("unit: the defaults are usable", func(t *testing.T) {
		settings := booking.DefaultSettings()

		if settings.HoldTTL != 10*time.Minute {
			t.Fatalf("expected a ten minute hold, got %s", settings.HoldTTL)
		}

		if settings.MaxHoldsPerParent != 3 {
			t.Fatalf("expected a cap of 3, got %d", settings.MaxHoldsPerParent)
		}

		if _, err := booking.NewService(&spyRepository{}, settings); err != nil {
			t.Fatalf("the defaults must build a service, got: %v", err)
		}
	})

	t.Run("edge: a missing repository is refused at construction", func(t *testing.T) {
		if _, err := booking.NewService(nil, booking.DefaultSettings()); err == nil {
			t.Fatal("expected a service with no repository to be refused")
		}
	})

	t.Run("edge: a hold lifetime of zero is refused at construction", func(t *testing.T) {
		settings := booking.DefaultSettings()
		settings.HoldTTL = 0

		// Refusing here rather than at the first booking is the point: a zero
		// lifetime would grant holds that are already expired.
		if _, err := booking.NewService(&spyRepository{}, settings); err == nil {
			t.Fatal("expected a zero hold lifetime to be refused")
		}
	})

	t.Run("edge: a hold cap below one is refused at construction", func(t *testing.T) {
		settings := booking.DefaultSettings()
		settings.MaxHoldsPerParent = 0

		if _, err := booking.NewService(&spyRepository{}, settings); err == nil {
			t.Fatal("expected a cap of zero to be refused, it would block every parent")
		}
	})
}

func TestAHoldCarriesThePolicyToTheRepository(t *testing.T) {
	t.Run("unit: the deadline is the injected instant plus the lifetime", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Hold(context.Background(), booking.HoldCommand{
			StudentID: studentOne, ClassID: classOpen,
		}); err != nil {
			t.Fatalf("expected the hold to be passed on, got: %v", err)
		}

		if len(spy.holds) != 1 {
			t.Fatalf("expected exactly one request, got %d", len(spy.holds))
		}

		request := spy.holds[0]

		if !request.Now.Equal(contractMoment) {
			t.Fatalf("expected the injected instant, got %s", request.Now)
		}

		if !request.ExpiresAt.Equal(contractMoment.Add(10 * time.Minute)) {
			t.Fatalf("expected a deadline ten minutes out, got %s", request.ExpiresAt)
		}

		if request.MaxHoldsPerParent != 3 {
			t.Fatalf("the cap did not reach the repository, got %d", request.MaxHoldsPerParent)
		}
	})

	t.Run("unit: every hold gets its own identifier", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		for range 3 {
			if _, err := service.Hold(context.Background(), booking.HoldCommand{
				StudentID: studentOne, ClassID: classOpen,
			}); err != nil {
				t.Fatalf("expected the hold to be passed on, got: %v", err)
			}
		}

		seen := make(map[string]struct{}, len(spy.holds))

		for _, request := range spy.holds {
			if request.BookingID == "" {
				t.Fatal("a booking reached the repository with no identifier")
			}

			if _, repeated := seen[request.BookingID]; repeated {
				t.Fatalf("the same identifier was handed out twice: %s", request.BookingID)
			}

			seen[request.BookingID] = struct{}{}
		}
	})

	t.Run("edge: an identifier source that fails stops the booking", func(t *testing.T) {
		spy := &spyRepository{}

		settings := settingsAt(contractMoment)
		settings.NewBookingID = func() (string, error) {
			return "", errors.New("no randomness available")
		}

		service, err := booking.NewService(spy, settings)
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Hold(context.Background(), booking.HoldCommand{
			StudentID: studentOne, ClassID: classOpen,
		}); err == nil {
			t.Fatal("expected the failure to be reported rather than a booking with an empty id")
		}

		if len(spy.holds) != 0 {
			t.Fatal("nothing must reach the repository when no identifier could be minted")
		}
	})
}

func TestAnIncompleteRequestNeverReachesStorage(t *testing.T) {
	cases := []struct {
		name    string
		command booking.HoldCommand
	}{
		{name: "edge: a hold with no child is refused", command: booking.HoldCommand{ClassID: classOpen}},
		{name: "edge: a hold with no class is refused", command: booking.HoldCommand{StudentID: studentOne}},
		{name: "edge: an empty hold is refused", command: booking.HoldCommand{}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			spy := &spyRepository{}

			service, err := booking.NewService(spy, settingsAt(contractMoment))
			if err != nil {
				t.Fatalf("expected the service to build, got: %v", err)
			}

			if _, err := service.Hold(context.Background(), testCase.command); !errors.Is(err, booking.ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got: %v", err)
			}

			if len(spy.holds) != 0 {
				t.Fatal("an incomplete request must be refused before storage is touched")
			}
		})
	}

	t.Run("edge: confirming nothing is refused before storage is touched", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Confirm(context.Background(), ""); !errors.Is(err, booking.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}

		if len(spy.confirms) != 0 {
			t.Fatal("an empty identifier must not reach the repository")
		}
	})

	t.Run("edge: cancelling nothing is refused before storage is touched", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Cancel(context.Background(), "", booking.ActorParent, ""); !errors.Is(err, booking.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}

		if len(spy.cancels) != 0 {
			t.Fatal("an empty identifier must not reach the repository")
		}
	})

	t.Run("edge: expiring nothing is refused before storage is touched", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Expire(context.Background(), ""); !errors.Is(err, booking.ErrInvalidRequest) {
			t.Fatalf("expected ErrInvalidRequest, got: %v", err)
		}

		if len(spy.expiries) != 0 {
			t.Fatal("an empty identifier must not reach the repository")
		}
	})
}

func TestAnExpiryIsJudgedAgainstTheServiceClock(t *testing.T) {
	t.Run("unit: the instant handed to the repository is the service clock", func(t *testing.T) {
		// The worker never passes an instant of its own. If it did, a job
		// written an hour ago could be judged against an hour-old clock and
		// expire a hold that was refreshed since.
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Expire(context.Background(), unknownIdentifier); err != nil {
			t.Fatalf("expected the expiry to be passed on, got: %v", err)
		}

		if len(spy.expiries) != 1 {
			t.Fatalf("expected exactly one request, got %d", len(spy.expiries))
		}

		if !spy.expiries[0].Now.Equal(contractMoment) {
			t.Fatalf("expected the pinned clock, got %v", spy.expiries[0].Now)
		}
	})
}

func TestACancellationCarriesWhoDidItAndWhy(t *testing.T) {
	t.Run("unit: the actor and reason reach the repository", func(t *testing.T) {
		spy := &spyRepository{}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		if _, err := service.Cancel(context.Background(), unknownIdentifier, booking.ActorAdmin, "withdrawn by an operator"); err != nil {
			t.Fatalf("expected the cancel to be passed on, got: %v", err)
		}

		if len(spy.cancels) != 1 {
			t.Fatalf("expected exactly one request, got %d", len(spy.cancels))
		}

		if spy.cancels[0].Actor != booking.ActorAdmin {
			t.Fatalf("expected the admin actor, got %s", spy.cancels[0].Actor)
		}

		if spy.cancels[0].Reason != "withdrawn by an operator" {
			t.Fatalf("the reason did not reach the audit trail, got %q", spy.cancels[0].Reason)
		}
	})
}

func TestSeatsRemainingIsAdvisoryAndNeverNegative(t *testing.T) {
	t.Run("unit: it is capacity minus the seats already taken", func(t *testing.T) {
		spy := &spyRepository{
			class: booking.Class{ID: classOpen, Capacity: 4, HoldAllowance: 2},
			seats: []int16{1, 2},
		}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		remaining, err := service.SeatsRemaining(context.Background(), classOpen)
		if err != nil {
			t.Fatalf("expected a count, got: %v", err)
		}

		if remaining != 2 {
			t.Fatalf("expected 2 seats left, got %d", remaining)
		}
	})

	t.Run("edge: a full class reports zero, not a negative number", func(t *testing.T) {
		// A negative here would reach a screen and read as nonsense. It can
		// only happen if capacity were lowered under confirmed bookings, which
		// is exactly the kind of data change an operator makes at some point.
		spy := &spyRepository{
			class: booking.Class{ID: classOpen, Capacity: 2, HoldAllowance: 0},
			seats: []int16{1, 2, 3},
		}

		service, err := booking.NewService(spy, settingsAt(contractMoment))
		if err != nil {
			t.Fatalf("expected the service to build, got: %v", err)
		}

		remaining, err := service.SeatsRemaining(context.Background(), classOpen)
		if err != nil {
			t.Fatalf("expected a count, got: %v", err)
		}

		if remaining != 0 {
			t.Fatalf("expected 0 seats left, got %d", remaining)
		}
	})
}
