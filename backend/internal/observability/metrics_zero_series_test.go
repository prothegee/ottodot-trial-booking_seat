// What a panel reads before anything has happened.
//
// A counter carrying a label publishes nothing until that label value is used
// once, so a service nobody has called yet publishes no failed job, no fault, no
// transaction, and no route, and every panel bound to one of those reads No data.
// A stack whose worker died reads No data as well. The two must not look alike,
// and the way to tell them apart is to create the series at zero on start.
//
// This file pins that. It is a separate file from metrics_test.go because that
// one asks whether a metric exists after it has been driven, and this one asks
// what exists before anything drives it at all.
package observability_test

import (
    "fmt"
    "path/filepath"
    "regexp"
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/observability"
)

// everyApiErrorCode is the closed set the client is allowed to report.
//
// Written out rather than read back from the package, for the reason the
// catalogue in metrics_test.go gives: a list derived from the same value under
// test passes even when that value is empty.
var everyApiErrorCode = []string{
    "invalid_request",
    "token_expired",
    "token_invalid",
    "token_reused",
    "not_your_child",
    "forbidden_role",
    "payment_declined",
    "already_booked",
    "too_many_holds",
    "class_full",
    "seat_lost",
    "rate_limited",
    "internal_error",
    "dependency_unavailable",
}

// everyClientRoute is the closed set of route patterns the client may time,
// written out here for the same reason the code list above is.
var everyClientRoute = []string{
    "/",
    "/sign-in",
    "/book/[classId]",
    "/pay/[bookingId]",
    "/booking/[bookingId]",
    "/bookings",
    "/roster/[classId]",
    "/status",
}

// codeSelectorPattern pulls the code list out of a panel expression, so the
// alternatives inside `code=~"a|b|c"` can be checked one at a time.
var codeSelectorPattern = regexp.MustCompile(`code=~"([^"]+)"`)

