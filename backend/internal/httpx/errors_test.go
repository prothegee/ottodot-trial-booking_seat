package httpx_test

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/catalogue"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/ratelimit"
    "ottodot-trial-booking/backend/internal/roster"
)

// The whole error contract, as a table.
//
// It is written out rather than derived, on purpose. This is the one place a
// reviewer can check the api against the document that describes it, and a table
// generated from the code would agree with the code by construction and prove
// nothing.
var errorContract = []struct {
    name   string
    err    error
    status int
    code   string
}{
    {"an expired access token", auth.ErrTokenExpired, http.StatusUnauthorized, httpx.CodeTokenExpired},
    {"a tampered access token", auth.ErrTokenInvalid, http.StatusUnauthorized, httpx.CodeTokenInvalid},
    {"a request with no token at all", auth.ErrNotAuthenticated, http.StatusUnauthorized, httpx.CodeTokenInvalid},
    {"a spent refresh token", auth.ErrTokenReused, http.StatusUnauthorized, httpx.CodeTokenReused},
    {"a parent role on an admin route", auth.ErrForbiddenRole, http.StatusForbidden, httpx.CodeForbiddenRole},
    {"another parent's child", httpx.ErrNotYourChild, http.StatusForbidden, httpx.CodeNotYourChild},

    {"a duplicate booking", booking.ErrAlreadyBooked, http.StatusConflict, httpx.CodeAlreadyBooked},
    {"a parent at the hold cap", booking.ErrTooManyHolds, http.StatusConflict, httpx.CodeTooManyHolds},
    {"a class with no room", booking.ErrClassFull, http.StatusConflict, httpx.CodeClassFull},
    {"a seat lost after paying", booking.ErrSeatLost, http.StatusConflict, httpx.CodeSeatLost},
    {"a booking that already moved on", booking.ErrNotHolding, http.StatusConflict, httpx.CodeInvalidRequest},
    {"a booking nobody has", booking.ErrBookingNotFound, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"a class nobody has", catalogue.ErrClassNotFound, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"a roster for a class nobody has", roster.ErrClassNotFound, http.StatusBadRequest, httpx.CodeInvalidRequest},

    {"a declined charge", payment.ErrDeclined, http.StatusPaymentRequired, httpx.CodePaymentDeclined},
    {"an unreachable provider", payment.ErrProviderUnavailable, http.StatusServiceUnavailable, httpx.CodeDependencyUnavailable},
    {"an unfinished earlier attempt", payment.ErrAttemptPending, http.StatusServiceUnavailable, httpx.CodeDependencyUnavailable},
    {"an amount this service does not charge", payment.ErrInvalidAmount, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"a malformed idempotency key", payment.ErrInvalidIdempotencyKey, http.StatusBadRequest, httpx.CodeInvalidRequest},

    {"a caller over the limit", httpx.ErrRateLimited, http.StatusTooManyRequests, httpx.CodeRateLimited},
    {"a submission the honeypot caught", httpx.ErrBotCheckFailed, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"a refused challenge", captcha.ErrRefused, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"a cross origin write", auth.ErrOriginRefused, http.StatusBadRequest, httpx.CodeInvalidRequest},
    {"an unreachable limiter", ratelimit.ErrUnavailable, http.StatusServiceUnavailable, httpx.CodeDependencyUnavailable},
    {"an unreachable cache", cache.ErrUnavailable, http.StatusServiceUnavailable, httpx.CodeDependencyUnavailable},
}

