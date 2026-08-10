package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
)

func TestTheMemoryDenylist(t *testing.T) {
	ctx := context.Background()

	t.Run("unit: a token that was never withdrawn is not denied", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()

		denied, err := denylist.IsDenied(ctx, "some-token-id", claimsMoment)
		if err != nil {
			t.Fatalf("cannot read the denylist: %v", err)
		}

		if denied {
			t.Fatal("expected an untouched token id to pass")
		}
	})

	t.Run("unit: a withdrawn token is denied for the rest of its life", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()
		deadline := claimsMoment.Add(15 * time.Minute)

		if err := denylist.Deny(ctx, "withdrawn", deadline); err != nil {
			t.Fatalf("cannot withdraw a token: %v", err)
		}

		denied, err := denylist.IsDenied(ctx, "withdrawn", claimsMoment.Add(time.Minute))
		if err != nil {
			t.Fatalf("cannot read the denylist: %v", err)
		}

		if !denied {
			t.Fatal("expected a withdrawn token to be denied")
		}
	})

	t.Run("edge: the deadline instant itself is past the entry, matching the token's own boundary", func(t *testing.T) {
		// The signature stops verifying at the same instant, so an entry that
		// outlived it would be protecting nothing.
		denylist := auth.NewMemoryDenylist()
		deadline := claimsMoment.Add(15 * time.Minute)

		if err := denylist.Deny(ctx, "withdrawn", deadline); err != nil {
			t.Fatalf("cannot withdraw a token: %v", err)
		}

		denied, err := denylist.IsDenied(ctx, "withdrawn", deadline)
		if err != nil {
			t.Fatalf("cannot read the denylist: %v", err)
		}

		if denied {
			t.Fatal("expected the entry to stop mattering on the instant the token expires")
		}
	})

	t.Run("edge: an entry that can no longer matter is dropped rather than kept forever", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()

		if err := denylist.Deny(ctx, "withdrawn", claimsMoment.Add(time.Minute)); err != nil {
			t.Fatalf("cannot withdraw a token: %v", err)
		}

		if denylist.Size() != 1 {
			t.Fatalf("expected one held entry, got %d", denylist.Size())
		}

		if _, err := denylist.IsDenied(ctx, "withdrawn", claimsMoment.Add(time.Hour)); err != nil {
			t.Fatalf("cannot read the denylist: %v", err)
		}

		if denylist.Size() != 0 {
			t.Fatalf("expected the expired entry to be dropped, %d held", denylist.Size())
		}
	})

	t.Run("edge: withdrawing nothing is refused rather than silently stored", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()

		if err := denylist.Deny(ctx, "", claimsMoment); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected an empty token id to be refused, got %v", err)
		}

		if err := denylist.Deny(ctx, "withdrawn", time.Time{}); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected an unset deadline to be refused, got %v", err)
		}
	})

	t.Run("edge: asking about nothing is refused", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()

		if _, err := denylist.IsDenied(ctx, "", claimsMoment); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected an empty token id to be refused, got %v", err)
		}
	})

	t.Run("integration: withdrawing one token leaves every other one alone", func(t *testing.T) {
		denylist := auth.NewMemoryDenylist()

		if err := denylist.Deny(ctx, "signed-out", claimsMoment.Add(time.Minute)); err != nil {
			t.Fatalf("cannot withdraw a token: %v", err)
		}

		denied, err := denylist.IsDenied(ctx, "still-signed-in", claimsMoment)
		if err != nil {
			t.Fatalf("cannot read the denylist: %v", err)
		}

		if denied {
			t.Fatal("one parent signing out must not sign another one out")
		}
	})
}
