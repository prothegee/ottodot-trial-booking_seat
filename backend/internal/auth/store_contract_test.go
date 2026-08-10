package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
	"ottodot-trial-booking/backend/internal/identifier"
)

// The contract every refresh store has to satisfy.
//
// The risk this file exists to remove: the fake implements rotation and reuse
// detection correctly while the sql is wrong, and every fast test stays green.
// One suite pointed at both implementations is the only thing that catches
// that, so this file runs twice, once against the memory store in the fast
// tiers and once against real Postgres behind the containers tag.

// storeMoment is the instant every case starts from. Whole seconds, because the
// column stores microseconds and a rounder value reads the same on both sides.
var storeMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// contractRefreshTTL is the life every token in this suite is written with,
// unless the case is about expiry.
const contractRefreshTTL = 720 * time.Hour

// storeFixture is one store, however it was built.
//
// It carries a parent id because the real table hangs from parents by a foreign
// key. The fake has no such constraint, which is exactly the kind of difference
// this suite exists to keep from mattering.
type storeFixture interface {
	Store() auth.RefreshStore
	ParentID() string
}

// newTokenID mints an identifier in the format the uuid column accepts.
func newTokenID(t *testing.T) string {
	t.Helper()

	minted, err := identifier.NewUUIDv7()
	if err != nil {
		t.Fatalf("cannot mint an identifier: %v", err)
	}

	return minted
}

// issueOne writes the first token of a fresh family and returns the token and
// what was stored for it.
func issueOne(t *testing.T, fixture storeFixture, token string) auth.RefreshRecord {
	t.Helper()

	written, err := fixture.Store().Issue(context.Background(), auth.IssueRequest{
		TokenID:   newTokenID(t),
		ParentID:  fixture.ParentID(),
		FamilyID:  newTokenID(t),
		TokenHash: auth.HashRefreshToken(token),
		ExpiresAt: storeMoment.Add(contractRefreshTTL),
		Now:       storeMoment,
	})
	if err != nil {
		t.Fatalf("cannot issue a refresh token: %v", err)
	}

	return written
}

// rotateWith spends one token and asks for the named successor.
func rotateWith(t *testing.T, fixture storeFixture, presented string, next string, now time.Time) (auth.RefreshRecord, error) {
	t.Helper()

	return fixture.Store().Rotate(context.Background(), auth.RotateRequest{
		PresentedHash: auth.HashRefreshToken(presented),
		NextTokenID:   newTokenID(t),
		NextTokenHash: auth.HashRefreshToken(next),
		NextExpiresAt: now.Add(contractRefreshTTL),
		Now:           now,
	})
}