func TestTheSeriesAPanelNeedsExistBeforeAnythingHappens(t *testing.T) {
    t.Run("behaviour: every api error code is published at zero on a fresh registry", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        observability.NewMetrics(registry)

        published := exposition(t, registry)

        for _, code := range everyApiErrorCode {
            wanted := fmt.Sprintf("frontend_api_error_total{code=%q} 0", code)

            if !strings.Contains(published, wanted) {
                t.Errorf("nothing publishes %s, so the panel selecting that code reads No data", wanted)
            }
        }
    })

    t.Run("integration: every client route has a page load count before anybody visits", func(t *testing.T) {
        // The panel draws an average beside its two percentiles, and an average
        // needs something to divide by. A percentile alone is not a number until
        // two page loads land in the window.
        registry := prometheus.NewRegistry()

        observability.NewMetrics(registry)

        published := exposition(t, registry)

        for _, route := range everyClientRoute {
            wanted := fmt.Sprintf("frontend_page_load_seconds_count{route=%q} 0", route)

            if !strings.Contains(published, wanted) {
                t.Errorf("nothing publishes %s, so the page load panel has nothing to average", wanted)
            }
        }
    })

    t.Run("behaviour: declaring the job kinds publishes each of them at zero", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Transaction.DeclareJobKinds([]string{"expire_hold", "reconcile_refund"})

        published := exposition(t, registry)

        for _, kind := range []string{"expire_hold", "reconcile_refund"} {
            wanted := fmt.Sprintf("queue_job_failed_total{kind=%q} 0", kind)

            if !strings.Contains(published, wanted) {
                t.Errorf("nothing publishes %s, so the failed jobs panel reads No data on a healthy stack", wanted)
            }
        }
    })

    t.Run("behaviour: declaring the fault points publishes each of them at zero", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Application.DeclareFaultPoints([]string{"confirm.before_commit", "cache.redis_error"})

        published := exposition(t, registry)

        for _, point := range []string{"confirm.before_commit", "cache.redis_error"} {
            wanted := fmt.Sprintf("fault_injected_total{point=%q} 0", point)

            if !strings.Contains(published, wanted) {
                t.Errorf("nothing publishes %s, so the triggered panel reads No data until one fires", wanted)
            }
        }
    })

    t.Run("behaviour: declaring the transaction names publishes every outcome at zero", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Transaction.DeclareTransactionNames(booking.TransactionNames())

        published := exposition(t, registry)

        for _, name := range booking.TransactionNames() {
            for _, outcome := range []string{observability.OutcomeCommit, observability.OutcomeRollback, observability.OutcomeConflict} {
                wanted := fmt.Sprintf("db_transaction_total{name=%q,outcome=%q} 0", name, outcome)

                if !strings.Contains(published, wanted) {
                    t.Errorf("nothing publishes %s, so the transactions panel reads No data until the first booking", wanted)
                }
            }
        }
    })

    t.Run("behaviour: declaring a route publishes its timing series at zero", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Application.DeclareRoute("GET /api/v1/classes", "GET")

        published := exposition(t, registry)

        wanted := `http_request_duration_seconds_count{method="GET",route="GET /api/v1/classes",status="200"} 0`

        if !strings.Contains(published, wanted) {
            t.Errorf("nothing publishes %s, so the request panels read No data until the first call", wanted)
        }
    })

    t.Run("edge: declaring a series that already has a count leaves the count alone", func(t *testing.T) {
        // The order the two calls happen in is not fixed. Declaring must create
        // what is missing and touch nothing else, or a restart of the wiring
        // would quietly reset a counter an alert is reading.
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        metrics.Transaction.QueueJobFailed("expire_hold")
        metrics.Application.FaultInjected("confirm.before_commit")
        metrics.Transaction.DatabaseTransaction("hold_seat", observability.OutcomeCommit)

        metrics.Transaction.DeclareJobKinds([]string{"expire_hold"})
        metrics.Application.DeclareFaultPoints([]string{"confirm.before_commit"})
        metrics.Transaction.DeclareTransactionNames(booking.TransactionNames())

        published := exposition(t, registry)

        if !strings.Contains(published, `queue_job_failed_total{kind="expire_hold"} 1`) {
            t.Error("declaring the kinds reset a failure that had already been counted")
        }

        if !strings.Contains(published, `db_transaction_total{name="hold_seat",outcome="commit"} 1`) {
            t.Error("declaring the names reset a transaction that had already been counted")
        }

        if !strings.Contains(published, `fault_injected_total{point="confirm.before_commit"} 1`) {
            t.Error("declaring the points reset a trigger that had already been counted")
        }
    })

    t.Run("edge: declaring on metrics that were never built does nothing rather than panicking", func(t *testing.T) {
        var transaction *observability.TransactionMetrics
        var application *observability.ApplicationMetrics

        transaction.DeclareJobKinds([]string{"expire_hold"})
        transaction.DeclareTransactionNames(booking.TransactionNames())
        application.DeclareFaultPoints([]string{"confirm.before_commit"})
        application.DeclareRoute("GET /api/v1/classes", "GET")
    })

    t.Run("integration: every code a frontend panel selects is one the api publishes", func(t *testing.T) {
        // The failure this catches is a panel that can never draw: a code
        // spelt one way in the dashboard and another way in the code has no
        // series behind it, and the panel is blank in exactly the way a working
        // one is on a quiet afternoon.
        queries := dashboardQueries(t, filepath.Join(dashboardDirectory, "frontend.json"))

        selected := 0

        for _, query := range queries {
            for _, match := range codeSelectorPattern.FindAllStringSubmatch(query, -1) {
                for _, code := range strings.Split(match[1], "|") {
                    selected++

                    if !observability.KnownCode(code) {
                        t.Errorf("a panel selects code %q, which this service never publishes", code)
                    }
                }
            }
        }

        if selected == 0 {
            t.Fatal("no panel selects on a code, so this test is reading the wrong dashboard")
        }
    })
}
