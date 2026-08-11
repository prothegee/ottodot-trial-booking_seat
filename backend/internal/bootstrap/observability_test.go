package bootstrap_test

import (
    "bytes"
    "log/slog"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/observability"
)

func TestObservability(t *testing.T) {
    t.Run("integration: the metrics and the exposition share one registry", func(t *testing.T) {
        // This is the whole reason the three are built together. Two registries
        // in one process would mean a scrape that returns half the numbers, and
        // which half would depend on which handler happened to be wired to the
        // route.
        var written bytes.Buffer

        watch := bootstrap.NewObservability(&written, slog.LevelInfo)

        watch.Metrics.Application.RaceLost()

        recorder := httptest.NewRecorder()
        watch.Exposition.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

        if !strings.Contains(recorder.Body.String(), observability.MetricRaceLost+" 1") {
            t.Fatal("a count recorded through the metrics did not reach the exposition")
        }
    })

    t.Run("integration: the logger writes through the redaction rules", func(t *testing.T) {
        var written bytes.Buffer

        bootstrap.NewObservability(&written, slog.LevelInfo).Logger.
            Info("sign in refused", slog.String("cookie", "session=abcdef123456"))

        if strings.Contains(written.String(), "abcdef123456") {
            t.Fatalf("a secret reached the output: %s", written.String())
        }
    })

    t.Run("unit: the process collectors are on the registry", func(t *testing.T) {
        // Layer one of the monitoring plan is cpu and memory per process, and it
        // comes from the collectors the registry is built with rather than from
        // anything this project wrote.
        var written bytes.Buffer

        recorder := httptest.NewRecorder()
        bootstrap.NewObservability(&written, slog.LevelInfo).Exposition.
            ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

        if !strings.Contains(recorder.Body.String(), "process_resident_memory_bytes") {
            t.Fatal("the registry does not publish this process's memory, which the resource row draws from")
        }
    })

    t.Run("unit: two processes do not share numbers", func(t *testing.T) {
        var first, second bytes.Buffer

        firstWatch := bootstrap.NewObservability(&first, slog.LevelInfo)
        secondWatch := bootstrap.NewObservability(&second, slog.LevelInfo)

        firstWatch.Metrics.Application.RaceLost()

        recorder := httptest.NewRecorder()
        secondWatch.Exposition.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

        if !strings.Contains(recorder.Body.String(), observability.MetricRaceLost+" 0") {
            t.Fatal("the second registry saw a count that belongs to the first")
        }
    })
}
