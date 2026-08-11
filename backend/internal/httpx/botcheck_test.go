package httpx_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/httpx"
)

// newBotCheck builds the check with a mock verifier, and hands back both so a
// case can assert on what the verifier was asked.
func newBotCheck(t *testing.T, requireCaptcha bool) (*httpx.BotCheck, *captcha.MockVerifier) {
    t.Helper()

    verifier := captcha.NewMockVerifier()

    check, err := httpx.NewBotCheck(httpx.BotCheckSettings{
        Verifier:       verifier,
        RequireCaptcha: requireCaptcha,
    }, nil)
    if err != nil {
        t.Fatalf("cannot build the bot check: %v", err)
    }

    return check, verifier
}

// personSignals are what a real form produces.
var personSignals = httpx.Signals{
    FilledInMillis: 4200,
    CaptchaToken:   captcha.TokenPass,
}

func TestInspectingASubmission(t *testing.T) {
    t.Run("unit: a form a person filled in passes", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        if err := check.Inspect(context.Background(), personSignals, "127.0.0.1"); err != nil {
            t.Fatalf("a plausible submission was refused: %v", err)
        }
    })

    t.Run("unit: a filled honeypot is refused", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.Honeypot = "http://cheap-pills.example"

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); !errors.Is(err, httpx.ErrBotCheckFailed) {
            t.Fatalf("a filled honeypot answered %v", err)
        }
    })

    t.Run("unit: a form submitted faster than a person can fill it is refused", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.FilledInMillis = int64(httpx.MinimumFillTime/time.Millisecond) - 1

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); !errors.Is(err, httpx.ErrBotCheckFailed) {
            t.Fatalf("an impossibly fast submission answered %v", err)
        }
    })

    t.Run("edge: exactly the minimum is a pass, so the boundary is not a coin toss", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.FilledInMillis = int64(httpx.MinimumFillTime / time.Millisecond)

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); err != nil {
            t.Fatalf("a submission at exactly the minimum was refused: %v", err)
        }
    })

    t.Run("edge: a client that did not measure is not punished for it", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.FilledInMillis = 0

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); err != nil {
            t.Fatalf("an unmeasured submission was refused: %v", err)
        }
    })

    t.Run("edge: the honeypot is checked before the challenge, so a bot costs nothing", func(t *testing.T) {
        check, verifier := newBotCheck(t, false)

        signals := personSignals
        signals.Honeypot = "anything"

        _ = check.Inspect(context.Background(), signals, "127.0.0.1")

        if verifier.Checks() != 0 {
            t.Fatalf("the challenge provider was asked %d times about a submission the honeypot caught",
                verifier.Checks())
        }
    })

    t.Run("behaviour: with the challenge optional, a missing token is a pass", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.CaptchaToken = ""

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); err != nil {
            t.Fatalf("a submission with no token was refused while the challenge is optional: %v", err)
        }
    })

    t.Run("behaviour: with the challenge required, a missing token is a refusal", func(t *testing.T) {
        check, _ := newBotCheck(t, true)

        signals := personSignals
        signals.CaptchaToken = ""

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); !errors.Is(err, httpx.ErrBotCheckFailed) {
            t.Fatalf("a submission with no token answered %v while the challenge is required", err)
        }
    })

    t.Run("behaviour: a token that is sent is verified whether it is required or not", func(t *testing.T) {
        check, verifier := newBotCheck(t, false)

        signals := personSignals
        signals.CaptchaToken = captcha.TokenFail

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); !errors.Is(err, captcha.ErrRefused) {
            t.Fatalf("a refused token answered %v", err)
        }

        if verifier.Checks() != 1 {
            t.Fatalf("the provider was asked %d times", verifier.Checks())
        }
    })

    t.Run("edge: an unreachable challenge provider is a pass", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        signals := personSignals
        signals.CaptchaToken = captcha.TokenUnavailable

        if err := check.Inspect(context.Background(), signals, "127.0.0.1"); err != nil {
            t.Fatalf("a parent was refused because a third party was down: %v", err)
        }
    })

    t.Run("edge: requiring a challenge with nothing to verify it is refused at construction", func(t *testing.T) {
        _, err := httpx.NewBotCheck(httpx.BotCheckSettings{RequireCaptcha: true}, nil)

        if err == nil {
            t.Fatal("a required challenge with no verifier was built, and it would let everything through")
        }
    })

    t.Run("edge: every refusal is the same failure, whatever caught it", func(t *testing.T) {
        check, _ := newBotCheck(t, false)

        honeypot := personSignals
        honeypot.Honeypot = "x"

        rushed := personSignals
        rushed.FilledInMillis = 1

        first := httpx.FailureFor(check.Inspect(context.Background(), honeypot, "127.0.0.1"))
        second := httpx.FailureFor(check.Inspect(context.Background(), rushed, "127.0.0.1"))

        if first.Code != second.Code || first.Message != second.Message {
            t.Fatalf("the two refusals differ: %+v and %+v, which tells a script which check caught it",
                first, second)
        }
    })
}
