package httpx

import (
    "net/http"
    "strings"
    "time"
)

// Measure times one route and records what went out.
//
// The route is bound in when the middleware is built rather than read off the
// request, and that is the whole design of this file. `/api/v1/bookings/{id}` is
// one label value, and `request.URL.Path` would be one label value per booking:
// a leak of who booked what, and a monitoring host running out of memory over a
// weekend.
//
// It sits outermost on each route, so the number covers everything a parent
// waits for, including the authentication, the rate limit, and the ownership
// read. A latency panel that measured only the handler would be reassuring and
// wrong.
//
// Note:
//   - building the middleware also creates that route's series at zero, since
//     this is where the pattern is already known and a panel with no series at
//     all looks the same as an exporter that stopped
//
// Param:
// route - string (the registered pattern, from the constants in paths.go)
// counters - *Counters (where the observation goes, nil for nowhere)
//
// Return:
//   - the middleware
func Measure(route string, counters *Counters) Middleware {
    return func(next http.Handler) http.Handler {
        if counters == nil {
            return next
        }

        // Created at zero here, where the pattern is already known, so the two
        // request panels draw on a service nobody has called yet instead of
        // reading No data, which is what a broken exporter looks like too.
        if method, _, found := strings.Cut(route, " "); found {
            counters.DeclareRoute(route, method)
        }

        return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
            started := time.Now()
            tracked := &statusWriter{ResponseWriter: response}

            // The observation is deferred so a handler that panics is still
            // timed. A route that falls over is exactly the one whose latency
            // somebody will want to look at afterwards.
            defer func() {
                counters.RequestObserved(route, request.Method, statusOf(tracked), time.Since(started).Seconds())
            }()

            next.ServeHTTP(tracked, request)
        })
    }
}

// statusOf is what went out, treating a handler that wrote nothing at all as the
// 200 the server will send on its behalf.
func statusOf(tracked *statusWriter) int {
    if status := tracked.Status(); status != 0 {
        return status
    }

    return http.StatusOK
}
