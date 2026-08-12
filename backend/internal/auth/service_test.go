package auth_test

import (
    "context"
    "errors"
    "fmt"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

// serviceStage is the auth flow wired against fakes, with the clock and both
// minters pinned so every case asserts on values it chose.
type serviceStage struct {
    service   *auth.Service
    store     *auth.MemoryRefreshStore
    directory *auth.MemoryDirectory
    denylist  *auth.MemoryDenylist
    signer    *auth.Signer

    // now is moved by hand. Nothing in these tests sleeps.
    now time.Time
}

// newServiceStage builds the flow.
func newServiceStage(t *testing.T) *serviceStage {
    t.Helper()

    stage := &serviceStage{
        store:     auth.NewMemoryRefreshStore(),
        directory: seededDirectory(),
        denylist:  auth.NewMemoryDenylist(),
        signer:    newTestSigner(t),
        now:       claimsMoment,
    }

    minted := 0

    settings := auth.DefaultSettings()
    settings.Clock = func() time.Time { return stage.now }
    settings.NewID = func() (string, error) {
        minted++

        // Sequential in the uuid text shape, so a failure names which mint it
        // was rather than a random value nobody can place.
        return fmt.Sprintf("0192a000-0000-7000-8000-%012d", minted), nil
    }
    settings.NewToken = func() (string, error) {
        minted++

        return fmt.Sprintf("refresh-token-%d", minted), nil
    }

    service, err := auth.NewService(stage.signer, stage.store, stage.directory, stage.denylist, settings)
    if err != nil {
        t.Fatalf("cannot build the service: %v", err)
    }

    stage.service = service

    return stage
}

func TestServiceConstruction(t *testing.T) {
    t.Run("edge: a service missing a collaborator is refused rather than built", func(t *testing.T) {
        // Refusing here beats a nil dereference on the first request, which
        // would arrive at three in the morning as a stack trace instead of a
        // startup failure.
        signer := newTestSigner(t)
        store := auth.NewMemoryRefreshStore()
        directory := auth.NewMemoryDirectory()
        denylist := auth.NewMemoryDenylist()

        if _, err := auth.NewService(nil, store, directory, denylist, auth.DefaultSettings()); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing signer to be refused, got %v", err)
        }

        if _, err := auth.NewService(signer, nil, directory, denylist, auth.DefaultSettings()); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing store to be refused, got %v", err)
        }

        if _, err := auth.NewService(signer, store, nil, denylist, auth.DefaultSettings()); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing directory to be refused, got %v", err)
        }

        if _, err := auth.NewService(signer, store, directory, nil, auth.DefaultSettings()); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing denylist to be refused, got %v", err)
        }
    })

    t.Run("unit: the default lifetimes are the two the design fixed", func(t *testing.T) {
        settings := auth.DefaultSettings()

        if settings.AccessTTL != 15*time.Minute {
            t.Fatalf("expected a 15 minute access token, got %v", settings.AccessTTL)
        }

        if settings.RefreshTTL != 720*time.Hour {
            t.Fatalf("expected a 30 day refresh token, got %v", settings.RefreshTTL)
        }
    })
}

