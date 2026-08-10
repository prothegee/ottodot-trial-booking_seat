package worker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ottodot-trial-booking/backend/internal/queue"
)

// The two routes this listener serves.
//
// The worker has no public surface. It accepts no request that changes
// anything, and these two exist so a container orchestrator can tell whether
// the process is alive and Prometheus can tell whether it is working.
//
// The other two routes the api serves, `/readyz` and `/version`, arrive with
// `internal/operations` in phase 6. They are left out rather than written twice
// and thrown away.
const (
	healthPath  = "/healthz"
	metricsPath = "/metrics"
)

// The timeouts on the listener. They are short because the two handlers here do
// almost nothing: a scrape that cannot finish in five seconds is a queue that
// stopped answering, and hanging on to the connection helps nobody.
const (
	listenerReadTimeout  = 5 * time.Second
	listenerWriteTimeout = 5 * time.Second
	listenerIdleTimeout  = 30 * time.Second

	// depthTimeout caps the one query a scrape makes. Prometheus scrapes on a
	// timer, so a slow answer must fail rather than pile up.
	depthTimeout = 3 * time.Second
)

// DepthReader is how the listener asks the queue what it is holding.
//
// It is a function rather than the queue itself so the listener can be tested
// without one, and so the attempt cap the runner uses is bound in once at
// startup instead of being repeated at every scrape.
type DepthReader func(ctx context.Context) (queue.Depth, error)

// NewListenerHandler builds the routes the worker answers on.
//
// It returns a handler rather than a server, so a test drives it directly and
// the process that owns the port decides everything about the socket.
//
// Param:
// counters - *Counters (what this worker has done, never nil)
// readDepth - DepthReader (what the queue holds right now, never nil)
//
// Return:
//   - the handler, serving the two routes above and 404 for everything else
//   - ErrInvalidSettings when either argument is missing, refused here rather
//     than as a panic on the first scrape
func NewListenerHandler(counters *Counters, readDepth DepthReader) (http.Handler, error) {
	if counters == nil || readDepth == nil {
		return nil, fmt.Errorf("%w: the listener needs counters and a way to read the queue", ErrInvalidSettings)
	}

	routes := http.NewServeMux()

	routes.HandleFunc(healthPath, func(writer http.ResponseWriter, _ *http.Request) {
		// Liveness touches no dependency on purpose. A worker whose database is
		// down is still alive, and restarting it would not bring the database
		// back.
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)

		fmt.Fprintln(writer, "ok")
	})

	routes.HandleFunc(metricsPath, func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), depthTimeout)
		defer cancel()

		depth, err := readDepth(ctx)
		if err != nil {
			// A scrape that cannot read the queue must fail rather than
			// publish zeroes. Zeroes would read as a healthy empty queue, which
			// is the opposite of what is happening.
			http.Error(writer, "the queue depth could not be read", http.StatusServiceUnavailable)

			return
		}

		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writer.WriteHeader(http.StatusOK)

		WriteExposition(writer, counters.Snapshot(), depth)
	})

	return routes, nil
}

// NewListener builds the http server the worker's metrics port runs.
//
// Param:
// address - string (host and port, loopback only in this project)
// handler - http.Handler (what NewListenerHandler returned)
//
// Return:
//   - the server, which the caller starts and shuts down
func NewListener(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         address,
		Handler:      handler,
		ReadTimeout:  listenerReadTimeout,
		WriteTimeout: listenerWriteTimeout,
		IdleTimeout:  listenerIdleTimeout,
	}
}
