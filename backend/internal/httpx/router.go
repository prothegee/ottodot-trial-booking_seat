package httpx

import (
    "errors"
    "net/http"

    "ottodot-trial-booking/backend/internal/auth"
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

    Classes  *ClassHandler
    Students *StudentHandler
    Bookings *BookingHandler
    Payments *PaymentHandler
    Roster   *RosterHandler
    Admin    *AdminHandler

    // Guard is what establishes the identity behind a request and what refuses
    // a write that did not come from this service's own page.
    Guard *auth.Guard

    // Limits applies the token buckets.
    Limits *Limits

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
    mux.Handle(StudentsPath, Chain(http.HandlerFunc(routes.Students.list), readChain...))
    mux.Handle(ClassListPath, Chain(http.HandlerFunc(routes.Classes.list), readChain...))
    mux.Handle(ClassPath, Chain(http.HandlerFunc(routes.Classes.one), readChain...))
    mux.Handle(BookingPath, Chain(http.HandlerFunc(routes.Bookings.read), readChain...))
    mux.Handle(BookingEventsPath, Chain(http.HandlerFunc(routes.Bookings.events), readChain...))

    // Parent facing writes.
    mux.Handle(CreateBookingPath, Chain(http.HandlerFunc(routes.Bookings.create), writeChain...))
    mux.Handle(CancelBookingPath, Chain(http.HandlerFunc(routes.Bookings.cancel), writeChain...))
    mux.Handle(PayBookingPath, Chain(http.HandlerFunc(routes.Payments.pay), writeChain...))

    // Operator reads. The roster is here rather than with the parent routes for
    // one reason: it is the only body in this api that carries a child's name
    // next to a seat.
    mux.Handle(RosterPath, Chain(http.HandlerFunc(routes.Roster.read), adminChain...))
    mux.Handle(AdminQueuePath, Chain(http.HandlerFunc(routes.Admin.queueDepth), adminChain...))
    mux.Handle(AdminBookingsPath, Chain(http.HandlerFunc(routes.Admin.worklist), adminChain...))

    // The two outermost wrappers go on once, around everything, including the
    // operations routes. A panic in a readiness probe should no more take the
    // process down than a panic in a booking.
    return Chain(mux, WithRequestID, Recover(routes.Recovery)), nil
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