func TestSigningIn(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: a seeded email is signed in with both tokens", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if issued.AccessToken == "" || issued.RefreshToken == "" {
            t.Fatal("expected both tokens to be issued")
        }

        if issued.Claims.Subject != contractParentID {
            t.Fatalf("expected the token to speak for %s, got %s", contractParentID, issued.Claims.Subject)
        }

        if issued.Claims.Role != auth.RoleParent {
            t.Fatalf("expected the parent role in the token, got %s", issued.Claims.Role)
        }
    })

    t.Run("unit: the access token verifies against the signer that issued it", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if _, err := stage.signer.Verify(issued.AccessToken, stage.now.Add(time.Minute)); err != nil {
            t.Fatalf("the token this service just issued does not verify: %v", err)
        }
    })

    t.Run("unit: only the refresh token's hash is stored, never the token", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        held, err := stage.store.Record(ctx, auth.HashRefreshToken(issued.RefreshToken))
        if err != nil {
            t.Fatalf("the refresh token was not stored: %v", err)
        }

        if string(held.TokenHash) == issued.RefreshToken {
            t.Fatal("the row holds the token itself")
        }
    })

    t.Run("unit: the two expiries follow the configured lifetimes", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if !issued.AccessExpiresAt.Equal(stage.now.Add(15 * time.Minute)) {
            t.Fatalf("expected the access token to last 15 minutes, got %v", issued.AccessExpiresAt)
        }

        if !issued.RefreshExpiresAt.Equal(stage.now.Add(720 * time.Hour)) {
            t.Fatalf("expected the refresh token to last 30 days, got %v", issued.RefreshExpiresAt)
        }
    })

    t.Run("integration: an admin signs in carrying the admin role", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, "ops.admin@example.test", seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if issued.Claims.Role != auth.RoleAdmin {
            t.Fatalf("expected the admin role, got %s", issued.Claims.Role)
        }
    })

    t.Run("edge: two sign ins start two families, so one device signing out leaves the other", func(t *testing.T) {
        stage := newServiceStage(t)

        phone, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in on the first device: %v", err)
        }

        laptop, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in on the second device: %v", err)
        }

        first, err := stage.store.Record(ctx, auth.HashRefreshToken(phone.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the first token: %v", err)
        }

        second, err := stage.store.Record(ctx, auth.HashRefreshToken(laptop.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the second token: %v", err)
        }

        if first.FamilyID == second.FamilyID {
            t.Fatal("two sign ins shared a family, so signing out of one would end the other")
        }
    })

    t.Run("edge: an address nobody has is refused and counted", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.LogIn(ctx, "nobody@example.test", seededPassword); !errors.Is(err, auth.ErrNoSuchParent) {
            t.Fatalf("expected an unknown address to be refused, got %v", err)
        }

        if stage.service.Counters().Snapshot().LoginRefused != 1 {
            t.Fatal("expected the refusal to be counted")
        }
    })

    t.Run("edge: an empty address is refused before the directory is asked", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.LogIn(ctx, "   ", seededPassword); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty address to be refused, got %v", err)
        }
    })

    t.Run("behaviour: the right address with the wrong password is refused and counted", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.LogIn(ctx, contractParentEmail, "not-the-password"); !errors.Is(err, auth.ErrNoSuchParent) {
            t.Fatalf("expected a wrong password to be refused, got %v", err)
        }

        if stage.service.Counters().Snapshot().LoginRefused != 1 {
            t.Fatal("expected the refusal to be counted")
        }
    })

    t.Run("edge: a wrong password and an unknown address are the same refusal", func(t *testing.T) {
        // Any difference between the two is a way to find out which addresses
        // have accounts here, one guess at a time.
        stage := newServiceStage(t)

        _, wrongPassword := stage.service.LogIn(ctx, contractParentEmail, "not-the-password")
        _, unknownAddress := stage.service.LogIn(ctx, "nobody@example.test", seededPassword)

        if wrongPassword.Error() != unknownAddress.Error() {
            t.Fatalf("a wrong password reports %q and an unknown address reports %q",
                wrongPassword, unknownAddress)
        }
    })

    t.Run("edge: an empty password is refused before the directory is asked", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.LogIn(ctx, contractParentEmail, ""); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty password to be refused, got %v", err)
        }
    })

    t.Run("edge: an account whose stored hash is unusable can never be signed in to", func(t *testing.T) {
        // A half finished seed, or a row somebody edited by hand. Treating it
        // as a match would be the worst possible reading of it.
        stage := newServiceStage(t)

        stage.directory.Add(
            "broken@example.test",
            "",
            auth.Parent{ID: contractLonelyParentID, DisplayName: "Broken Row", Role: auth.RoleParent},
            nil)

        if _, err := stage.service.LogIn(ctx, "broken@example.test", seededPassword); !errors.Is(err, auth.ErrNoSuchParent) {
            t.Fatalf("expected an account with no usable hash to be refused, got %v", err)
        }
    })
}

