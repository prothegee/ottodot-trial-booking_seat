package httpx_test

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "ottodot-trial-booking/backend/internal/httpx"
)

// recordedRequest is one observation the measure middleware made.
type recordedRequest struct {
    route   string
    method  string
    status  int
    seconds float64
}

// recordingSink captures what the surface published, so a case can assert on the
// label values rather than on a scrape it would have to parse.
type recordingSink struct {
    requests []recordedRequest
    denials  []string
    limits   []string
    checks   []string
    lookups  [][2]string
    panics   int
}

func (sink *recordingSink) AccessDenied(reason string) {
    sink.denials = append(sink.denials, reason)
}

func (sink *recordingSink) RateLimitRejected(scope string) {
    sink.limits = append(sink.limits, scope)
}

func (sink *recordingSink) BotCheckRejected(check string) {
    sink.checks = append(sink.checks, check)
}

func (sink *recordingSink) CacheLookup(endpoint string, result string) {
    sink.lookups = append(sink.lookups, [2]string{endpoint, result})
}

func (sink *recordingSink) NotModified(_ string) {}

func (sink *recordingSink) PanicRecovered() {
    sink.panics++
}

func (sink *recordingSink) RequestObserved(route string, method string, status int, seconds float64) {
    sink.requests = append(sink.requests, recordedRequest{route: route, method: method, status: status, seconds: seconds})
}

func TestMeasure(t *testing.T) {
    t.Run("integration: the observation carries the route, the method, and the status", func(t *testing.T) {
        sink := &recordingSink{}
        counters := httpx.NewCounters(sink)

        handler := httpx.Chain(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
            response.WriteHeader(http.StatusConflict)
        }), httpx.Measure(httpx.CreateBookingPath, counters))

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil))

        if len(sink.requests) != 1 {
            t.Fatalf("%d requests were observed", len(sink.requests))
        }

        observed := sink.requests[0]

        if observed.route != httpx.CreateBookingPath {
            t.Errorf("the route label is %q", observed.route)
        }

        if observed.method != http.MethodPost || observed.status != http.StatusConflict {
            t.Errorf("the observation reads %s %d", observed.method, observed.status)
        }
    })

    t.Run("edge: the label is the registered pattern rather than the path", func(t *testing.T) {
        // This is the whole design of the middleware. The path carries a booking
        // identifier, and a label carrying one is a time series per booking:
        // both a leak of who booked what and a way to exhaust the monitoring
        // host over a weekend.
        sink := &recordingSink{}

        handler := httpx.Chain(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
            response.WriteHeader(http.StatusOK)
        }), httpx.Measure(httpx.BookingPath, httpx.NewCounters(sink)))

        handler.ServeHTTP(httptest.NewRecorder(),
            httptest.NewRequest(http.MethodGet, "/api/v1/bookings/0192a000-0000-7000-8000-000000000031", nil))

        if sink.requests[0].route != httpx.BookingPath {
            t.Fatalf("the route label is %q, which carries an identifier", sink.requests[0].route)
        }
    })

    t.Run("edge: a handler that writes no status is observed as the 200 that goes out", func(t *testing.T) {
        sink := &recordingSink{}

        handler := httpx.Chain(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}),
            httpx.Measure(httpx.ClassListPath, httpx.NewCounters(sink)))

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if sink.requests[0].status != http.StatusOK {
            t.Fatalf("a silent handler was observed as %d", sink.requests[0].status)
        }
    })

    t.Run("behaviour: a handler that panics is still timed", func(t *testing.T) {
        // A route that falls over is exactly the one whose latency somebody will
        // want to look at afterwards.
        sink := &recordingSink{}
        counters := httpx.NewCounters(sink)

        handler := httpx.Chain(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
            panic("the handler fell over")
        }), httpx.Recover(nil, counters), httpx.Measure(httpx.PayBookingPath, counters))

        handler.ServeHTTP(httptest.NewRecorder(),
            httptest.NewRequest(http.MethodPost, "/api/v1/bookings/0192a000/payments", nil))

        if len(sink.requests) != 1 {
            t.Fatalf("%d requests were observed for one that panicked", len(sink.requests))
        }

        if sink.panics != 1 {
            t.Fatalf("%d panics were counted", sink.panics)
        }
    })

    t.Run("unit: no counters means no wrapper at all", func(t *testing.T) {
        // A nil here is a test that did not ask for metrics, and it must cost
        // nothing rather than being wrapped in a middleware that does nothing.
        reached := false

        handler := httpx.Chain(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
            reached = true
        }), httpx.Measure(httpx.ClassListPath, nil))

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if !reached {
            t.Fatal("the handler was not reached")
        }
    })
}

func TestCountersPublishTheirLabels(t *testing.T) {
    t.Run("unit: every count reaches the sink with the value it was given", func(t *testing.T) {
        sink := &recordingSink{}
        counters := httpx.NewCounters(sink)

        counters.AccessDenied("not_your_child")
        counters.RateLimited("subject")
        counters.BotRejected("fill_time")
        counters.CacheHit("classes")
        counters.CacheMiss("classes")
        counters.CacheError("classes")

        if len(sink.denials) != 1 || sink.denials[0] != "not_your_child" {
            t.Errorf("the denials read %v", sink.denials)
        }

        if len(sink.limits) != 1 || sink.limits[0] != "subject" {
            t.Errorf("the rate limits read %v", sink.limits)
        }

        if len(sink.checks) != 1 || sink.checks[0] != "fill_time" {
            t.Errorf("the bot checks read %v", sink.checks)
        }

        if len(sink.lookups) != 3 {
            t.Fatalf("%d cache lookups were published", len(sink.lookups))
        }

        for index, wanted := range []string{"hit", "miss", "error"} {
            if sink.lookups[index][1] != wanted {
                t.Errorf("lookup %d reads %q", index, sink.lookups[index][1])
            }
        }
    })

    t.Run("unit: the local counts and the published ones do not disagree", func(t *testing.T) {
        // The two exist side by side so a test can assert on a number without
        // parsing a scrape. They would be worse than useless if they could drift.
        sink := &recordingSink{}
        counters := httpx.NewCounters(sink)

        counters.AccessDenied("not_your_child")
        counters.AccessDenied("forbidden_role")

        if counters.Snapshot().AccessDenied != int64(len(sink.denials)) {
            t.Fatalf("the local count is %d and the published one is %d",
                counters.Snapshot().AccessDenied, len(sink.denials))
        }
    })

    t.Run("unit: a nil sink records locally and publishes nowhere", func(t *testing.T) {
        counters := httpx.NewCounters(nil)

        counters.AccessDenied("not_your_child")
        counters.PanicRecovered()
        counters.RequestObserved(httpx.ClassListPath, http.MethodGet, http.StatusOK, 0.01)

        if counters.Snapshot().AccessDenied != 1 || counters.Snapshot().PanicRecovered != 1 {
            t.Fatalf("the counts read %+v", counters.Snapshot())
        }
    })
}
