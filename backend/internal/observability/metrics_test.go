package observability_test

import (
    "net/http"
    "net/http/httptest"
    "regexp"
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"

    "ottodot-trial-booking/backend/internal/observability"
)

// uuidPattern is what a leaked identifier looks like in a label value.
//
// Every identifier in this service is a uuid version 7, so one appearing in the
// exposition is either a metric label built from a request or a help string that
// quoted one. Both are the same mistake.
var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// exposition scrapes a registry the way Prometheus does and hands back the text.
func exposition(t *testing.T, registry *prometheus.Registry) string {
    t.Helper()

    recorder := httptest.NewRecorder()
    observability.Handler(registry).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

    if recorder.Code != http.StatusOK {
        t.Fatalf("the scrape answered %d, so a collector failed", recorder.Code)
    }

    return recorder.Body.String()
}

// everyMetricName is the whole catalogue, which the exposition test walks.
//
// It is written out rather than derived, on purpose. A list derived from the
// same constants the code registers would pass even if half of them were never
// registered at all, which is the exact failure this test exists to catch.
var everyMetricName = []string{
    observability.MetricAccessDenied,
    observability.MetricRateLimitRejected,
    observability.MetricBotCheckRejected,
    observability.MetricAuthTokenIssued,
    observability.MetricAuthRefreshReuse,
    observability.MetricAuthRefreshRotated,
    observability.MetricAuthLoginRefused,
    observability.MetricDatabaseTransaction,
    observability.MetricConfirmTransaction,
    observability.MetricConfirmDurationSeconds,
    observability.MetricPaymentAttempt,
    observability.MetricQueueJobFailed,
    observability.MetricRefundPendingBookings,
    observability.MetricRequestDurationSeconds,
    observability.MetricNotModified,
    observability.MetricPanicRecovered,
    observability.MetricHoldGranted,
    observability.MetricBookingConfirmed,
    observability.MetricRaceLost,
    observability.MetricDuplicateRejected,
    observability.MetricHoldExpired,
    observability.MetricQueueDepth,
    observability.MetricQueueDepthUnread,
    observability.MetricQueueJobDuration,
    observability.MetricWorkerJobsClaimed,
    observability.MetricWorkerJobsCompleted,
    observability.MetricCacheLookup,
    observability.MetricDatabasePoolConnections,
    observability.MetricReplicationLagBytes,
    observability.MetricFaultInjectionEnabled,
    observability.MetricFaultInjected,
    observability.MetricFrontendPageLoadSeconds,
    observability.MetricFrontendApiError,
    observability.MetricFrontendBookingFunnel,
    observability.MetricFrontendCacheLookup,
}

// drivenRegistry builds a registry with every metric touched at least once.
//
// A metric with no observations and no pre-created series is absent from the
// exposition entirely, so a name can only be checked against a registry that has
// had something happen on it. Driving each one here rather than listing the
// names is what makes the check mean "the code publishes this" rather than "the
// constant is spelt the same way twice".
func drivenRegistry(t *testing.T) *prometheus.Registry {
    t.Helper()

    registry := prometheus.NewRegistry()
    metrics := observability.NewMetrics(registry)

    metrics.Access.AccessDenied(observability.ReasonTokenExpired)
    metrics.Access.RateLimitRejected(observability.ScopeSubject)
    metrics.Access.BotCheckRejected(observability.CheckHoneypot)
    metrics.Access.TokenIssued(observability.TokenKindAccess)
    metrics.Access.RefreshReuseDetected()
    metrics.Access.RefreshRotated()
    metrics.Access.LoginRefused()

    metrics.Transaction.DatabaseTransaction("confirm_seat", observability.OutcomeCommit)
    metrics.Transaction.ConfirmTransaction(observability.OutcomeConfirmed, 0.01)
    metrics.Transaction.PaymentAttempt(observability.OutcomeSettled)
    metrics.Transaction.QueueJobFailed("expire_hold")
    metrics.Transaction.RefundPending(0)

    metrics.Application.RequestObserved("GET /api/v1/classes", http.MethodGet, http.StatusOK, 0.01)
    metrics.Application.NotModified("GET /api/v1/classes")
    metrics.Application.PanicRecovered()
    metrics.Application.HoldGranted(observability.OutcomeGranted)
    metrics.Application.BookingConfirmed()
    metrics.Application.RaceLost()
    metrics.Application.DuplicateRejected()
    metrics.Application.HoldExpired()
    metrics.Application.QueueDepthUnknown()
    metrics.Application.QueueDepth(0, 0, 0)
    metrics.Application.QueueJob("expire_hold", observability.OutcomeCommit, 0.02)
    metrics.Application.JobsClaimed(1)
    metrics.Application.JobsCompleted()
    metrics.Application.CacheLookup("classes", observability.ResultHit)
    metrics.Application.PoolConnections(observability.PoolPrimary, 1, 2, 3)
    metrics.Application.ReplicationLag(0)
    metrics.Application.FaultInjectionEnabled(false)
    metrics.Application.FaultInjected("confirm.before_commit")

    metrics.Frontend.PageLoad("/", 0.4)
    metrics.Frontend.ApiError("class_full")
    metrics.Frontend.FunnelStep(observability.StepList)
    metrics.Frontend.ClientCacheLookup(observability.ClientResultFresh)

    return registry
}

