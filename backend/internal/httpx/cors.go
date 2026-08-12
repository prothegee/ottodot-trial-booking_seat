package httpx

import (
    "net/http"
    "strconv"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

// The client is served from one port and this api answers on another, so every
// call a browser makes is cross origin. Without the headers below the browser
// refuses to hand the answer to the page, and every screen fails with a message
// that says nothing about why. curl never sees any of this, which is exactly why
// a suite that only ever used curl could pass while nothing worked in a browser.

// preflightMaxAge is how long a browser may remember the answer to a preflight.
//
// Ten minutes rather than a day: long enough that a parent clicking through a
// booking pays for one preflight instead of one per write, short enough that
// changing the allowed list during development is not something to wait out.
const preflightMaxAge = 10 * time.Minute

// allowedRequestHeaders is what a request may carry beyond what a browser lets
// through without asking.
//
// Content-Type is here because a json body needs it. Idempotency-Key is here
// because every write in this api is retried under one, and a header left out of
// this list is a header the browser never sends: the preflight is refused and
// the write never leaves the page. There is no Authorization header in this
// design, because the token travels as a cookie the page cannot read.
const allowedRequestHeaders = "Content-Type, If-None-Match, " + IdempotencyKeyHeader

// allowedMethods is every method this api serves. OPTIONS is included because
// the preflight itself is one.
const allowedMethods = "GET, POST, DELETE, OPTIONS"

// exposedResponseHeaders is what the page is allowed to read off an answer.
//
// A browser hides every header that is not named here. ETag is what the
// conditional request path needs, and the request id is what a parent reads back
// when reporting a failure.
const exposedResponseHeaders = "ETag, X-Request-Id, Retry-After"

// CrossOrigin answers a browser's origin questions, and refuses to answer them
// for anywhere else.
//
// Note:
//   - the origin is echoed rather than answered with a wildcard. A wildcard is
//     not allowed at all once credentials are involved, and the cookies this
//     service uses are credentials.
//   - Vary: Origin is always set, including on a refusal. Without it a cache in
//     front of this service could hand one origin's answer, headers and all, to
//     a request from another.
//   - a preflight is answered here and never reaches a handler. It carries no
//     cookie and no body, so there is nothing behind it to run.
//   - a refused origin is not answered with an error. It gets an ordinary
//     response with no cors headers on it, which the browser turns into the
//     failure. Refusing with a status would tell a probing page that the origin
//     was recognised and rejected, which is more than it needs to know.
//
// Param:
// guard - *auth.Guard (owns the one list of origins this service serves)
// next - http.Handler (what runs for anything that is not a preflight)
//
// Return:
//   - a handler that adds the headers a browser needs and answers preflights
func CrossOrigin(guard *auth.Guard, next http.Handler) http.Handler {
    return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        response.Header().Add("Vary", "Origin")

        origin := request.Header.Get("Origin")
        allowed := guard != nil && guard.OriginIsAllowed(origin)

        if allowed {
            response.Header().Set("Access-Control-Allow-Origin", origin)
            response.Header().Set("Access-Control-Allow-Credentials", "true")
            response.Header().Set("Access-Control-Expose-Headers", exposedResponseHeaders)
        }

        if request.Method != http.MethodOptions {
            next.ServeHTTP(response, request)

            return
        }

        // A preflight names the method and the headers it is asking about, and
        // those two headers are what makes it a preflight rather than an
        // ordinary OPTIONS request somebody sent by hand.
        response.Header().Add("Vary", "Access-Control-Request-Method")

        if allowed {
            response.Header().Set("Access-Control-Allow-Methods", allowedMethods)
            response.Header().Set("Access-Control-Allow-Headers", allowedRequestHeaders)
            response.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(preflightMaxAge.Seconds())))
        }

        response.WriteHeader(http.StatusNoContent)
    })
}
