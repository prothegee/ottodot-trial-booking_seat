package httpx

import (
    "context"
    "net/http"

    "ottodot-trial-booking/backend/internal/identifier"
)

// requestIDKey is the context key a request's identifier is carried under.
//
// It is an unexported type rather than a string, so nothing outside this package
// can write a value under it. An identifier a handler could set for itself is an
// identifier that cannot be trusted to match a log line.
type requestIDKey struct{}

// RequestIDFrom reads the identifier this request is being logged under.
//
// Return:
//   - the identifier when the request came through WithRequestID
//   - an empty string otherwise, which a caller treats as nothing to report
//     rather than as a failure
func RequestIDFrom(ctx context.Context) string {
    held, carried := ctx.Value(requestIDKey{}).(string)
    if !carried {
        return ""
    }

    return held
}

// WithRequestID gives every request an identifier and puts it on the response.
//
// It is the first middleware in the chain, and it has to be: everything after it
// can fail, and a failure with no identifier is a failure nobody can find in a
// log.
//
// The identifier is minted here and never read from the request. A client
// supplied one would let a caller write anything into this service's logs,
// including a value that looks like somebody else's request.
//
// Note:
//   - an identifier source that fails does not stop the request. The service
//     answers with no identifier rather than refusing a parent a booking
//     because randomness was briefly unavailable, and the empty value is
//     visible in a log as an absence.
//
// Param:
// next - http.Handler (what runs with the identifier in its context)
//
// Return:
//   - a handler that stamps the identifier and passes the request on
func WithRequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        minted, err := identifier.NewUUIDv7()
        if err != nil {
            minted = ""
        }

        if minted != "" {
            response.Header().Set(RequestIDHeader, minted)
        }

        next.ServeHTTP(response, request.WithContext(
            context.WithValue(request.Context(), requestIDKey{}, minted)))
    })
}
