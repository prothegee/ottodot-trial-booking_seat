// Package captcha is the last bot prevention layer, and the weakest one.
//
// It is here in the shape a real provider would have rather than as a real
// integration, for the same reason the payment provider is: a reviewer must be
// able to clone this repository and run everything without an account anywhere.
// Swapping the mock for Turnstile or hCaptcha is one implementation of one
// interface, and nothing else in the service changes.
//
// It is deliberately the last layer. Everything above it, the token, the
// ownership check, the rate limit, and the hold cap, works on properties a bot
// cannot argue with. A challenge only raises the cost of pretending to be a
// person, so nothing important is allowed to rest on it alone.
package captcha

import (
    "context"
    "errors"
)

// The failures this package can report.
var (
    // ErrInvalidRequest means the call was refused before anything was
    // checked: no token where one was required.
    ErrInvalidRequest = errors.New("captcha: the request carries no challenge token")

    // ErrRefused means the provider looked at the token and said no.
    ErrRefused = errors.New("captcha: the challenge token was not accepted")

    // ErrUnavailable means the provider could not be reached.
    //
    // It is separate from a refusal because the two mean opposite things: one
    // is evidence about the caller, the other is evidence about this service.
    // The http layer treats an unreachable challenge as a pass, since refusing
    // every parent because a third party is down is a worse outage than letting
    // a bot through a layer that was never load bearing.
    ErrUnavailable = errors.New("captcha: the provider could not be reached")
)

// Verifier is the one method a challenge provider has.
//
// The signature is Turnstile's shape: the token the widget produced, and the
// caller address the provider uses to notice one token being replayed from
// somewhere else. Nothing else about the request is shared, because nothing else
// is any of the provider's business.
type Verifier interface {
    // Verify decides whether this token stands for a person.
    //
    // Return:
    //   - nil when the challenge was passed
    //   - ErrRefused when it was not
    //   - ErrUnavailable when the provider never answered
    Verify(ctx context.Context, token string, callerAddress string) error
}
