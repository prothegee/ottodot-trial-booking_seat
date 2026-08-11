package httpx

import (
    "errors"
    "net/http"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/catalogue"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/ratelimit"
    "ottodot-trial-booking/backend/internal/roster"
)

// The complete set of codes this api answers with.
//
// It is closed, and that is the contract: the client switches on a code it
// already knows and never parses prose. Adding one here is a change to the
// frontend as well, which is why they are listed in one place rather than spelt
// out at each call site.
const (
    CodeInvalidRequest        = "invalid_request"
    CodeTokenExpired          = "token_expired"
    CodeTokenInvalid          = "token_invalid"
    CodeTokenReused           = "token_reused"
    CodeNotYourChild          = "not_your_child"
    CodeForbiddenRole         = "forbidden_role"
    CodePaymentDeclined       = "payment_declined"
    CodeAlreadyBooked         = "already_booked"
    CodeTooManyHolds          = "too_many_holds"
    CodeClassFull             = "class_full"
    CodeSeatLost              = "seat_lost"
    CodeRateLimited           = "rate_limited"
    CodeInternalError         = "internal_error"
    CodeDependencyUnavailable = "dependency_unavailable"
)

// The failures this package raises for itself, where no domain package has a
// sentinel that fits.
var (
    // ErrNotYourChild means the token subject does not own the student the
    // request named.
    ErrNotYourChild = errors.New("httpx: that student is not on this account")

    // ErrRateLimited means the caller's bucket was empty.
    ErrRateLimited = errors.New("httpx: this caller has asked too often")

    // ErrBotCheckFailed means the honeypot or the fill timer said this was not
    // a person.
    //
    // It maps to the generic refusal rather than to a code of its own, on
    // purpose. A bot told exactly which check caught it is a bot that gets past
    // that check next time.
    ErrBotCheckFailed = errors.New("httpx: the submission did not look like a person")
)

// Failure is one refusal as it appears on the wire.
type Failure struct {
    Status  int
    Code    string
    Message string

    // RetryAfterSeconds is how long the client should wait. It is only ever set
    // on a rate limit refusal, and it is the reason a refused client backs off
    // instead of retrying into the same wall.
    RetryAfterSeconds int

    // BookingID names the booking a refusal points at. Only already_booked
    // carries one, so the duplicate notice can link to the booking the parent
    // already has instead of sending them looking for it.
    BookingID string
}

// FailureFor turns any failure in this service into the answer a client gets.
//
// The order of the cases is the order of specificity, not the order of the code
// table. Anything this function does not name becomes internal_error, which
// carries no detail at all: a driver message reaching a client is how a table
// name ends up in a screenshot.
//
// Param:
// err - error (whatever failed)
//
// Return:
//   - the status, code, and wording for that failure
func FailureFor(err error) Failure {
    if failure, matched := authFailureFor(err); matched {
        return failure
    }

    if failure, matched := bookingFailureFor(err); matched {
        return failure
    }

    if failure, matched := paymentFailureFor(err); matched {
        return failure
    }

    if failure, matched := surfaceFailureFor(err); matched {
        return failure
    }

    return Failure{
        Status:  http.StatusInternalServerError,
        Code:    CodeInternalError,
        Message: "something went wrong on our side",
    }
}

// authFailureFor covers who the caller is and whether they may act.
//
// It maps the auth package's sentinels rather than calling auth.FailureFor,
// because that function answers internal_error for anything it does not know and
// chaining it would swallow every other package's failures. The test that keeps
// the two agreeing is what stops them drifting.
func authFailureFor(err error) (Failure, bool) {
    switch {
    case errors.Is(err, auth.ErrTokenExpired):
        return Failure{http.StatusUnauthorized, CodeTokenExpired,
            "your session needs refreshing", 0, ""}, true

    case errors.Is(err, auth.ErrTokenReused):
        return Failure{http.StatusUnauthorized, CodeTokenReused,
            "this sign in was ended for safety, please sign in again", 0, ""}, true

    case errors.Is(err, auth.ErrTokenInvalid),
        errors.Is(err, auth.ErrTokenNotFound),
        errors.Is(err, auth.ErrNotAuthenticated):
        return Failure{http.StatusUnauthorized, CodeTokenInvalid,
            "please sign in again", 0, ""}, true

    case errors.Is(err, auth.ErrForbiddenRole):
        return Failure{http.StatusForbidden, CodeForbiddenRole,
            "this is not available on your account", 0, ""}, true

    case errors.Is(err, ErrNotYourChild):
        return Failure{http.StatusForbidden, CodeNotYourChild,
            "that student is not on this account", 0, ""}, true
    }

    return Failure{}, false
}

