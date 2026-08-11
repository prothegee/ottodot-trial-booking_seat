package operations_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/operations"
)

func TestVersion(t *testing.T) {
    t.Run("integration: a stamped build reports what it was built from", func(t *testing.T) {
        identity := operations.NewIdentity("0.1.0", "6b30337", "2026-08-10T14:00:00Z")

        recorder := httptest.NewRecorder()
        identity.Handle(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))

        if recorder.Code != http.StatusOK {
            t.Fatalf("version answered %d", recorder.Code)
        }

        var reported operations.Identity

        if err := json.NewDecoder(recorder.Body).Decode(&reported); err != nil {
            t.Fatalf("the body is not readable: %v", err)
        }

        if reported.Version != "0.1.0" || reported.Commit != "6b30337" {
            t.Fatalf("the body reports version %q commit %q", reported.Version, reported.Commit)
        }

        if reported.Service != operations.ServiceName {
            t.Fatalf("the body names the service %q", reported.Service)
        }
    })

    t.Run("unit: the runtime comes from the runtime, so it cannot disagree with what is executing", func(t *testing.T) {
        identity := operations.NewIdentity("0.1.0", "6b30337", "2026-08-10T14:00:00Z")

        if !strings.HasPrefix(identity.Runtime, "go") {
            t.Fatalf("the runtime reports %q", identity.Runtime)
        }
    })

    t.Run("edge: an unstamped build reports unknown rather than an empty field", func(t *testing.T) {
        identity := operations.NewIdentity("", "", "")

        if identity.Version != "unknown" || identity.Commit != "unknown" || identity.BuiltAt != "unknown" {
            t.Fatalf("an unstamped build reports %+v", identity)
        }
    })

    t.Run("edge: the body carries no environment value and no connection detail", func(t *testing.T) {
        identity := operations.NewIdentity("0.1.0", "6b30337", "2026-08-10T14:00:00Z")

        recorder := httptest.NewRecorder()
        identity.Handle(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))

        body := recorder.Body.String()

        for _, leak := range []string{"postgres", "redis", "password", "secret", "127.0.0.1", "JWT"} {
            if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
                t.Fatalf("the version body carries %q: %s", leak, body)
            }
        }
    })

    t.Run("edge: version is never cached, or a deploy would be invisible", func(t *testing.T) {
        identity := operations.NewIdentity("0.1.0", "6b30337", "2026-08-10T14:00:00Z")

        recorder := httptest.NewRecorder()
        identity.Handle(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("version answered with Cache-Control %q", recorder.Header().Get("Cache-Control"))
        }
    })
}

func TestRegisteringTheOperationsRoutes(t *testing.T) {
    t.Run("integration: all three routes answer without a token", func(t *testing.T) {
        readiness, err := operations.NewReadiness([]operations.Dependency{alwaysUp("postgres_primary", true)})
        if err != nil {
            t.Fatalf("cannot build readiness: %v", err)
        }

        handler, err := operations.NewHandler(readiness, operations.NewIdentity("0.1.0", "6b30337", ""))
        if err != nil {
            t.Fatalf("cannot build the handler: %v", err)
        }

        routes := http.NewServeMux()
        handler.Register(routes)

        for _, path := range []string{"/healthz", "/readyz", "/version"} {
            recorder := httptest.NewRecorder()
            routes.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

            if recorder.Code != http.StatusOK {
                t.Fatalf("%s answered %d with no token", path, recorder.Code)
            }
        }
    })

    t.Run("edge: a handler with no readiness source is refused", func(t *testing.T) {
        if _, err := operations.NewHandler(nil, operations.NewIdentity("", "", "")); err == nil {
            t.Fatal("a handler with nothing to check was built")
        }
    })
}
