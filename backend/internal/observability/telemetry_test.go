package observability_test

import (
    "errors"
    "strings"
    "testing"

    "github.com/prometheus/client_golang/prometheus"

    "ottodot-trial-booking/backend/internal/observability"
)

func TestTelemetry(t *testing.T) {
    t.Run("integration: a batch of client events becomes metrics", func(t *testing.T) {
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        tally, err := observability.NewTelemetry(metrics.Frontend).Record(observability.Batch{
            Events: []observability.Event{
                {Kind: observability.EventPageLoad, Route: "/", Seconds: 0.42},
                {Kind: observability.EventFunnelStep, Step: observability.StepList},
                {Kind: observability.EventFunnelStep, Step: observability.StepHold},
                {Kind: observability.EventApiError, Code: "class_full"},
                {Kind: observability.EventCacheLookup, Result: observability.ClientResultFresh},
            },
        })
        if err != nil {
            t.Fatalf("a well formed batch was refused: %v", err)
        }

        if tally.Accepted != 5 || tally.Dropped != 0 {
            t.Fatalf("the tally reads %d accepted and %d dropped", tally.Accepted, tally.Dropped)
        }

        published := exposition(t, registry)

        for _, series := range []string{
            observability.MetricFrontendBookingFunnel + `{step="list"} 1`,
            observability.MetricFrontendApiError + `{code="class_full"} 1`,
            observability.MetricFrontendCacheLookup + `{result="fresh"} 1`,
        } {
            if !strings.Contains(published, series) {
                t.Errorf("the exposition is missing %q", series)
            }
        }
    })

    t.Run("edge: an unknown route is dropped rather than given a series", func(t *testing.T) {
        // The client is somebody else's computer. A route this service does not
        // recognise is either a page that has not been reloaded or somebody
        // writing into the monitoring system, and neither earns a time series.
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        tally, err := observability.NewTelemetry(metrics.Frontend).Record(observability.Batch{
            Events: []observability.Event{
                {Kind: observability.EventPageLoad, Route: "/booking/0192a000-0000-7000-8000-000000000031", Seconds: 0.3},
            },
        })
        if err != nil {
            t.Fatalf("the batch was refused whole: %v", err)
        }

        if tally.Accepted != 0 || tally.Dropped != 1 {
            t.Fatalf("the tally reads %d accepted and %d dropped", tally.Accepted, tally.Dropped)
        }

        if strings.Contains(exposition(t, registry), "0192a000") {
            t.Fatal("a booking identifier reached the exposition through the telemetry endpoint")
        }
    })

    t.Run("behaviour: one unrecognised event does not throw away the good ones beside it", func(t *testing.T) {
        // A client that has not been reloaded can send one stale field. Refusing
        // the whole batch for it would lose nine good events and would make every
        // deployment blind for as long as an old tab stayed open.
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        tally, err := observability.NewTelemetry(metrics.Frontend).Record(observability.Batch{
            Events: []observability.Event{
                {Kind: "something_nobody_wrote_down"},
                {Kind: observability.EventFunnelStep, Step: observability.StepConfirmed},
            },
        })
        if err != nil {
            t.Fatalf("the batch was refused whole: %v", err)
        }

        if tally.Accepted != 1 || tally.Dropped != 1 {
            t.Fatalf("the tally reads %d accepted and %d dropped", tally.Accepted, tally.Dropped)
        }
    })

    t.Run("edge: an empty batch and an oversized batch are both refused", func(t *testing.T) {
        telemetry := observability.NewTelemetry(observability.NewMetrics(prometheus.NewRegistry()).Frontend)

        if _, err := telemetry.Record(observability.Batch{}); !errors.Is(err, observability.ErrTelemetryRefused) {
            t.Errorf("an empty batch answered %v", err)
        }

        oversized := make([]observability.Event, observability.MaxBatchEvents+1)
        for index := range oversized {
            oversized[index] = observability.Event{Kind: observability.EventFunnelStep, Step: observability.StepList}
        }

        if _, err := telemetry.Record(observability.Batch{Events: oversized}); !errors.Is(err, observability.ErrTelemetryRefused) {
            t.Errorf("an oversized batch answered %v", err)
        }
    })

    t.Run("edge: an absurd page load time is dropped", func(t *testing.T) {
        // The number arrives from a browser, so it is a claim. One bogus sample
        // drags every quantile with it and the panel stops describing anybody.
        registry := prometheus.NewRegistry()
        metrics := observability.NewMetrics(registry)

        tally, _ := observability.NewTelemetry(metrics.Frontend).Record(observability.Batch{
            Events: []observability.Event{
                {Kind: observability.EventPageLoad, Route: "/", Seconds: observability.MaxPageLoadSeconds + 1},
                {Kind: observability.EventPageLoad, Route: "/", Seconds: -4},
            },
        })

        if tally.Dropped != 2 {
            t.Fatalf("%d of the two impossible durations were dropped", tally.Dropped)
        }
    })

    t.Run("unit: recording into a nil metric group is harmless", func(t *testing.T) {
        tally, err := observability.NewTelemetry(nil).Record(observability.Batch{
            Events: []observability.Event{{Kind: observability.EventFunnelStep, Step: observability.StepPay}},
        })
        if err != nil {
            t.Fatalf("the batch was refused: %v", err)
        }

        if tally.Accepted != 1 {
            t.Fatalf("the tally reads %d accepted", tally.Accepted)
        }
    })
}