func TestEveryTypedFailureMapsToItsStatusAndCode(t *testing.T) {
    for _, expected := range errorContract {
        t.Run("unit: "+expected.name, func(t *testing.T) {
            failure := httpx.FailureFor(expected.err)

            if failure.Status != expected.status {
                t.Fatalf("answered %d, wanted %d", failure.Status, expected.status)
            }

            if failure.Code != expected.code {
                t.Fatalf("answered %q, wanted %q", failure.Code, expected.code)
            }
        })
    }

    t.Run("edge: a failure this api does not name becomes internal_error with no detail", func(t *testing.T) {
        failure := httpx.FailureFor(errors.New(`pq: relation "bookings" does not exist`))

        if failure.Status != http.StatusInternalServerError || failure.Code != httpx.CodeInternalError {
            t.Fatalf("an unknown failure answered %d %q", failure.Status, failure.Code)
        }

        if strings.Contains(failure.Message, "bookings") || strings.Contains(failure.Message, "pq") {
            t.Fatalf("the message carries the driver's words: %q", failure.Message)
        }
    })

    t.Run("edge: no message anywhere in the contract carries an identifier or a name", func(t *testing.T) {
        for _, expected := range errorContract {
            message := httpx.FailureFor(expected.err).Message

            for _, leak := range []string{"@", "uuid", studentOne, parentOne, "Alice", "Adi"} {
                if strings.Contains(message, leak) {
                    t.Fatalf("%s answers with %q, which carries %q", expected.name, message, leak)
                }
            }
        }
    })
}

func TestTheAuthPackageAgreesWithThisOne(t *testing.T) {
    // The auth package writes its own envelope for its own four routes, so the
    // two mappings exist side by side. This is what stops them drifting: every
    // failure auth can raise has to answer the same way on both sides, or a
    // client would see one code from the sign in route and a different one for
    // the same cause everywhere else.
    shared := []error{
        auth.ErrTokenExpired,
        auth.ErrTokenInvalid,
        auth.ErrTokenReused,
        auth.ErrTokenNotFound,
        auth.ErrNotAuthenticated,
        auth.ErrForbiddenRole,
        auth.ErrInvalidRequest,
        auth.ErrOriginRefused,
    }

    for _, err := range shared {
        t.Run("unit: "+err.Error(), func(t *testing.T) {
            theirs := auth.FailureFor(err)
            ours := httpx.FailureFor(err)

            if theirs.Status != ours.Status || theirs.Code != ours.Code {
                t.Fatalf("auth answers %d %q and httpx answers %d %q",
                    theirs.Status, theirs.Code, ours.Status, ours.Code)
            }
        })
    }
}

func TestWritingTheEnvelope(t *testing.T) {
    t.Run("integration: a refusal carries the code and the no-store header", func(t *testing.T) {
        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil)

        httpx.Deny(recorder, request, booking.ErrClassFull)

        if recorder.Code != http.StatusConflict {
            t.Fatalf("answered %d", recorder.Code)
        }

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("a refusal answered with Cache-Control %q, and a cached 401 ends a session for the next person",
                recorder.Header().Get("Cache-Control"))
        }
    })

    t.Run("behaviour: a rate limit refusal carries the wait in the body and the header", func(t *testing.T) {
        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil)

        failure := httpx.FailureFor(httpx.ErrRateLimited)
        failure.RetryAfterSeconds = 7

        httpx.WriteFailure(recorder, request, failure)

        if recorder.Header().Get("Retry-After") != "7" {
            t.Fatalf("the header reads %q", recorder.Header().Get("Retry-After"))
        }

        body := failureOf(t, recorder)

        if body.Error.RetryAfterSeconds != 7 {
            t.Fatalf("the body reports a wait of %d", body.Error.RetryAfterSeconds)
        }
    })

    t.Run("edge: only internal_error carries a request id", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        refused := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/classes"})
        body := failureOf(t, refused)

        if body.Error.Code != httpx.CodeTokenInvalid {
            t.Fatalf("an unauthenticated read answered %q", body.Error.Code)
        }

        if body.Error.RequestID != "" {
            t.Fatalf("an ordinary refusal carries a request id: %s", body.Error.RequestID)
        }
    })

    t.Run("edge: an ordinary refusal carries no optional field at all", func(t *testing.T) {
        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil)

        httpx.Deny(recorder, request, booking.ErrClassFull)

        for _, absent := range []string{"retry_after_seconds", "request_id", "booking_id"} {
            if strings.Contains(recorder.Body.String(), absent) {
                t.Fatalf("the body carries %q when it means nothing: %s", absent, recorder.Body.String())
            }
        }
    })
}
