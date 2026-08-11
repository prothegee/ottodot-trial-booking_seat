// Package httpx is the api's http surface: the routes, the middleware chain,
// and the one failure envelope every refusal is written in.
//
// It owns no rule. Every decision it makes has already been made somewhere else
// and this package's job is to ask the right thing in the right order, then turn
// the answer into a status and a code. That boundary is why the domain packages
// can be tested without a request, and why this one can be tested without a
// database.
//
// Two groups of routes, split on purpose. Operations routes never move, so they
// sit unversioned at the root and live in internal/operations. Business routes
// carry /api/v1, so a breaking change becomes /api/v2 rather than a silent
// contract shift.
package httpx

// The business routes this package serves.
//
// They are constants because the frontend names the same paths, and a route that
// exists in one and not the other is a screen that does nothing. The method is
// part of the value, because Go's mux matches on both and a route registered
// without one answers every verb.
const (
    StudentsPath = "GET /api/v1/students"

    ClassListPath = "GET /api/v1/classes"
    ClassPath     = "GET /api/v1/classes/{classId}"
    RosterPath    = "GET /api/v1/classes/{classId}/roster"

    CreateBookingPath = "POST /api/v1/bookings"
    BookingPath       = "GET /api/v1/bookings/{bookingId}"
    CancelBookingPath = "DELETE /api/v1/bookings/{bookingId}"
    BookingEventsPath = "GET /api/v1/bookings/{bookingId}/events"
    PayBookingPath    = "POST /api/v1/bookings/{bookingId}/payments"

    AdminQueuePath    = "GET /api/v1/admin/queue"
    AdminBookingsPath = "GET /api/v1/admin/bookings"

    // TelemetryPath is where the client posts what it saw. It is a write in
    // every sense that matters here, so it carries the origin check and the
    // write rate limit like the rest of them.
    TelemetryPath = "POST /api/v1/telemetry"
)

// MetricsPath is where Prometheus scrapes.
//
// It sits outside /api/v1 with the other operations routes, because a metric
// name is not part of the contract the client is written against and versioning
// it would suggest otherwise.
const MetricsPath = "GET /metrics"

// The path parameters those routes carry.
const (
    classIDParameter   = "classId"
    bookingIDParameter = "bookingId"
)

// The two request headers this package reads that are not standard.
const (
    // IdempotencyKeyHeader is what makes a retried write produce one booking
    // and one charge.
    IdempotencyKeyHeader = "Idempotency-Key"

    // RequestIDHeader carries the identifier a failure is logged under, back to
    // the client. It is the only thing an internal_error tells anybody, and it
    // is what turns "something broke" into a line an operator can find.
    RequestIDHeader = "X-Request-Id"
)
