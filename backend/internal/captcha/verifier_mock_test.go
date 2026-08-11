package captcha_test

import (
    "context"
    "errors"
    "sync"
    "testing"

    "ottodot-trial-booking/backend/internal/captcha"
)

func TestVerifyingAChallengeToken(t *testing.T) {
    t.Run("unit: the token the widget mints passes", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        if err := verifier.Verify(context.Background(), captcha.TokenPass, "127.0.0.1"); err != nil {
            t.Fatalf("the passing token was refused: %v", err)
        }
    })

    t.Run("unit: an unrecognised token is refused", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        err := verifier.Verify(context.Background(), "something-a-script-made-up", "127.0.0.1")

        if !errors.Is(err, captcha.ErrRefused) {
            t.Fatalf("an unknown token answered %v, wanted a refusal", err)
        }
    })

    t.Run("unit: the failing token is refused, so the path can be driven end to end", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        if err := verifier.Verify(context.Background(), captcha.TokenFail, "127.0.0.1"); !errors.Is(err, captcha.ErrRefused) {
            t.Fatalf("the failing token answered %v, wanted a refusal", err)
        }
    })

    t.Run("edge: an absent token is refused before anything is decided", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        if err := verifier.Verify(context.Background(), "   ", "127.0.0.1"); !errors.Is(err, captcha.ErrInvalidRequest) {
            t.Fatalf("an empty token answered %v, wanted the invalid request refusal", err)
        }
    })

    t.Run("edge: an unreachable provider is not a refusal", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        err := verifier.Verify(context.Background(), captcha.TokenUnavailable, "127.0.0.1")

        if !errors.Is(err, captcha.ErrUnavailable) {
            t.Fatalf("the unreachable token answered %v, wanted the unavailable failure", err)
        }

        if errors.Is(err, captcha.ErrRefused) {
            t.Fatal("an unreachable provider was reported as a refusal, which would turn its outage into ours")
        }

        if verifier.Refusals() != 0 {
            t.Fatalf("%d refusals were counted for a caller nobody turned away", verifier.Refusals())
        }
    })

    t.Run("behaviour: the same token answers the same way every time", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        for round := 1; round <= 5; round++ {
            if err := verifier.Verify(context.Background(), captcha.TokenPass, "127.0.0.1"); err != nil {
                t.Fatalf("round %d refused a token that passed the first time: %v", round, err)
            }
        }
    })

    t.Run("behaviour: every call is counted, so an untouched layer can be proven untouched", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        if verifier.Checks() != 0 {
            t.Fatalf("a fresh verifier reports %d checks", verifier.Checks())
        }

        _ = verifier.Verify(context.Background(), captcha.TokenPass, "127.0.0.1")
        _ = verifier.Verify(context.Background(), captcha.TokenFail, "127.0.0.1")

        if verifier.Checks() != 2 {
            t.Fatalf("two calls were counted as %d", verifier.Checks())
        }

        if verifier.Refusals() != 1 {
            t.Fatalf("one refusal was counted as %d", verifier.Refusals())
        }
    })

    t.Run("integration: parallel checks leave the counts intact", func(t *testing.T) {
        verifier := captcha.NewMockVerifier()

        var waiting sync.WaitGroup

        for range 32 {
            waiting.Add(1)

            go func() {
                defer waiting.Done()

                _ = verifier.Verify(context.Background(), captcha.TokenPass, "127.0.0.1")
            }()
        }

        waiting.Wait()

        if verifier.Checks() != 32 {
            t.Fatalf("32 parallel checks were counted as %d", verifier.Checks())
        }
    })
}
