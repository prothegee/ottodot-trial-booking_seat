package httpx

import (
    "errors"
    "net/http"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/operations"
    "ottodot-trial-booking/backend/internal/ratelimit"
)

// Routes is everything that answers on the api port.
//
// It is one struct rather than a list of arguments because the wiring is the
// part that has to be read carefully: every route below is either behind the
// guard or deliberately not, and a reviewer should be able to see which in one
// screen.
type Routes struct {
    // Operations serves liveness, readiness, and build identity. None of them
    // is authenticated and none of them may be: a readiness probe that needed a
    // working database to answer could never report that the database is down.
    Operations *operations.Handler

    // Auth serves the four session routes. It registers itself, because the
    // cookies and the origin check are its own business.
    Auth *auth.Handler

    Classes   *ClassHandler
    Students  *StudentHandler
    Bookings  *BookingHandler
    Payments  *PaymentHandler
    Roster    *RosterHandler
    Admin     *AdminHandler
    Telemetry *TelemetryHandler

    // Guard is what establishes the identity behind a request and what refuses
    // a write that did not come from this service's own page.
    Guard *auth.Guard

    // Limits applies the token buckets.
    Limits *Limits

    // Counters is where this surface's numbers go. Nil means nowhere, which is
    // acceptable in a test and not in a running service.
    Counters *Counters

    // Exposition is what Prometheus scrapes. Nil leaves /metrics unregistered,
    // which is what a test wants and what a running service must never have.
    Exposition http.Handler

    // Faults is the development only injection surface. Nil is the ordinary
    // state: when it is nil the routes are never registered at all, so the
    // surface answers a plain not found rather than a refusal that would confirm
    // it exists.
    Faults *faults.Handler

    // Recovery is where a panic is written down. Nil means nowhere, which is
    // acceptable in a test and not in a running service.
    Recovery ReportFunc
}

// ErrIncompleteRoutes means something the router needs was not handed to it.
var ErrIncompleteRoutes = errors.New("httpx: the router is missing a handler it needs")

// NewRouter builds the api's whole surface.
//
// The chain around every business route is the same, in this order, and the
// order is the design:
//
//	the request id is stamped first, so every failure below it can be found in a
//	log
//
//	the panic recovery wraps everything after it, so a handler falling over costs
//	one request rather than the process
//
//	the origin check refuses a write that did not come from this service's own
//	page, before any work is done for it
//
//	authentication establishes who the caller is, and it has to come before the
//	rate limit or every request would be counted against an address rather than
//	an account
//
//	the rate limit is last, so the bucket it spends is the right one
//
// Param:
// routes - Routes (every handler, the guard, and the limiter)
//
// Return:
//   - the mux, ready to serve
//   - ErrIncompleteRoutes when anything above is missing
func NewRouter(routes Routes) (http.Handler, error) {
    if err := routes.validate(); err != nil {
        return nil, err
    }

    mux := http.NewServeMux()

    routes.Operations.Register(mux)
    routes.Auth.Register(mux)

    if routes.Exposition != nil {
        // The scrape route is deliberately unauthenticated, like the other
        // operations routes. It is published on the loopback address only, and
        // everything on it is a bounded enumeration by rule, so there is nothing
        // here worth a token.
        mux.Handle(MetricsPath, routes.Exposition)
    }

    readChain := []Middleware{
        routes.Guard.Authenticate,
        routes.Limits.Guard(ratelimit.ReadRule),
    }

    writeChain := []Middleware{
        routes.Guard.CheckOrigin,
        routes.Guard.Authenticate,
        routes.Limits.Guard(ratelimit.WriteRule),
    }

    adminChain := []Middleware{
        requireAdmin(routes.Guard),
        routes.Limits.Guard(ratelimit.ReadRule),
    }

    // Parent facing reads.
    routes.handle(mux, StudentsPath, routes.Students.list, readChain)
    routes.handle(mux, ClassListPath, routes.Classes.list, readChain)
    routes.handle(mux, ClassPath, routes.Classes.one, readChain)
    routes.handle(mux, BookingPath, routes.Bookings.read, readChain)
    routes.handle(mux, BookingEventsPath, routes.Bookings.events, readChain)

    // Parent facing writes.
    routes.handle(mux, CreateBookingPath, routes.Bookings.create, writeChain)
    routes.handle(mux, CancelBookingPath, routes.Bookings.cancel, writeChain)
    routes.handle(mux, PayBookingPath, routes.Payments.pay, writeChain)

    if routes.Telemetry != nil {
        routes.handle(mux, TelemetryPath, routes.Telemetry.record, writeChain)
    }

    // Operator reads. The roster is here rather than with the parent routes for
    // one reason: it is the only body in this api that carries a child's name
    // next to a seat.
    routes.handle(mux, RosterPath, routes.Roster.read, adminChain)
    routes.handle(mux, AdminQueuePath, routes.Admin.queueDepth, adminChain)
    routes.handle(mux, AdminBookingsPath, routes.Admin.worklist, adminChain)

    if routes.Faults != nil {
        // Registered only when every guard has already passed, which is decided
        // by the process that wires this and not here. It carries the admin role
        // check and the write rate limit, exactly like every other mutation.
        routes.Faults.Register(mux, func(next http.Handler) http.Handler {
            return Chain(next, requireAdmin(routes.Guard), routes.Limits.Guard(ratelimit.WriteRule))
        })
    }

    // The two outermost wrappers go on once, around everything, including the
    // operations routes. A panic in a readiness probe should no more take the
    // process down than a panic in a booking.
    return Chain(mux, WithRequestID, Recover(routes.Recovery, routes.Counters)), nil
}

// handle registers one route with its guard chain and its timer.
//
// The timer goes on outermost, so the number covers everything a parent waits
// for rather than only the handler, and the route label is the registered
// pattern rather than the path that was asked for.
func (routes Routes) handle(mux *http.ServeMux, pattern string, handler http.HandlerFunc, chain []Middleware) {
    wrapped := append([]Middleware{Measure(pattern, routes.Counters)}, chain...)

    mux.Handle(pattern, Chain(handler, wrapped...))
}

// requireAdmin is the admin role check as a Middleware.
//
// It wraps Authenticate rather than sitting next to it, which is the guard's own
// doing: an admin route cannot be wired in a way that checks the role without
// checking the token.
func requireAdmin(guard *auth.Guard) Middleware {
    return func(next http.Handler) http.Handler {
        return guard.RequireRole(auth.RoleAdmin, next)
    }
}

// validate refuses a router that would answer with a nil dereference.
func (routes Routes) validate() error {
    missing := routes.Operations == nil ||
        routes.Auth == nil ||
        routes.Classes == nil ||
        routes.Students == nil ||
        routes.Bookings == nil ||
        routes.Payments == nil ||
        routes.Roster == nil ||
        routes.Admin == nil ||
        routes.Guard == nil ||
        routes.Limits == nil

    if missing {
        return ErrIncompleteRoutes
    }

    return nil
}
