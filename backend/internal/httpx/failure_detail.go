package httpx

import (
    "net/http"

    "ottodot-trial-booking/backend/internal/observability"
)

// ReportFailures writes down the error behind an internal_error.
//
// The client is told a code and a request id and nothing else, on purpose. That
// leaves whoever has to fix it with a request id and no reason, which is how a
// missing schema looks exactly like a broken password check. This middleware is
// the other half: the answer stays empty and the log gets the detail.
//
// Note:
//   - only internal_error is reported. Every other refusal is a decision this
//     service made deliberately, and a log line for each one would bury the ones
//     nobody meant.
//   - a panic is not reported here. Recover already wrote that one down, and it
//     never reaches the refusal path.
//
// Param:
// report - ReportFunc (where the detail goes, nil for nowhere)
//
// Return:
//   - the middleware
func ReportFailures(report ReportFunc) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
            carrying, detail := observability.WithFailureDetail(request.Context())

            next.ServeHTTP(response, request.WithContext(carrying))

            if report == nil || detail.Err() == nil {
                return
            }

            report(RequestIDFrom(request.Context()), detail.Err())
        })
    }
}
