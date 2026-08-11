package operations_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/operations"
)

// readReport drives the readiness route and decodes what came back.
func readReport(t *testing.T, dependencies []operations.Dependency) (operations.Report, int) {
    t.Helper()

    readiness, err := operations.NewReadiness(dependencies)
    if err != nil {
        t.Fatalf("cannot build readiness: %v", err)
    }

    recorder := httptest.NewRecorder()
    readiness.Handle(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

    var report operations.Report

    if err := json.NewDecoder(recorder.Body).Decode(&report); err != nil {
        t.Fatalf("the body is not readable: %v", err)
    }

    return report, recorder.Code
}

func TestReadiness(t *testing.T) {
    t.Run("integration: everything up answers ready", func(t *testing.T) {
        report, status := readReport(t, []operations.Dependency{
            alwaysUp("postgres_primary", true),
            alwaysUp("postgres_replica", false),
            alwaysUp("redis", true),
        })

        if status != http.StatusOK {
            t.Fatalf("a healthy service answered %d", status)
        }

        if report.Status != operations.StatusReady {
            t.Fatalf("a healthy service reported %q", report.Status)
        }

        for name, state := range report.Checks {
            if state != "ok" {
                t.Fatalf("%s reported %q on a healthy service", name, state)
            }
        }
    })

    t.Run("behaviour: a downed replica is degraded and still takes traffic", func(t *testing.T) {
        report, status := readReport(t, []operations.Dependency{
            alwaysUp("postgres_primary", true),
            alwaysDown("postgres_replica", false),
            alwaysUp("redis", true),
        })

        if status != http.StatusOK {
            t.Fatalf("a downed replica answered %d, and every deciding read already uses the primary", status)
        }

        if report.Status != operations.StatusDegraded {
            t.Fatalf("a downed replica reported %q, wanted degraded", report.Status)
        }

        if report.Checks["postgres_replica"] != "down" {
            t.Fatalf("the replica reported %q while being unreachable", report.Checks["postgres_replica"])
        }
    })

    t.Run("behaviour: a downed primary is unavailable, because nothing can be decided", func(t *testing.T) {
        report, status := readReport(t, []operations.Dependency{
            alwaysDown("postgres_primary", true),
            alwaysUp("postgres_replica", false),
            alwaysUp("redis", true),
        })

        if status != http.StatusServiceUnavailable {
            t.Fatalf("a downed primary answered %d, wanted 503", status)
        }

        if report.Status != operations.StatusUnavailable {
            t.Fatalf("a downed primary reported %q", report.Status)
        }
    })

    t.Run("behaviour: a downed redis is unavailable, because the denylist cannot say no", func(t *testing.T) {
        _, status := readReport(t, []operations.Dependency{
            alwaysUp("postgres_primary", true),
            alwaysUp("postgres_replica", false),
            alwaysDown("redis", true),
        })

        if status != http.StatusServiceUnavailable {
            t.Fatalf("a downed redis answered %d, wanted 503", status)
        }
    })

    t.Run("behaviour: every probe runs, so a second outage is not hidden by the first", func(t *testing.T) {
        report, _ := readReport(t, []operations.Dependency{
            alwaysDown("postgres_primary", true),
            alwaysDown("postgres_replica", false),
            alwaysDown("redis", true),
        })

        if len(report.Checks) != 3 {
            t.Fatalf("%d checks were reported, wanted 3", len(report.Checks))
        }

        for name, state := range report.Checks {
            if state != "down" {
                t.Fatalf("%s reported %q while everything was unreachable", name, state)
            }
        }
    })

    t.Run("edge: a required failure outranks an advisory one", func(t *testing.T) {
        report, status := readReport(t, []operations.Dependency{
            alwaysDown("postgres_primary", true),
            alwaysDown("postgres_replica", false),
        })

        if report.Status != operations.StatusUnavailable || status != http.StatusServiceUnavailable {
            t.Fatalf("both down reported %q with status %d, wanted unavailable and 503", report.Status, status)
        }
    })

    t.Run("edge: nothing in the body names a host, a port, or a driver message", func(t *testing.T) {
        readiness, err := operations.NewReadiness([]operations.Dependency{
            alwaysDown("postgres_primary", true),
            alwaysDown("redis", true),
        })
        if err != nil {
            t.Fatalf("cannot build readiness: %v", err)
        }

        recorder := httptest.NewRecorder()
        readiness.Handle(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

        body := recorder.Body.String()

        for _, leak := range []string{"10.1.2.3", "5432", "connection refused", "dial", "tcp"} {
            if strings.Contains(body, leak) {
                t.Fatalf("the readiness body carries %q: %s", leak, body)
            }
        }
    })

    t.Run("edge: readiness is never cached, or a probe would read one old answer forever", func(t *testing.T) {
        readiness, err := operations.NewReadiness([]operations.Dependency{alwaysUp("postgres_primary", true)})
        if err != nil {
            t.Fatalf("cannot build readiness: %v", err)
        }

        recorder := httptest.NewRecorder()
        readiness.Handle(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("readiness answered with Cache-Control %q", recorder.Header().Get("Cache-Control"))
        }
    })
}

func TestBuildingReadiness(t *testing.T) {
    t.Run("edge: no dependency at all is refused, because it would answer ready forever", func(t *testing.T) {
        if _, err := operations.NewReadiness(nil); err == nil {
            t.Fatal("a readiness route with nothing to check was allowed")
        }
    })

    t.Run("edge: advisory checks alone are refused, for the same reason", func(t *testing.T) {
        if _, err := operations.NewReadiness([]operations.Dependency{alwaysUp("postgres_replica", false)}); err == nil {
            t.Fatal("a readiness route with nothing required was allowed")
        }
    })

    t.Run("edge: a dependency with no probe is refused rather than silently skipped", func(t *testing.T) {
        _, err := operations.NewReadiness([]operations.Dependency{{Name: "redis", Required: true}})

        if err == nil {
            t.Fatal("a dependency with no probe was accepted, and it would always report ok")
        }
    })

    t.Run("edge: a probe that never answers is reported down rather than left hanging", func(t *testing.T) {
        readiness, err := operations.NewReadiness([]operations.Dependency{{
            Name:     "postgres_primary",
            Required: true,
            Probe: func(ctx context.Context) error {
                <-ctx.Done()

                return ctx.Err()
            },
        }})
        if err != nil {
            t.Fatalf("cannot build readiness: %v", err)
        }

        // The caller's own deadline is what ends this, rather than the probe
        // budget, so the case proves the behaviour without spending two seconds
        // in the fast tier.
        ctx, cancel := context.WithCancel(context.Background())
        cancel()

        report, status := readiness.Check(ctx)

        if status != http.StatusServiceUnavailable || report.Checks["postgres_primary"] != "down" {
            t.Fatalf("a hanging probe answered %d with %q", status, report.Checks["postgres_primary"])
        }
    })
}
