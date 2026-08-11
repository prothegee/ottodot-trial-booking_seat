package httpx

import (
    "context"
    "errors"
    "strings"
    "time"

    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/observability"
)

// MinimumFillTime is how quickly a form may be submitted before this service
// stops believing a person filled it in.
//
// A second and a half is chosen against the slowest plausible person, not the
// fastest: picking a child and pressing a button takes longer than this even
// when somebody knows exactly what they are doing. Anything under it was not
// typed.
const MinimumFillTime = 1500 * time.Millisecond

// Signals are what a form reports about how it was filled in.
//
// All three are optional on the wire, and that is deliberate rather than lax.
// This layer catches naive scripts, and everything it could catch is something a
// determined caller can simply omit. The layers that actually hold, the token,
// the ownership check, the unique index, and the hold cap, work on properties
// nobody can decline to send.
type Signals struct {
    // Honeypot is a field a person never sees and never fills in. It has an
    // ordinary looking name on the wire for the same reason it is invisible: a
    // script fills in what looks fillable.
    Honeypot string `json:"website"`

    // FilledInMillis is how long the form was open before it was submitted.
    // Zero means the client did not measure, which is not evidence of anything
    // and is not refused.
    FilledInMillis int64 `json:"filled_in_ms"`

    // CaptchaToken is what the challenge widget produced.
    CaptchaToken string `json:"captcha_token"`
}

// BotCheck applies the three cooperative checks.
type BotCheck struct {
    verifier       captcha.Verifier
    minimumFill    time.Duration
    requireCaptcha bool
    counters       *Counters
}

// BotCheckSettings is how strict this service is being.
type BotCheckSettings struct {
    // Verifier is the challenge provider. Nil means no challenge is checked at
    // all, which is the shape a deployment without one runs in.
    Verifier captcha.Verifier

    // MinimumFill overrides MinimumFillTime. Zero means the constant.
    MinimumFill time.Duration

    // RequireCaptcha refuses a submission that carries no challenge token.
    //
    // It is off by default, and the reason is worth stating plainly: with it
    // off, a token that is sent is verified and a token that is absent is not
    // held against the caller, so the layer is real but optional. Turning it on
    // is a configuration change rather than a code change, which is what makes
    // the difference between the two easy to reason about.
    RequireCaptcha bool
}

// NewBotCheck wires the checks.
//
// Param:
// settings - BotCheckSettings (which checks apply and how strictly)
// counters - *Counters (where a refusal is recorded, nil for nowhere)
//
// Return:
//   - the check
//   - an error when a challenge is required and there is nothing to verify it
//     with, refused here rather than as a route that lets everything through
func NewBotCheck(settings BotCheckSettings, counters *Counters) (*BotCheck, error) {
    if settings.RequireCaptcha && settings.Verifier == nil {
        return nil, errors.New("httpx: a required challenge needs a verifier")
    }

    minimumFill := settings.MinimumFill
    if minimumFill <= 0 {
        minimumFill = MinimumFillTime
    }

    return &BotCheck{
        verifier:       settings.Verifier,
        minimumFill:    minimumFill,
        requireCaptcha: settings.RequireCaptcha,
        counters:       counters,
    }, nil
}

// Inspect decides whether this submission looks like a person.
//
// The order is cheapest first: the honeypot is a string comparison, the fill
// time is arithmetic, and the challenge is the only one that can reach out of
// the process. There is no reason to ask a third party about a submission that
// filled in a field nobody can see.
//
// Note:
//   - every refusal is the same generic failure. A script told which check
//     caught it is a script that gets past that check next time.
//   - a challenge provider that cannot be reached is a pass. Refusing every
//     parent because a third party is down turns their outage into ours, and
//     this layer was never the one holding anything up.
//
// Param:
// signals - Signals (what the form reported)
// callerAddress - string (passed through to the challenge provider, nothing else)
//
// Return:
//   - nil when the submission may go on
//   - ErrBotCheckFailed when the honeypot was filled or the form was submitted
//     faster than a person can
//   - captcha.ErrRefused when the challenge was not accepted
func (check *BotCheck) Inspect(ctx context.Context, signals Signals, callerAddress string) error {
    if strings.TrimSpace(signals.Honeypot) != "" {
        return check.refuse(ErrBotCheckFailed, observability.CheckHoneypot)
    }

    if signals.FilledInMillis > 0 && time.Duration(signals.FilledInMillis)*time.Millisecond < check.minimumFill {
        return check.refuse(ErrBotCheckFailed, observability.CheckFillTime)
    }

    token := strings.TrimSpace(signals.CaptchaToken)

    if token == "" {
        if check.requireCaptcha {
            return check.refuse(ErrBotCheckFailed, observability.CheckCaptcha)
        }

        return nil
    }

    if check.verifier == nil {
        // A token arrived and there is nothing to check it against. It is not
        // evidence either way, so it is ignored rather than believed.
        return nil
    }

    err := check.verifier.Verify(ctx, token, callerAddress)

    if errors.Is(err, captcha.ErrUnavailable) {
        return nil
    }

    if err != nil {
        return check.refuse(err, observability.CheckCaptcha)
    }

    return nil
}

// refuse records the rejection and hands the reason back, so the count and the
// answer can never disagree.
//
// The check that caught it goes to the metric and never to the caller. An
// operator needs to know which layer is doing the work, and a script told which
// layer caught it is a script that gets past that layer next time.
func (check *BotCheck) refuse(reason error, caughtBy string) error {
    if check.counters != nil {
        check.counters.BotRejected(caughtBy)
    }

    return reason
}
