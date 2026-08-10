package auth_test

import (
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
)

// claimsMoment is the instant every case here starts from.
var claimsMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// liveClaims is a claim set this service would have issued.
func liveClaims() auth.Claims {
	return auth.Claims{
		Subject:   "0192a000-0000-7000-8000-000000000001",
		Role:      auth.RoleParent,
		Type:      auth.TypeAccess,
		TokenID:   "0192a000-0000-7000-8000-0000000000aa",
		IssuedAt:  claimsMoment.Unix(),
		ExpiresAt: claimsMoment.Add(15 * time.Minute).Unix(),
	}
}

func TestClaimsValidation(t *testing.T) {
	t.Run("unit: a claim set this service issued is accepted", func(t *testing.T) {
		if err := liveClaims().Validate(); err != nil {
			t.Fatalf("expected the claim set to be accepted, got %v", err)
		}
	})

	t.Run("unit: an admin role is accepted, because the admin routes are gated on it", func(t *testing.T) {
		claims := liveClaims()
		claims.Role = auth.RoleAdmin

		if err := claims.Validate(); err != nil {
			t.Fatalf("expected an admin claim set to be accepted, got %v", err)
		}
	})

	t.Run("edge: a token with no jti is refused", func(t *testing.T) {
		// This is the one that matters most. A token nobody can name cannot be
		// denylisted, so accepting one quietly removes logout for as long as
		// that token lives.
		claims := liveClaims()
		claims.TokenID = ""

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected a claim set with no jti to be refused")
		}
	})

	t.Run("edge: a token with no subject is refused", func(t *testing.T) {
		claims := liveClaims()
		claims.Subject = ""

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected a claim set with no subject to be refused")
		}
	})

	t.Run("edge: a role this service does not know is refused", func(t *testing.T) {
		claims := liveClaims()
		claims.Role = "superuser"

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected an unknown role to be refused")
		}
	})

	t.Run("edge: a type other than access is refused", func(t *testing.T) {
		// Nothing signs a refresh JWT today, and this is what keeps the day a
		// second type is signed from being the day one is spent as the other.
		claims := liveClaims()
		claims.Type = "refresh"

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected a non access type to be refused")
		}
	})

	t.Run("edge: an expiry at or before the issue instant is refused", func(t *testing.T) {
		claims := liveClaims()
		claims.ExpiresAt = claims.IssuedAt

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected a token with no life at all to be refused")
		}
	})

	t.Run("edge: an unset issue instant is refused", func(t *testing.T) {
		claims := liveClaims()
		claims.IssuedAt = 0

		if !errors.Is(claims.Validate(), auth.ErrTokenInvalid) {
			t.Fatal("expected a claim set with no issue instant to be refused")
		}
	})
}

func TestClaimsExpiry(t *testing.T) {
	t.Run("unit: a token inside its life is not expired", func(t *testing.T) {
		if liveClaims().IsExpired(claimsMoment.Add(time.Minute)) {
			t.Fatal("expected a token one minute old to still be live")
		}
	})

	t.Run("edge: a token expiring on this exact instant is already spent", func(t *testing.T) {
		// The boundary is inclusive on purpose. Leeway here is time in which a
		// withdrawn token still works, and there is nothing to buy with it:
		// one service issues and verifies, on one clock.
		claims := liveClaims()

		if !claims.IsExpired(claims.Expiry()) {
			t.Fatal("expected the expiry instant itself to count as expired")
		}
	})

	t.Run("edge: one second before the expiry the token still stands", func(t *testing.T) {
		claims := liveClaims()

		if claims.IsExpired(claims.Expiry().Add(-time.Second)) {
			t.Fatal("expected a token with a second left to still be live")
		}
	})

	t.Run("unit: the expiry reads back as the instant the denylist has to cover", func(t *testing.T) {
		claims := liveClaims()

		if !claims.Expiry().Equal(claimsMoment.Add(15 * time.Minute)) {
			t.Fatalf("expected %v, got %v", claimsMoment.Add(15*time.Minute), claims.Expiry())
		}
	})
}

func TestKnownRoles(t *testing.T) {
	t.Run("unit: parent and admin are the two roles", func(t *testing.T) {
		if !auth.IsKnownRole(auth.RoleParent) || !auth.IsKnownRole(auth.RoleAdmin) {
			t.Fatal("expected both seeded roles to be known")
		}
	})

	t.Run("edge: an empty role is not a role", func(t *testing.T) {
		if auth.IsKnownRole("") {
			t.Fatal("expected an empty role to be refused")
		}
	})
}
