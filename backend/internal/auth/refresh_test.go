package auth_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

func TestRotatingARefreshToken(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: a refresh issues a new pair and spends the old token", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.now = stage.now.Add(20 * time.Minute)

        second, err := stage.service.Refresh(ctx, first.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if second.RefreshToken == first.RefreshToken {
            t.Fatal("expected a new refresh token, not the same one back")
        }

        if second.AccessToken == first.AccessToken {
            t.Fatal("expected a new access token")
        }

        spent, err := stage.store.Record(ctx, auth.HashRefreshToken(first.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the spent token: %v", err)
        }

        if !spent.IsRevoked() {
            t.Fatal("expected the presented token to be spent")
        }
    })

    t.Run("unit: the new access token verifies and speaks for the same parent", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.now = stage.now.Add(20 * time.Minute)

        second, err := stage.service.Refresh(ctx, first.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        claims, err := stage.signer.Verify(second.AccessToken, stage.now)
        if err != nil {
            t.Fatalf("the refreshed token does not verify: %v", err)
        }

        if claims.Subject != contractParentID {
            t.Fatalf("expected the token to speak for %s, got %s", contractParentID, claims.Subject)
        }
    })

    t.Run("unit: the successor stays in the same family", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        before, err := stage.store.Record(ctx, auth.HashRefreshToken(first.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the first token: %v", err)
        }

        second, err := stage.service.Refresh(ctx, first.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        after, err := stage.store.Record(ctx, auth.HashRefreshToken(second.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the successor: %v", err)
        }

        if after.FamilyID != before.FamilyID {
            t.Fatal("a rotation started a new family, which would leave the old chain unrevokable")
        }
    })

    t.Run("unit: a refresh that produced a successor is counted", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, first.RefreshToken); err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if stage.service.Counters().Snapshot().Rotated != 1 {
            t.Fatal("expected the rotation to be counted")
        }
    })

    t.Run("behaviour: the role is read fresh, so a changed role arrives at the next refresh", func(t *testing.T) {
        // Carrying the old role over would mean a promotion or a demotion took
        // effect only at the next sign in, which is a day rather than fifteen
        // minutes.
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.directory.Add(
            contractParentEmail,
            auth.Parent{ID: contractParentID, DisplayName: contractParentName, Role: auth.RoleAdmin},
            []auth.Child{{ID: contractChildID, FullName: contractChildName, GradeLevel: 5}})

        second, err := stage.service.Refresh(ctx, first.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if second.Claims.Role != auth.RoleAdmin {
            t.Fatalf("expected the new role to be picked up, got %s", second.Claims.Role)
        }
    })

    t.Run("edge: presenting nothing is refused before the store is touched", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.Refresh(ctx, ""); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected an empty token to be refused, got %v", err)
        }
    })

    t.Run("edge: a token this service never issued reports not found", func(t *testing.T) {
        stage := newServiceStage(t)

        if _, err := stage.service.Refresh(ctx, "a-token-from-somewhere-else"); !errors.Is(err, auth.ErrTokenNotFound) {
            t.Fatalf("expected not found, got %v", err)
        }
    })

    t.Run("edge: a refresh token past its life is refused", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.now = stage.now.Add(721 * time.Hour)

        if _, err := stage.service.Refresh(ctx, first.RefreshToken); !errors.Is(err, auth.ErrTokenExpired) {
            t.Fatalf("expected an expired refresh token to be refused, got %v", err)
        }
    })

    t.Run("edge: an expired refresh is not counted as reuse", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.now = stage.now.Add(721 * time.Hour)

        if _, err := stage.service.Refresh(ctx, first.RefreshToken); err == nil {
            t.Fatal("expected the expired token to be refused")
        }

        if stage.service.Counters().Snapshot().ReuseDetected != 0 {
            t.Fatal("a parent who was away too long is not a theft")
        }
    })

    t.Run("behaviour: a spent token presented again is reported as reuse and counted", func(t *testing.T) {
        stage := newServiceStage(t)

        first, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, first.RefreshToken); err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, first.RefreshToken); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected reuse to be reported, got %v", err)
        }

        if stage.service.Counters().Snapshot().ReuseDetected != 1 {
            t.Fatal("expected the reuse to be counted, which is what puts it on a dashboard")
        }
    })

    t.Run("edge: a chain of refreshes works as long as each one presents the newest token", func(t *testing.T) {
        stage := newServiceStage(t)

        issued, err := stage.service.LogIn(ctx, contractParentEmail)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        for round := 0; round < 5; round++ {
            stage.now = stage.now.Add(10 * time.Minute)

            issued, err = stage.service.Refresh(ctx, issued.RefreshToken)
            if err != nil {
                t.Fatalf("refresh %d failed: %v", round, err)
            }
        }

        if stage.service.Counters().Snapshot().Rotated != 5 {
            t.Fatalf("expected five rotations, got %d", stage.service.Counters().Snapshot().Rotated)
        }
    })
}