// bookingFailureFor covers the seat.
//
// Every one of these is a business answer a parent can act on, which is why none
// of them is a 500 and none of them is worded as an error.
func bookingFailureFor(err error) (Failure, bool) {
    switch {
    case errors.Is(err, booking.ErrAlreadyBooked):
        return Failure{http.StatusConflict, CodeAlreadyBooked,
            "this child already has a booking for this class", 0, ""}, true

    case errors.Is(err, booking.ErrTooManyHolds):
        return Failure{http.StatusConflict, CodeTooManyHolds,
            "finish or cancel a booking before starting another", 0, ""}, true

    case errors.Is(err, booking.ErrClassFull):
        return Failure{http.StatusConflict, CodeClassFull,
            "this class filled while you were choosing", 0, ""}, true

    case errors.Is(err, booking.ErrSeatLost):
        return Failure{http.StatusConflict, CodeSeatLost,
            "the last seat went to someone else, your payment is being returned", 0, ""}, true

    case errors.Is(err, booking.ErrBookingNotFound),
        errors.Is(err, booking.ErrClassNotFound),
        errors.Is(err, booking.ErrStudentNotFound),
        errors.Is(err, catalogue.ErrClassNotFound),
        errors.Is(err, roster.ErrClassNotFound):
        // A not-found is answered as a bad request rather than as a 404 with a
        // code of its own. Two reasons: an id a caller invented is a malformed
        // request, and an api that distinguishes "no such booking" from "not
        // yours" is an api that can be asked which identifiers exist.
        return Failure{http.StatusBadRequest, CodeInvalidRequest,
            "that request was not accepted", 0, ""}, true

    case errors.Is(err, booking.ErrNotHolding),
        errors.Is(err, booking.ErrInvalidTransition):
        return Failure{http.StatusConflict, CodeInvalidRequest,
            "this booking has already moved on", 0, ""}, true

    case errors.Is(err, booking.ErrInvalidRequest),
        errors.Is(err, catalogue.ErrInvalidRequest),
        errors.Is(err, roster.ErrInvalidRequest):
        return Failure{http.StatusBadRequest, CodeInvalidRequest,
            "that request was not accepted", 0, ""}, true
    }

    return Failure{}, false
}

// paymentFailureFor covers the money.
func paymentFailureFor(err error) (Failure, bool) {
    switch {
    case errors.Is(err, payment.ErrDeclined):
        return Failure{http.StatusPaymentRequired, CodePaymentDeclined,
            "that payment was declined, no money was taken", 0, ""}, true

    case errors.Is(err, payment.ErrProviderUnavailable):
        // Deliberately not a decline. Nobody knows whether money moved, and
        // telling a parent their payment failed when it may have succeeded is
        // the worst answer available.
        return Failure{http.StatusServiceUnavailable, CodeDependencyUnavailable,
            "we could not reach the payment service, please try again shortly", 0, ""}, true

    case errors.Is(err, payment.ErrAttemptPending):
        return Failure{http.StatusServiceUnavailable, CodeDependencyUnavailable,
            "an earlier attempt has not finished, please try again shortly", 0, ""}, true

    case errors.Is(err, payment.ErrInvalidAmount),
        errors.Is(err, payment.ErrInvalidCurrency),
        errors.Is(err, payment.ErrInvalidIdempotencyKey),
        errors.Is(err, payment.ErrIdempotencyConflict),
        errors.Is(err, payment.ErrInvalidRequest),
        errors.Is(err, payment.ErrBookingNotFound):
        return Failure{http.StatusBadRequest, CodeInvalidRequest,
            "that request was not accepted", 0, ""}, true
    }

    return Failure{}, false
}

// surfaceFailureFor covers this package's own refusals and the two dependencies
// that are optimisations rather than authorities.
func surfaceFailureFor(err error) (Failure, bool) {
    switch {
    case errors.Is(err, ErrRateLimited):
        return Failure{http.StatusTooManyRequests, CodeRateLimited,
            "too many requests, please wait a moment", 0, ""}, true

    case errors.Is(err, ErrBotCheckFailed),
        errors.Is(err, captcha.ErrRefused),
        errors.Is(err, captcha.ErrInvalidRequest),
        errors.Is(err, auth.ErrInvalidRequest),
        errors.Is(err, auth.ErrOriginRefused):
        return Failure{http.StatusBadRequest, CodeInvalidRequest,
            "that request was not accepted", 0, ""}, true

    case errors.Is(err, ratelimit.ErrUnavailable),
        errors.Is(err, cache.ErrUnavailable),
        errors.Is(err, queue.ErrInvalidRequest):
        return Failure{http.StatusServiceUnavailable, CodeDependencyUnavailable,
            "the service is having trouble, please try again shortly", 0, ""}, true
    }

    return Failure{}, false
}
