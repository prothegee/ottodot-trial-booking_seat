package auth_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
)

// The contract every directory has to satisfy.
//
// The rule worth proving twice is email normalisation. The fake lowercases and
// trims, the sql compares on lower(email), and if those two ever disagree then
// a parent signs in against one and not the other. That is a difference no
// test of either half alone can see.

// The seeded account both fixtures carry, so the suite can name it.
const (
    contractParentID    = "0192a000-0000-7000-8000-000000000001"
    contractParentEmail = "alice.tan@example.test"
    contractParentName  = "Alice Tan"
    contractChildID     = "0192a000-0000-7000-8000-000000000011"
    contractChildName   = "Mira Tan"

    // contractLonelyParentID has no children, which is the case a screen has
    // to render as an empty list rather than as a missing one.
    contractLonelyParentID    = "0192a000-0000-7000-8000-000000000003"
    contractLonelyParentEmail = "chandra.wijaya@example.test"
)

// directoryFixture is one directory, however it was built, already carrying the
// two seeded accounts above.
type directoryFixture interface {
    Directory() auth.Directory
}

// runDirectoryContract is the whole suite, pointed at whichever directory the
// caller builds.
func runDirectoryContract(t *testing.T, newFixture func(t *testing.T) directoryFixture) {
    ctx := context.Background()

    t.Run("integration: a seeded email finds its parent", func(t *testing.T) {
        fixture := newFixture(t)

        found, err := fixture.Directory().CredentialByEmail(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot look the parent up: %v", err)
        }

        if found.Parent.ID != contractParentID {
            t.Fatalf("expected %s, got %s", contractParentID, found.Parent.ID)
        }

        if found.Parent.DisplayName != contractParentName {
            t.Fatalf("expected %s, got %s", contractParentName, found.Parent.DisplayName)
        }

        if found.Parent.Role != auth.RoleParent {
            t.Fatalf("expected the parent role, got %s", found.Parent.Role)
        }
    })

    t.Run("integration: the sign in lookup carries the stored password hash", func(t *testing.T) {
        // Without this the whole password check is a no-op that passes every
        // other test in this file.
        fixture := newFixture(t)

        found, err := fixture.Directory().CredentialByEmail(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot look the parent up: %v", err)
        }

        if err := auth.VerifyPassword(found.PasswordHash, seededPassword); err != nil {
            t.Fatalf("the stored hash does not match the seeded password: %v", err)
        }
    })

    t.Run("edge: the same address typed with capitals finds the same parent", func(t *testing.T) {
        // A sign in that fails on capitalisation is a support ticket, not a
        // security property.
        fixture := newFixture(t)

        found, err := fixture.Directory().CredentialByEmail(ctx, "  Alice.TAN@Example.Test  ")
        if err != nil {
            t.Fatalf("cannot look the parent up: %v", err)
        }

        if found.Parent.ID != contractParentID {
            t.Fatalf("expected %s, got %s", contractParentID, found.Parent.ID)
        }
    })

    t.Run("edge: an address nobody has is refused", func(t *testing.T) {
        fixture := newFixture(t)

        if _, err := fixture.Directory().CredentialByEmail(ctx, "nobody@example.test"); !errors.Is(err, auth.ErrNoSuchParent) {
            t.Fatalf("expected an unknown address to be refused, got %v", err)
        }
    })

    t.Run("edge: an empty address is refused before anything is read", func(t *testing.T) {
        fixture := newFixture(t)

        if _, err := fixture.Directory().CredentialByEmail(ctx, "   "); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty address to be refused, got %v", err)
        }
    })

    t.Run("integration: the account read carries the parent and their children", func(t *testing.T) {
        fixture := newFixture(t)

        account, err := fixture.Directory().Account(ctx, contractParentID)
        if err != nil {
            t.Fatalf("cannot read the account: %v", err)
        }

        if account.Parent.DisplayName != contractParentName {
            t.Fatalf("expected %s, got %s", contractParentName, account.Parent.DisplayName)
        }

        if len(account.Children) != 1 {
            t.Fatalf("expected one child, got %d", len(account.Children))
        }

        if account.Children[0].ID != contractChildID || account.Children[0].FullName != contractChildName {
            t.Fatalf("expected the seeded child, got %+v", account.Children[0])
        }
    })

    t.Run("edge: an account with no children reads as an empty list, never a missing one", func(t *testing.T) {
        fixture := newFixture(t)

        account, err := fixture.Directory().Account(ctx, contractLonelyParentID)
        if err != nil {
            t.Fatalf("cannot read the account: %v", err)
        }

        if account.Children == nil {
            t.Fatal("expected an empty list rather than nil, so the client renders a list and not a guard")
        }

        if len(account.Children) != 0 {
            t.Fatalf("expected no children, got %d", len(account.Children))
        }
    })

    t.Run("edge: an id nobody has is refused", func(t *testing.T) {
        fixture := newFixture(t)

        if _, err := fixture.Directory().Account(ctx, "0192a000-0000-7000-8000-0000000000ff"); !errors.Is(err, auth.ErrNoSuchParent) {
            t.Fatalf("expected an unknown id to be refused, got %v", err)
        }
    })

    t.Run("edge: an empty id is refused before anything is read", func(t *testing.T) {
        fixture := newFixture(t)

        if _, err := fixture.Directory().Account(ctx, ""); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty id to be refused, got %v", err)
        }
    })
}
