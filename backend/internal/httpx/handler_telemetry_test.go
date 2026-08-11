package httpx_test

import (
    "net/http"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/observability"
)

func TestTheTelemetryRoute(t *testing.T) {
    t.Run("integration: a signed in parent's batch becomes metrics", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            parent: parentOne,
            body:   `{"events":[{"kind":"funnel_step","step":"hold"},{"kind":"api_error","code":"class_full"}]}`,
        })

        if recorded.Code != http.StatusNoContent {
            t.Fatalf("the batch answered %d: %s", recorded.Code, recorded.Body.String())
        }

        if recorded.Body.Len() != 0 {
            t.Fatalf("the answer carries a body: %s", recorded.Body.String())
        }

        published := fixture.exposition(t)

        for _, series := range []string{
            observability.MetricFrontendBookingFunnel + `{step="hold"} 1`,
            observability.MetricFrontendApiError + `{code="class_full"} 1`,
        } {
            if !strings.Contains(published, series) {
                t.Errorf("the exposition is missing %q", series)
            }
        }
    })

    t.Run("edge: an anonymous caller cannot post", func(t *testing.T) {
        // The endpoint is behind the same session every other route is. An open
        // one would be a way for anybody to write into the monitoring system,
        // and the label lists would be the only thing standing in the way.
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            body:   `{"events":[{"kind":"funnel_step","step":"list"}]}`,
        })

        if recorded.Code != http.StatusUnauthorized {
            t.Fatalf("an anonymous post answered %d", recorded.Code)
        }
    })

    t.Run("edge: a post from another origin is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            parent: parentOne,
            origin: "https://somewhere-else.example",
            body:   `{"events":[{"kind":"funnel_step","step":"list"}]}`,
        })

        if recorded.Code != http.StatusBadRequest {
            t.Fatalf("a cross origin post answered %d", recorded.Code)
        }
    })

    t.Run("edge: an empty batch is refused as a bad request", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            parent: parentOne,
            body:   `{"events":[]}`,
        })

        if recorded.Code != http.StatusBadRequest {
            t.Fatalf("an empty batch answered %d", recorded.Code)
        }
    })

    t.Run("edge: an unreadable body is refused rather than recorded as nothing", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            parent: parentOne,
            body:   `{"events":`,
        })

        if recorded.Code != http.StatusBadRequest {
            t.Fatalf("a broken body answered %d", recorded.Code)
        }
    })

    t.Run("behaviour: a route this service does not know produces no series", func(t *testing.T) {
        // A client route carrying a booking identifier would be one series per
        // booking. The list is closed for exactly that reason, and this proves
        // the closing is enforced on the way in rather than assumed.
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/telemetry",
            parent: parentOne,
            body:   `{"events":[{"kind":"page_load","route":"/booking/0192a000-0000-7000-8000-000000000031","seconds":0.4}]}`,
        })

        if recorded.Code != http.StatusNoContent {
            t.Fatalf("the batch answered %d", recorded.Code)
        }

        if strings.Contains(fixture.exposition(t), "0192a000") {
            t.Fatal("a booking identifier reached the exposition through the telemetry route")
        }
    })
}

func TestTheScrapeRoute(t *testing.T) {
    t.Run("integration: a served request appears on the exposition", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        fixture.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

        published := fixture.exposition(t)

        if !strings.Contains(published, observability.MetricRequestDurationSeconds) {
            t.Fatal("the request was not timed")
        }

        if !strings.Contains(published, `route="`+httpx.ClassListPath+`"`) {
            t.Fatalf("the route label is missing from the exposition:\n%s", published)
        }
    })

    t.Run("edge: the scrape needs no session", func(t *testing.T) {
        // It sits with the other operations routes, unauthenticated, published
        // on the loopback address only. Everything on it is a bounded
        // enumeration by rule, so there is nothing here worth a token.
        fixture := newStage(t, stageOptions{})

        if published := fixture.exposition(t); published == "" {
            t.Fatal("an unauthenticated scrape returned nothing")
        }
    })
}
