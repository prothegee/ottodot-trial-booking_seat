package operations_test

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "testing"

    "ottodot-trial-booking/backend/internal/operations"
)

// probeFailure is what a downed dependency reports. It is one value, so a case
// can prove the body never repeats it.
var probeFailure = errors.New("dial tcp 10.1.2.3:5432: connection refused")

// alwaysUp is a dependency that answers.
func alwaysUp(name string, required bool) operations.Dependency {
    return operations.Dependency{
        Name:     name,
        Required: required,
        Probe:    func(context.Context) error { return nil },
    }
}

// alwaysDown is a dependency that does not.
func alwaysDown(name string, required bool) operations.Dependency {
    return operations.Dependency{
        Name:     name,
        Required: required,
        Probe:    func(context.Context) error { return probeFailure },
    }
}

func TestLiveness(t *testing.T) {
    t.Run("unit: a running process answers 200", func(t *testing.T) {
        recorder := httptest.NewRecorder()

        operations.Liveness(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

        if recorder.Code != http.StatusOK {
            t.Fatalf("liveness answered %d, wanted 200", recorder.Code)
        }
    })

    t.Run("edge: liveness is never cached, or a probe would read one old answer forever", func(t *testing.T) {
        recorder := httptest.NewRecorder()

        operations.Liveness(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("liveness answered with Cache-Control %q", recorder.Header().Get("Cache-Control"))
        }
    })

    t.Run("edge: liveness touches no dependency, so a downed database cannot kill the process", func(t *testing.T) {
        touched := false

        readiness, err := operations.NewReadiness([]operations.Dependency{{
            Name:     "postgres_primary",
            Required: true,
            Probe: func(context.Context) error {
                touched = true

                return nil
            },
        }})
        if err != nil {
            t.Fatalf("cannot build readiness: %v", err)
        }

        handler, err := operations.NewHandler(readiness, operations.NewIdentity("dev", "unknown", ""))
        if err != nil {
            t.Fatalf("cannot build the handler: %v", err)
        }

        routes := http.NewServeMux()
        handler.Register(routes)

        recorder := httptest.NewRecorder()
        routes.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

        if touched {
            t.Fatal("liveness reached a dependency, so one slow database would restart every instance at once")
        }
    })
}