func TestSigningOut(t *testing.T) {
    ctx := context.Background()

    t.Run("behaviour: signing out withdraws the access token and ends the family", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        err = stage.service.LogOut(ctx, auth.LogOutRequest{
            TokenID:      issued.Claims.TokenID,
            TokenExpiry:  issued.AccessExpiresAt,
            RefreshToken: issued.RefreshToken,
        })
        if err != nil {
            t.Fatalf("cannot sign out: %v", err)
        }

        withdrawn, err := stage.denylist.IsDenied(ctx, issued.Claims.TokenID, stage.now.Add(time.Minute))
        if err != nil {
            t.Fatalf("cannot read the denylist: %v", err)
        }

        if !withdrawn {
            t.Fatal("expected the access token to be withdrawn")
        }

        held, err := stage.store.Record(ctx, auth.HashRefreshToken(issued.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the refresh token: %v", err)
        }

        if !held.IsRevoked() {
            t.Fatal("expected the refresh token family to be ended")
        }
    })

    t.Run("edge: the denylist entry stands for exactly the token's remaining life", func(t *testing.T) {
        // Not longer. Past that instant the signature stops verifying, so the
        // entry protects nothing and only costs memory.
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if err := stage.service.LogOut(ctx, auth.LogOutRequest{
            TokenID:     issued.Claims.TokenID,
            TokenExpiry: issued.AccessExpiresAt,
        }); err != nil {
            t.Fatalf("cannot sign out: %v", err)
        }

        stillWithdrawn, err := stage.denylist.IsDenied(ctx, issued.Claims.TokenID, issued.AccessExpiresAt)
        if err != nil {
            t.Fatalf("cannot read the denylist: %v", err)
        }

        if stillWithdrawn {
            t.Fatal("expected the entry to stop mattering when the token expires")
        }
    })

    t.Run("edge: signing out without a refresh cookie still withdraws the access token", func(t *testing.T) {
        // The parent asked to be signed out. Refusing because their refresh
        // cookie had already lapsed would leave them signed in.
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if err := stage.service.LogOut(ctx, auth.LogOutRequest{
            TokenID:     issued.Claims.TokenID,
            TokenExpiry: issued.AccessExpiresAt,
        }); err != nil {
            t.Fatalf("cannot sign out without a refresh token: %v", err)
        }

        withdrawn, err := stage.denylist.IsDenied(ctx, issued.Claims.TokenID, stage.now)
        if err != nil {
            t.Fatalf("cannot read the denylist: %v", err)
        }

        if !withdrawn {
            t.Fatal("expected the access token to be withdrawn anyway")
        }
    })

    t.Run("edge: a refresh token this service never issued is not a failure", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if err := stage.service.LogOut(ctx, auth.LogOutRequest{
            TokenID:      issued.Claims.TokenID,
            TokenExpiry:  issued.AccessExpiresAt,
            RefreshToken: "a-token-from-somewhere-else",
        }); err != nil {
            t.Fatalf("expected an unknown refresh token to be ignored, got %v", err)
        }
    })

    t.Run("edge: a sign out naming no token is refused", func(t *testing.T) {
        stage := newServiceStage(t)

        if err := stage.service.LogOut(ctx, auth.LogOutRequest{}); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a sign out with nothing to withdraw to be refused, got %v", err)
        }
    })

    t.Run("edge: one parent signing out leaves another parent signed in", func(t *testing.T) {
        stage := newServiceStage(t)

        leaving, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign the first parent in: %v", err)
        }

        staying, err := stage.service.LogIn(ctx, contractLonelyParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign the second parent in: %v", err)
        }

        if err := stage.service.LogOut(ctx, auth.LogOutRequest{
            TokenID:      leaving.Claims.TokenID,
            TokenExpiry:  leaving.AccessExpiresAt,
            RefreshToken: leaving.RefreshToken,
        }); err != nil {
            t.Fatalf("cannot sign out: %v", err)
        }

        held, err := stage.store.Record(ctx, auth.HashRefreshToken(staying.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the other parent's token: %v", err)
        }

        if held.IsRevoked() {
            t.Fatal("one parent signing out ended another parent's session")
        }
    })
}

func TestTheSessionRead(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: the session read carries the parent and their children", func(t *testing.T) {
        stage := newServiceStage(t)

        account, err := stage.service.Account(ctx, contractParentID)
        if err != nil {
            t.Fatalf("cannot read the account: %v", err)
        }

        if account.Parent.DisplayName != contractParentName {
            t.Fatalf("expected %s, got %s", contractParentName, account.Parent.DisplayName)
        }

        if len(account.Children) != 1 {
            t.Fatalf("expected one child, got %d", len(account.Children))
        }
    })

    t.Run("edge: an empty id is refused before the directory is asked", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.Account(ctx, ""); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty id to be refused, got %v", err)
        }
    })
}
