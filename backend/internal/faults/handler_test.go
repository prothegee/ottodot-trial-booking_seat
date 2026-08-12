package faults_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/faults"
)

// armedSurface builds the routes over a fresh registry.
func armedSurface(t *testing.T) (*http.ServeMux, *faults.Registry) {
    t.Helper()

    registry := faults.NewRegistry(faults.Settings{})
    mux := http.NewServeMux()

    faults.NewHandler(registry).Register(mux, nil)

    return mux, registry
}

// call drives one request through the mux.
func call(mux *http.ServeMux, method string, body string) *httptest.ResponseRecorder {
    request := httptest.NewRequest(method, faults.RoutePrefix, strings.NewReader(body))
    recorder := httptest.NewRecorder()

    mux.ServeHTTP(recorder, request)

    return recorder
}

func TestHandler(t *testing.T) {
    t.Run("integration: arming, listing, and disarming over http", func(t *testing.T) {
        mux, registry := armedSurface(t)

        armed := call(mux, http.MethodPost, `{"point":"confirm.before_commit","count":2,"ttl_seconds":30}`)
        if armed.Code != http.StatusOK {
            t.Fatalf("arming answered %d: %s", armed.Code, armed.Body.String())
        }

        var reported faults.Armed

        if err := json.NewDecoder(armed.Body).Decode(&reported); err != nil {
            t.Fatalf("the arm answer is not readable: %v", err)
        }

        if reported.Point != faults.PointConfirmBeforeCommit || reported.Remaining != 2 {
            t.Fatalf("the answer reports %s with %d left", reported.Point, reported.Remaining)
        }

        if reported.ExpiresAt.IsZero() {
            t.Fatal("the answer carries no deadline, so nobody can tell when it stops")
        }

        listed := call(mux, http.MethodGet, "")
        if !strings.Contains(listed.Body.String(), faults.PointConfirmBeforeCommit) {
            t.Fatalf("the list does not name what was armed: %s", listed.Body.String())
        }

        if code := call(mux, http.MethodDelete, "").Code; code != http.StatusOK {
            t.Fatalf("disarming answered %d", code)
        }

        if registry.Trigger(faults.PointConfirmBeforeCommit) {
            t.Fatal("the point still fired after being disarmed over http")
        }
    })

    t.Run("edge: a misspelt point comes back with the real list", func(t *testing.T) {
        // A typo should be fixable from the answer rather than from the source,
        // because the person hitting this route is usually mid demonstration.
        mux, _ := armedSurface(t)

        refused := call(mux, http.MethodPost, `{"point":"confirm.before_comit"}`)

        if refused.Code != http.StatusBadRequest {
            t.Fatalf("a misspelt point answered %d", refused.Code)
        }

        for _, point := range faults.Points() {
            if !strings.Contains(refused.Body.String(), point) {
                t.Errorf("the refusal does not name %s", point)
            }
        }
    })

    t.Run("edge: an unreadable body is refused rather than treated as an empty arm", func(t *testing.T) {
        mux, registry := armedSurface(t)

        refused := call(mux, http.MethodPost, `{"point":`)

        if refused.Code != http.StatusBadRequest {
            t.Fatalf("a broken body answered %d", refused.Code)
        }

        if len(registry.List()) != 0 {
            t.Fatal("a broken body armed something")
        }
    })

    t.Run("edge: a count over the cap is refused", func(t *testing.T) {
        mux, _ := armedSurface(t)

        refused := call(mux, http.MethodPost, `{"point":"queue.job_error","count":99}`)

        if refused.Code != http.StatusBadRequest {
            t.Fatalf("a count of ninety nine answered %d", refused.Code)
        }
    })

    t.Run("unit: disarming with nothing armed still answers ok", func(t *testing.T) {
        // A recovery step that could fail because there was nothing to recover
        // from would be worse than useless.
        mux, _ := armedSurface(t)

        cleared := call(mux, http.MethodDelete, "")

        if cleared.Code != http.StatusOK {
            t.Fatalf("disarming an empty registry answered %d", cleared.Code)
        }
    })

    t.Run("unit: nothing on this surface may be cached", func(t *testing.T) {
        mux, _ := armedSurface(t)

        listed := call(mux, http.MethodGet, "")

        if listed.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("the list answered with Cache-Control %q", listed.Header().Get("Cache-Control"))
        }
    })

    t.Run("behaviour: the routes only exist when they were registered", func(t *testing.T) {
        // When the surface is off it is never put on the mux, so an
        // unauthorised caller gets a 404 rather than a 403 that would confirm
        // there is something here to find.
        bare := http.NewServeMux()

        recorder := httptest.NewRecorder()
        bare.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, faults.RoutePrefix, nil))

        if recorder.Code != http.StatusNotFound {
            t.Fatalf("an unregistered fault surface answered %d rather than a plain not found", recorder.Code)
        }
    })

    t.Run("integration: the guard chain wraps every route", func(t *testing.T) {
        // The role check and the rate limit are the caller's to apply, and this
        // proves all three routes go through whatever was handed in rather than
        // only the two that mutate.
        registry := faults.NewRegistry(faults.Settings{})
        mux := http.NewServeMux()

        wrapped := 0

        faults.NewHandler(registry).Register(mux, func(next http.Handler) http.Handler {
            return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
                wrapped++

                next.ServeHTTP(response, request)
            })
        })

        call(mux, http.MethodPost, `{"point":"queue.job_error"}`)
        call(mux, http.MethodGet, "")
        call(mux, http.MethodDelete, "")

        if wrapped != 3 {
            t.Fatalf("%d of the three routes went through the guard chain", wrapped)
        }
    })
}