func TestMetrics(t *testing.T) {
    t.Run("integration: every named metric is on the exposition", func(t *testing.T) {
        published := exposition(t, drivenRegistry(t))

        for _, name := range everyMetricName {
            if !strings.Contains(published, "# TYPE "+name+" ") {
                t.Errorf("the exposition does not declare %s, so every panel and alert naming it reads empty", name)
            }
        }
    })

    t.Run("edge: no label value on the exposition holds an identifier", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        // Every method whose label value could reach it from outside the process
        // is driven with an identifier here. The api's own labels, the route
        // pattern, the transaction name, the job kind, the fault point, are
        // constants written into the code and are not part of this case: they
        // cannot carry an identifier without somebody typing one into a source
        // file.
        bookingID := "0192a000-0000-7000-8000-0000000000aa"

        metrics.Access.AccessDenied(bookingID)
        metrics.Access.RateLimitRejected(bookingID)
        metrics.Access.BotCheckRejected(bookingID)
        metrics.Access.TokenIssued(bookingID)
        metrics.Transaction.ConfirmTransaction(bookingID, 0.01)
        metrics.Transaction.PaymentAttempt(bookingID)
        metrics.Application.HoldGranted(bookingID)
        metrics.Frontend.ApiError(bookingID)
        metrics.Frontend.FunnelStep(bookingID)
        metrics.Frontend.ClientCacheLookup(bookingID)

        published := exposition(t, registry)

        if found := uuidPattern.FindString(published); found != "" {
            t.Fatalf("the exposition carries the identifier %q, which is one time series per booking and a leak of who booked what", found)
        }
    })

    t.Run("unit: a counter exists at zero before anything happens", func(t *testing.T) {
        // A counter that has never been incremented is missing from the format,
        // and a missing series makes rate() return nothing rather than zero. A
        // panel would read empty on a healthy service, and an alert could not
        // tell "no denials" from "the metric was renamed".
        registry := prometheus.NewRegistry()

        observability.NewMetrics(registry)

        published := exposition(t, registry)

        wanted := observability.MetricAccessDenied + `{reason="token_expired"} 0`

        if !strings.Contains(published, wanted) {
            t.Fatalf("the exposition is missing %q, so a healthy service and a renamed metric look the same", wanted)
        }
    })

    t.Run("unit: an unknown label value is folded into a known one", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Transaction.PaymentAttempt("something nobody wrote down")

        published := exposition(t, registry)

        if !strings.Contains(published, observability.MetricPaymentAttempt+`{outcome="error"} 1`) {
            t.Fatal("an unrecognised payment outcome should count as an error rather than opening a series of its own")
        }
    })

    t.Run("behaviour: a lost race and a broken transaction are different numbers", func(t *testing.T) {
        // This is the distinction the whole transaction group exists for. A
        // confirm that rolls back because somebody else took the seat is correct
        // behaviour, and one that rolls back because the transaction broke is
        // not. An alert cannot tell them apart if they share a series.
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Transaction.ConfirmTransaction(observability.OutcomeSeatLost, 0.004)
        metrics.Transaction.ConfirmTransaction(observability.OutcomeSeatLost, 0.004)
        metrics.Transaction.ConfirmTransaction(observability.OutcomeError, 0.004)

        published := exposition(t, registry)

        if !strings.Contains(published, observability.MetricConfirmTransaction+`{outcome="seat_lost"} 2`) {
            t.Error("two lost races should be counted as lost races")
        }

        if !strings.Contains(published, observability.MetricConfirmTransaction+`{outcome="error"} 1`) {
            t.Error("one broken transaction should be counted on its own")
        }
    })

    t.Run("unit: every recording method is safe on a nil group", func(t *testing.T) {
        // Nothing here is wired in a test that does not want metrics, so a nil
        // group has to be a no-op rather than a panic. The alternative is a nil
        // check at every call site, which is right in nine places and missing in
        // the tenth.
        var access *observability.AccessMetrics
        var transaction *observability.TransactionMetrics
        var application *observability.ApplicationMetrics
        var frontend *observability.FrontendMetrics

        access.AccessDenied(observability.ReasonTokenInvalid)
        access.RateLimitRejected(observability.ScopeSubject)
        access.BotCheckRejected(observability.CheckHoneypot)
        access.TokenIssued(observability.TokenKindAccess)
        access.RefreshReuseDetected()
        access.RefreshRotated()
        access.LoginRefused()

        transaction.DatabaseTransaction("confirm_seat", observability.OutcomeCommit)
        transaction.ConfirmTransaction(observability.OutcomeConfirmed, 0.01)
        transaction.PaymentAttempt(observability.OutcomeSettled)
        transaction.QueueJobFailed("expire_hold")
        transaction.RefundPending(2)

        application.RequestObserved("GET /", http.MethodGet, http.StatusOK, 0.01)
        application.NotModified("GET /")
        application.PanicRecovered()
        application.HoldGranted(observability.OutcomeGranted)
        application.BookingConfirmed()
        application.RaceLost()
        application.DuplicateRejected()
        application.HoldExpired()
        application.QueueDepth(1, 2, 3)
        application.QueueJob("expire_hold", observability.OutcomeCommit, 0.1)
        application.JobsClaimed(2)
        application.JobsCompleted()
        application.CacheLookup("classes", observability.ResultHit)
        application.PoolConnections(observability.PoolPrimary, 1, 2, 3)
        application.ReplicationLag(0)
        application.FaultInjectionEnabled(false)
        application.FaultInjected("confirm.before_commit")

        frontend.PageLoad("/", 0.2)
        frontend.ApiError("class_full")
        frontend.FunnelStep(observability.StepList)
        frontend.ClientCacheLookup(observability.ClientResultFresh)
    })
}