// runRefreshStoreContract is the whole suite, pointed at whichever store the
// caller builds.
func runRefreshStoreContract(t *testing.T, newFixture func(t *testing.T) storeFixture) {
	ctx := context.Background()

	t.Run("integration: an issued token reads back by its hash", func(t *testing.T) {
		fixture := newFixture(t)

		written := issueOne(t, fixture, "first-token")

		held, err := fixture.Store().Record(ctx, auth.HashRefreshToken("first-token"))
		if err != nil {
			t.Fatalf("cannot read the token back: %v", err)
		}

		if held.ID != written.ID || held.FamilyID != written.FamilyID {
			t.Fatalf("expected %+v, got %+v", written, held)
		}

		if held.IsRevoked() {
			t.Fatal("a freshly issued token must be live")
		}
	})

	t.Run("edge: a token nobody issued is not found", func(t *testing.T) {
		fixture := newFixture(t)

		if _, err := fixture.Store().Record(ctx, auth.HashRefreshToken("never-issued")); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Fatalf("expected an unknown hash to report not found, got %v", err)
		}
	})

	t.Run("edge: an issue request missing anything it needs is refused", func(t *testing.T) {
		fixture := newFixture(t)

		refused := []auth.IssueRequest{
			{ParentID: "p", FamilyID: "f", TokenHash: []byte{1}, ExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{TokenID: "t", FamilyID: "f", TokenHash: []byte{1}, ExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{TokenID: "t", ParentID: "p", TokenHash: []byte{1}, ExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{TokenID: "t", ParentID: "p", FamilyID: "f", ExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{TokenID: "t", ParentID: "p", FamilyID: "f", TokenHash: []byte{1}, Now: storeMoment},
		}

		for index, request := range refused {
			if _, err := fixture.Store().Issue(ctx, request); !errors.Is(err, auth.ErrInvalidRequest) {
				t.Fatalf("request %d was not refused, got %v", index, err)
			}
		}
	})

	t.Run("edge: a token that expires before it is written is refused", func(t *testing.T) {
		// A row nobody could ever spend is a row that only ever confuses
		// whoever finds it.
		fixture := newFixture(t)

		_, err := fixture.Store().Issue(ctx, auth.IssueRequest{
			TokenID:   newTokenID(t),
			ParentID:  fixture.ParentID(),
			FamilyID:  newTokenID(t),
			TokenHash: auth.HashRefreshToken("already-dead"),
			ExpiresAt: storeMoment,
			Now:       storeMoment,
		})

		if !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected a token with no life to be refused, got %v", err)
		}
	})

	t.Run("integration: rotation spends the presented token and writes a successor in the same family", func(t *testing.T) {
		fixture := newFixture(t)

		first := issueOne(t, fixture, "R1")

		second, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(time.Hour))
		if err != nil {
			t.Fatalf("cannot rotate: %v", err)
		}

		if second.FamilyID != first.FamilyID {
			t.Fatalf("expected the successor in family %s, got %s", first.FamilyID, second.FamilyID)
		}

		if second.ParentID != first.ParentID {
			t.Fatalf("expected the successor to belong to the same parent")
		}

		if second.IsRevoked() {
			t.Fatal("the successor must be live")
		}

		spent, err := fixture.Store().Record(ctx, auth.HashRefreshToken("R1"))
		if err != nil {
			t.Fatalf("cannot read the spent token: %v", err)
		}

		if !spent.IsRevoked() {
			t.Fatal("expected the presented token to be spent by the rotation")
		}
	})

	t.Run("behaviour: presenting a spent token reports reuse and revokes the whole family", func(t *testing.T) {
		// This is the case the whole design is for. Two parties hold R1, one of
		// them stole it, and this service cannot tell which. Ending the chain
		// signs the real parent out too, which is the correct trade.
		fixture := newFixture(t)

		issueOne(t, fixture, "R1")

		if _, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(time.Hour)); err != nil {
			t.Fatalf("cannot rotate: %v", err)
		}

		if _, err := rotateWith(t, fixture, "R1", "R3", storeMoment.Add(2*time.Hour)); !errors.Is(err, auth.ErrTokenReused) {
			t.Fatalf("expected reuse to be reported, got %v", err)
		}

		successor, err := fixture.Store().Record(ctx, auth.HashRefreshToken("R2"))
		if err != nil {
			t.Fatalf("cannot read the successor: %v", err)
		}

		if !successor.IsRevoked() {
			t.Fatal("expected the live successor to be revoked along with the rest of the family")
		}
	})

	t.Run("behaviour: after reuse the honest holder is signed out too", func(t *testing.T) {
		fixture := newFixture(t)

		issueOne(t, fixture, "R1")

		if _, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(time.Hour)); err != nil {
			t.Fatalf("cannot rotate: %v", err)
		}

		if _, err := rotateWith(t, fixture, "R1", "R3", storeMoment.Add(2*time.Hour)); !errors.Is(err, auth.ErrTokenReused) {
			t.Fatalf("expected reuse to be reported, got %v", err)
		}

		if _, err := rotateWith(t, fixture, "R2", "R4", storeMoment.Add(3*time.Hour)); !errors.Is(err, auth.ErrTokenReused) {
			t.Fatalf("expected the revoked successor to be refused as reuse, got %v", err)
		}
	})

	t.Run("edge: rotating a token nobody issued reports not found", func(t *testing.T) {
		fixture := newFixture(t)

		if _, err := rotateWith(t, fixture, "never-issued", "R2", storeMoment); !errors.Is(err, auth.ErrTokenNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	})

	t.Run("edge: an expired token cannot be rotated", func(t *testing.T) {
		fixture := newFixture(t)

		issueOne(t, fixture, "R1")

		past := storeMoment.Add(contractRefreshTTL).Add(time.Hour)

		if _, err := rotateWith(t, fixture, "R1", "R2", past); !errors.Is(err, auth.ErrTokenExpired) {
			t.Fatalf("expected an expired token to be refused, got %v", err)
		}
	})

	t.Run("edge: a token expiring on this exact instant is already spent", func(t *testing.T) {
		fixture := newFixture(t)

		issueOne(t, fixture, "R1")

		if _, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(contractRefreshTTL)); !errors.Is(err, auth.ErrTokenExpired) {
			t.Fatalf("expected the expiry instant itself to count as expired, got %v", err)
		}
	})

	t.Run("edge: an expired token is refused rather than reported as reuse", func(t *testing.T) {
		// The two are different events. Expiry is a parent who was away too
		// long, reuse is a stolen token, and only one of them is worth waking
		// somebody for.
		fixture := newFixture(t)

		issueOne(t, fixture, "R1")

		_, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(contractRefreshTTL))

		if errors.Is(err, auth.ErrTokenReused) {
			t.Fatal("an expired token must not be counted as a theft")
		}
	})

	t.Run("integration: revoking a family ends every live token in it", func(t *testing.T) {
		fixture := newFixture(t)

		first := issueOne(t, fixture, "R1")

		if _, err := rotateWith(t, fixture, "R1", "R2", storeMoment.Add(time.Hour)); err != nil {
			t.Fatalf("cannot rotate: %v", err)
		}

		ended, err := fixture.Store().RevokeFamily(ctx, auth.RevokeFamilyRequest{
			FamilyID: first.FamilyID,
			Now:      storeMoment.Add(2 * time.Hour),
		})
		if err != nil {
			t.Fatalf("cannot revoke the family: %v", err)
		}

		// R1 was already spent by the rotation, so only R2 was still live.
		if ended != 1 {
			t.Fatalf("expected one live token to be ended, got %d", ended)
		}
	})

	t.Run("edge: revoking leaves an already spent token at the instant it was spent", func(t *testing.T) {
		// The audit has to read as what happened rather than as what the last
		// call did.
		fixture := newFixture(t)

		first := issueOne(t, fixture, "R1")
		spentAt := storeMoment.Add(time.Hour)

		if _, err := rotateWith(t, fixture, "R1", "R2", spentAt); err != nil {
			t.Fatalf("cannot rotate: %v", err)
		}

		if _, err := fixture.Store().RevokeFamily(ctx, auth.RevokeFamilyRequest{
			FamilyID: first.FamilyID,
			Now:      storeMoment.Add(5 * time.Hour),
		}); err != nil {
			t.Fatalf("cannot revoke the family: %v", err)
		}

		spent, err := fixture.Store().Record(ctx, auth.HashRefreshToken("R1"))
		if err != nil {
			t.Fatalf("cannot read the spent token: %v", err)
		}

		if !spent.RevokedAt.Equal(spentAt) {
			t.Fatalf("expected the original revocation instant %v, got %v", spentAt, spent.RevokedAt)
		}
	})

	t.Run("edge: revoking a family nobody has ends nothing and reports so", func(t *testing.T) {
		fixture := newFixture(t)

		ended, err := fixture.Store().RevokeFamily(ctx, auth.RevokeFamilyRequest{
			FamilyID: newTokenID(t),
			Now:      storeMoment,
		})
		if err != nil {
			t.Fatalf("expected an unknown family to be a count of zero rather than a failure: %v", err)
		}

		if ended != 0 {
			t.Fatalf("expected nothing to be ended, got %d", ended)
		}
	})

	t.Run("edge: one family's revocation leaves another family alone", func(t *testing.T) {
		// A parent signing out on one device must not sign themselves out on
		// the other one, which is the whole reason a family is a chain rather
		// than an account.
		fixture := newFixture(t)

		onePhone := issueOne(t, fixture, "phone-token")
		issueOne(t, fixture, "laptop-token")

		if _, err := fixture.Store().RevokeFamily(ctx, auth.RevokeFamilyRequest{
			FamilyID: onePhone.FamilyID,
			Now:      storeMoment.Add(time.Hour),
		}); err != nil {
			t.Fatalf("cannot revoke the family: %v", err)
		}

		laptop, err := fixture.Store().Record(ctx, auth.HashRefreshToken("laptop-token"))
		if err != nil {
			t.Fatalf("cannot read the other family: %v", err)
		}

		if laptop.IsRevoked() {
			t.Fatal("revoking one chain must not end another")
		}
	})

	t.Run("edge: a rotation request missing anything it needs is refused", func(t *testing.T) {
		fixture := newFixture(t)

		refused := []auth.RotateRequest{
			{NextTokenID: "t", NextTokenHash: []byte{1}, NextExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{PresentedHash: []byte{1}, NextTokenHash: []byte{1}, NextExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{PresentedHash: []byte{1}, NextTokenID: "t", NextExpiresAt: storeMoment.Add(time.Hour), Now: storeMoment},
			{PresentedHash: []byte{1}, NextTokenID: "t", NextTokenHash: []byte{1}, Now: storeMoment},
		}

		for index, request := range refused {
			if _, err := fixture.Store().Rotate(ctx, request); !errors.Is(err, auth.ErrInvalidRequest) {
				t.Fatalf("request %d was not refused, got %v", index, err)
			}
		}
	})
}
