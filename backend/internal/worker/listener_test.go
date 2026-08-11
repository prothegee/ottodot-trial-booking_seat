package worker_test

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// errQueueUnreachable is what a depth reader reports when the database is gone.
var errQueueUnreachable = errors.New("the queue could not be reached")

// newTestListener builds the handler over a depth reader that answers with one
// fixed value, or with a failure.
func newTestListener(t *testing.T, counters *worker.Counters, depth queue.Depth, answer error) http.Handler {
    t.Helper()

    handler, err := worker.NewListenerHandler(counters, func(_ context.Context) (queue.Depth, error) {
        return depth, answer
    })
    if err != nil {
        t.Fatalf("expected the listener to build, got: %v", err)
    }

    return handler
}

// call drives one request through the handler.
func call(handler http.Handler, path string) *httptest.ResponseRecorder {
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

    return recorder
}

func TestTheWorkerAnswersOnItsMetricsPort(t *testing.T) {
    t.Run("integration: liveness answers without touching the queue", func(t *testing.T) {
        // A worker whose database is down is still alive, and restarting it
        // would not bring the database back.
        handler := newTestListener(t, worker.NewCounters(), queue.Depth{}, errQueueUnreachable)

        recorded := call(handler, "/healthz")

        if recorded.Code != http.StatusOK {
            t.Fatalf("expected 200 while the process is alive, got %d", recorded.Code)
        }
    })

    t.Run("integration: the scrape carries the counters and the depth together", func(t *testing.T) {
        counters := worker.NewCounters()
        counters.Claimed(2)
        counters.Completed()

        handler := newTestListener(t, counters, queue.Depth{Ready: 4, Parked: 1}, nil)

        recorded := call(handler, "/metrics")

        if recorded.Code != http.StatusOK {
            t.Fatalf("expected 200, got %d", recorded.Code)
        }

        body := recorded.Body.String()

        for _, expected := range []string{
            "worker_jobs_claimed_total 2",
            "worker_jobs_completed_total 1",
            `queue_depth{state="ready"} 4`,
            `queue_depth{state="parked"} 1`,
        } {
            if !strings.Contains(body, expected) {
                t.Fatalf("expected %q in the scrape, got:\n%s", expected, body)
            }
        }
    })

    t.Run("edge: a queue that cannot be read fails the scrape rather than reporting zeroes", func(t *testing.T) {
        // Zeroes would read as a healthy empty queue, which is the opposite of
        // what is happening, and no alert would fire.
        handler := newTestListener(t, worker.NewCounters(), queue.Depth{}, errQueueUnreachable)

        recorded := call(handler, "/metrics")

        if recorded.Code != http.StatusServiceUnavailable {
            t.Fatalf("expected 503, got %d", recorded.Code)
        }

        if strings.Contains(recorded.Body.String(), "queue_depth") {
            t.Fatalf("a failed scrape must publish no series, got:\n%s", recorded.Body.String())
        }
    })

    t.Run("edge: the scrape declares the format Prometheus expects", func(t *testing.T) {
        handler := newTestListener(t, worker.NewCounters(), queue.Depth{}, nil)

        contentType := call(handler, "/metrics").Header().Get("Content-Type")

        if !strings.HasPrefix(contentType, "text/plain") {
            t.Fatalf("expected a text exposition, got %q", contentType)
        }
    })

    t.Run("edge: nothing else is served", func(t *testing.T) {
        // The worker has no public surface. Anything beyond these two routes
        // arriving here is a misrouted request, not a feature.
        handler := newTestListener(t, worker.NewCounters(), queue.Depth{}, nil)

        if recorded := call(handler, "/api/v1/bookings"); recorded.Code != http.StatusNotFound {
            t.Fatalf("expected 404, got %d", recorded.Code)
        }
    })

    t.Run("edge: a listener with no way to read the queue is refused at construction", func(t *testing.T) {
        if _, err := worker.NewListenerHandler(worker.NewCounters(), nil); !errors.Is(err, worker.ErrInvalidSettings) {
            t.Fatalf("expected ErrInvalidSettings, got: %v", err)
        }

        if _, err := worker.NewListenerHandler(nil, func(_ context.Context) (queue.Depth, error) {
            return queue.Depth{}, nil
        }); !errors.Is(err, worker.ErrInvalidSettings) {
            t.Fatalf("expected ErrInvalidSettings, got: %v", err)
        }
    })
}

func TestTheListenerIsBuiltWithTimeouts(t *testing.T) {
    t.Run("unit: the server refuses to hold a connection open forever", func(t *testing.T) {
        // A scrape runs on a timer. A hung connection with no timeout stacks up
        // one per scrape until the worker runs out of sockets.
        handler := newTestListener(t, worker.NewCounters(), queue.Depth{}, nil)

        server := worker.NewListener("127.0.0.1:9002", handler)

        if server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
            t.Fatalf("expected every timeout set, got %+v", server)
        }

        if server.Addr != "127.0.0.1:9002" {
            t.Fatalf("expected the address handed in, got %q", server.Addr)
        }
    })
}
