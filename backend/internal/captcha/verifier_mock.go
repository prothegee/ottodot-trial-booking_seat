package captcha

import (
    "context"
    "strings"
    "sync"
)

// The tokens the mock recognises by name.
//
// A reviewer, or a test, produces any of the three answers by sending a string
// rather than by driving a browser. The frontend widget mints the passing one,
// which is why it is a fixed value and not a random string: a mock that accepted
// anything could not demonstrate a refusal at all.
const (
    // TokenPass is what the mock widget produces, and the only value that
    // passes.
    TokenPass = "mock-captcha-pass"

    // TokenFail is a token the provider refuses, so the refusal path can be
    // driven end to end.
    TokenFail = "mock-captcha-fail"

    // TokenUnavailable makes the provider unreachable, which is the case the
    // http layer has to treat as a pass rather than as a refusal.
    TokenUnavailable = "mock-captcha-unavailable"
)

// MockVerifier is the verifier every test and every demo runs against.
//
// It is deterministic on purpose. A challenge that passed at random would make a
// failing test a coin toss and a demonstration a matter of luck, so
// this one decides from the token alone and answers the same way every time.
type MockVerifier struct {
    mutex sync.Mutex

    checks   int
    refusals int
}

// NewMockVerifier builds a verifier that follows the rule above.
func NewMockVerifier() *MockVerifier {
    return &MockVerifier{}
}

// Checks is how many tokens this verifier was asked about. It is the mock's own
// record, and it is what proves a request refused by an earlier layer never
// reached this one.
func (verifier *MockVerifier) Checks() int {
    verifier.mutex.Lock()
    defer verifier.mutex.Unlock()

    return verifier.checks
}

// Refusals is how many of those it turned away.
func (verifier *MockVerifier) Refusals() int {
    verifier.mutex.Lock()
    defer verifier.mutex.Unlock()

    return verifier.refusals
}

// Verify decides whether this token stands for a person.
//
// Note:
//   - the caller address is accepted and ignored. A real provider uses it to
//     notice one token being redeemed from somewhere else, and the parameter is
//     here so swapping in that provider changes no call site.
func (verifier *MockVerifier) Verify(_ context.Context, token string, _ string) error {
    trimmed := strings.TrimSpace(token)

    verifier.mutex.Lock()
    verifier.checks++
    verifier.mutex.Unlock()

    if trimmed == "" {
        return verifier.refuse(ErrInvalidRequest)
    }

    switch trimmed {
    case TokenPass:
        return nil

    case TokenUnavailable:
        // Not counted as a refusal. Nobody was turned away, this service simply
        // learned nothing about them.
        return ErrUnavailable
    }

    return verifier.refuse(ErrRefused)
}

// refuse records the refusal and hands the reason back, so the two never drift
// apart.
func (verifier *MockVerifier) refuse(reason error) error {
    verifier.mutex.Lock()
    verifier.refusals++
    verifier.mutex.Unlock()

    return reason
}