func TestRegistry(t *testing.T) {
    t.Run("integration: the process and runtime collectors are on the registry", func(t *testing.T) {
        // Layer one of the monitoring plan is cpu, memory, and file descriptors
        // per process, and it comes from these two collectors rather than from
        // anything written here. If they are ever dropped, the resource row of
        // the dashboard loses its api and worker panels silently.
        published := exposition(t, observability.NewRegistry())

        for _, name := range []string{
            "process_cpu_seconds_total",
            "process_resident_memory_bytes",
            "process_open_fds",
            "go_goroutines",
            "go_gc_duration_seconds",
        } {
            if !strings.Contains(published, "# TYPE "+name+" ") {
                t.Errorf("the registry does not publish %s, which the resource row draws from", name)
            }
        }
    })

    t.Run("unit: two registries do not share numbers", func(t *testing.T) {
        // This is why the registry is built rather than taken from the library's
        // global default. Two cases in one test run must not see each other's
        // counts, and a global registry cannot be built twice.
        first := prometheus.NewRegistry()
        second := prometheus.NewRegistry()

        observability.NewMetrics(first).Application.RaceLost()
        observability.NewMetrics(second)

        if !strings.Contains(exposition(t, first), observability.MetricRaceLost+" 1") {
            t.Fatal("the first registry lost the count it was given")
        }

        if !strings.Contains(exposition(t, second), observability.MetricRaceLost+" 0") {
            t.Fatal("the second registry saw a count that belongs to the first")
        }
    })
}
