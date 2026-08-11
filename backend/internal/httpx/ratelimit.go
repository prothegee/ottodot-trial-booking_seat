package httpx

import (
    "net"
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/observability"
    "ottodot-trial-booking/backend/internal/ratelimit"
)

// Limits applies the token buckets to a request.
//
// Two buckets are checked, not one. The subject bucket is the one that matters:
// a parent id is an account this service issued, so it cannot be changed by
// dialling from somewhere else. The address bucket is the weaker backstop, and
// it is what covers a flood arriving before any token exists.
type Limits struct {
    limiter  ratelimit.Limiter
    clock    func() time.Time
    counters *Counters
}

// NewLimits wires the middleware.
//
// Param:
// limiter - ratelimit.Limiter (the shared one in production, the fake in tests)
// clock - func() time.Time (nil means the real clock)
// counters - *Counters (where a refusal is recorded, nil for nowhere)
//
// Return:
//   - the middleware source
//   - ErrRateLimited when there is no limiter, refused here rather than as a
//     route with no limit that looks like it has one
func NewLimits(limiter ratelimit.Limiter, clock func() time.Time, counters *Counters) (*Limits, error) {
    if limiter == nil {
        return nil, ErrRateLimited
    }

    if clock == nil {
        clock = time.Now
    }

    return &Limits{limiter: limiter, clock: clock, counters: counters}, nil
}

// Guard refuses a caller that has asked too often.
//
// What happens when the limiter itself is unreachable is decided by the method,
// not by the route, so it cannot be configured wrongly one endpoint at a time:
//
//	a safe method fails open. Refusing every read because Redis is down turns a
//	cache outage into a site outage, and a read costs a cached body
//
//	a write fails closed. A write costs a transaction and a seat, and an
//	unlimited write path during an outage is exactly when a flood does damage
//
// Param:
// rule - ratelimit.Bucket (the burst and the refill for this group of routes)
// next - http.Handler (what runs when the caller is under the limit)
//
// Return:
//   - a handler that checks both buckets and then passes the request on
func (limits *Limits) Guard(rule ratelimit.Bucket) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
            now := limits.clock()
            failOpen := isSafeMethod(request.Method)

            for _, bucket := range limits.bucketsFor(request) {
                decision, err := limits.limiter.Allow(request.Context(), bucket.key, rule, now)

                if err != nil {
                    if failOpen {
                        continue
                    }

                    Deny(response, request, ratelimit.ErrUnavailable)

                    return
                }

                if !decision.Allowed {
                    limits.refuse(response, request, decision, bucket.scope)

                    return
                }
            }

            next.ServeHTTP(response, request)
        })
    }
}

// bucket is one token bucket this request is counted against, and which of the
// two layers it belongs to.
//
// The scope travels with the key rather than being worked out again at the
// refusal, so the metric can never name a different bucket from the one that
// actually ran dry.
type bucket struct {
    key   string
    scope string
}

// bucketsFor is which buckets this request is counted against.
//
// A signed in request is counted against both its account and its address. An
// anonymous one has only the address, which is the whole reason the address
// bucket exists.
func (limits *Limits) bucketsFor(request *http.Request) []bucket {
    buckets := make([]bucket, 0, 2)

    if identity, carried := auth.IdentityFrom(request.Context()); carried {
        if key := ratelimit.SubjectKey(identity.ParentID); key != "" {
            buckets = append(buckets, bucket{key: key, scope: observability.ScopeSubject})
        }
    }

    if key := ratelimit.AddressKey(CallerAddress(request)); key != "" {
        buckets = append(buckets, bucket{key: key, scope: observability.ScopeAddress})
    }

    return buckets
}

// refuse answers a caller that is over the limit, with the wait it should
// honour.
func (limits *Limits) refuse(response http.ResponseWriter, request *http.Request, decision ratelimit.Decision, scope string) {
    if limits.counters != nil {
        limits.counters.RateLimited(scope)
    }

    failure := FailureFor(ErrRateLimited)
    failure.RetryAfterSeconds = int(decision.RetryAfter / time.Second)

    if failure.RetryAfterSeconds < 1 {
        failure.RetryAfterSeconds = 1
    }

    WriteFailure(response, request, failure)
}

// CallerAddress is the address a request came from, without its port.
//
// It is read from RemoteAddr and from nothing else. A forwarded-for header is
// written by whoever is dialling, so trusting one here would let any caller pick
// which bucket to spend, which is worse than having no address bucket at all.
// Behind a proxy this becomes the proxy's address, and that is the honest
// consequence: the address layer stops being useful and the subject layer, which
// was always the real one, carries on unchanged.
func CallerAddress(request *http.Request) string {
    host, _, err := net.SplitHostPort(request.RemoteAddr)
    if err != nil {
        return request.RemoteAddr
    }

    return host
}

// isSafeMethod reports whether a method changes nothing.
func isSafeMethod(method string) bool {
    return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
