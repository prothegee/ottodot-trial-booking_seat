package auth_test

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

// Simulation 13: refresh rotation and reuse detection.
//
//	sequenceDiagram
//	    participant UI as Client
//	    participant API as Go api
//	    participant PG as Repository
//	    participant T as Thief
//
//	    UI->>API: POST /api/v1/auth/refresh with token R1
//	    API->>PG: match hash, revoke R1, insert R2 in the same family
//	    API-->>UI: new cookies carrying R2
//	    T->>API: POST /api/v1/auth/refresh with the stolen R1
//	    API->>PG: R1 is revoked, revoke the whole family
//	    API-->>T: 401 token_reused
//	    UI->>API: POST /api/v1/auth/refresh with R2
//	    API-->>UI: 401 token_reused, sign in again
//
// Asserts: R1 works exactly once, the family is revoked on reuse,
// auth_refresh_reuse_detected_total increments, and the honest consequence is
// stated rather than hidden. The real parent is signed out too. That is the
// correct trade for a stolen token: two parties hold R1, one of them is a
// thief, and this service cannot tell which.

func TestSimulation13RefreshRotationAndReuseDetection(t *testing.T) {
    t.Run("behaviour: one refresh token works exactly once", func(t *testing.T) {
        stage := newServiceStage(t)
        ctx := context.Background()

        signedIn, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stage.now = stage.now.Add(20 * time.Minute)

        rotated, err := stage.service.Refresh(ctx, signedIn.RefreshToken)
        if err != nil {
            t.Fatalf("the first refresh must succeed: %v", err)
        }

        if rotated.RefreshToken == signedIn.RefreshToken {
            t.Fatal("the token did not rotate, so there is nothing to detect reuse of")
        }

        stage.now = stage.now.Add(time.Minute)

        if _, err := stage.service.Refresh(ctx, signedIn.RefreshToken); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected the second use of R1 to be reported as reuse, got %v", err)
        }
    })

    t.Run("behaviour: the thief is refused and the whole family is revoked", func(t *testing.T) {
        stage := newServiceStage(t)
        ctx := context.Background()

        signedIn, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        // The parent refreshes normally, and a copy of R1 is in somebody
        // else's hands.
        stolen := signedIn.RefreshToken

        rotated, err := stage.service.Refresh(ctx, signedIn.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, stolen); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected the thief to be refused, got %v", err)
        }

        held, err := stage.store.Record(ctx, auth.HashRefreshToken(rotated.RefreshToken))
        if err != nil {
            t.Fatalf("cannot read the live token: %v", err)
        }

        if !held.IsRevoked() {
            t.Fatal("expected the live token to be revoked along with the rest of the family")
        }
    })

    t.Run("behaviour: the honest parent is signed out too, which is the stated trade", func(t *testing.T) {
        // This is the consequence the design accepts rather than hides. The
        // alternative is leaving the thief signed in, and the parent can sign
        // in again in one click.
        stage := newServiceStage(t)
        ctx := context.Background()

        signedIn, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stolen := signedIn.RefreshToken

        rotated, err := stage.service.Refresh(ctx, signedIn.RefreshToken)
        if err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, stolen); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected the thief to be refused, got %v", err)
        }

        if _, err := stage.service.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected the honest holder to be signed out as well, got %v", err)
        }
    })

    t.Run("behaviour: the reuse is counted, which is what puts it in front of a person", func(t *testing.T) {
        stage := newServiceStage(t)
        ctx := context.Background()

        signedIn, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, signedIn.RefreshToken); err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, signedIn.RefreshToken); !errors.Is(err, auth.ErrTokenReused) {
            t.Fatalf("expected reuse, got %v", err)
        }

        counted := stage.service.Counters().Snapshot()

        if counted.ReuseDetected != 1 {
            t.Fatalf("expected one reuse to be detected, got %d", counted.ReuseDetected)
        }

        if counted.Rotated != 1 {
            t.Fatalf("expected one successful rotation to be counted, got %d", counted.Rotated)
        }
    })

    t.Run("behaviour: over http the thief gets 401 token_reused and empty cookies", func(t *testing.T) {
        // The same story through the endpoints the client actually calls, so
        // the wire answer is asserted rather than assumed.
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        stage.now = stage.now.Add(20 * time.Minute)

        honest := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil), signedIn)
        honest.Header.Set("Origin", testOrigin)

        rotated := stage.call(honest)

        if rotated.Code != http.StatusNoContent {
            t.Fatalf("the honest refresh must succeed, got %d", rotated.Code)
        }

        // The thief replays the cookies they copied before the rotation.
        thief := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil), signedIn)
        thief.Header.Set("Origin", testOrigin)

        refused := stage.call(thief)

        if refused.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", refused.Code)
        }

        if codeOf(t, refused) != auth.CodeTokenReused {
            t.Fatalf("expected %s, got %s", auth.CodeTokenReused, codeOf(t, refused))
        }

        for name, cookie := range cookiesOn(t, refused) {
            if cookie.MaxAge != -1 {
                t.Fatalf("%s was left in the thief's browser", name)
            }
        }

        // And the parent, on their next refresh, is sent back to sign in.
        back := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil), rotated)
        back.Header.Set("Origin", testOrigin)

        signedOut := stage.call(back)

        if signedOut.Code != http.StatusUnauthorized {
            t.Fatalf("expected the honest holder to be sent back to sign in, got %d", signedOut.Code)
        }

        if codeOf(t, signedOut) != auth.CodeTokenReused {
            t.Fatalf("expected %s, got %s", auth.CodeTokenReused, codeOf(t, signedOut))
        }
    })

    t.Run("edge: a stolen token is worthless once the parent refreshes, without anybody noticing the theft", func(t *testing.T) {
        // The property worth naming: nothing here depends on the theft being
        // spotted. The next honest refresh is what makes the copy useless.
        stage := newServiceStage(t)
        ctx := context.Background()

        signedIn, err := stage.service.LogIn(ctx, contractParentEmail, seededPassword)
        if err != nil {
            t.Fatalf("cannot sign in: %v", err)
        }

        stolen := signedIn.RefreshToken

        if _, err := stage.service.Refresh(ctx, signedIn.RefreshToken); err != nil {
            t.Fatalf("cannot refresh: %v", err)
        }

        if _, err := stage.service.Refresh(ctx, stolen); err == nil {
            t.Fatal("the stolen token still worked after the parent refreshed")
        }
    })
}
