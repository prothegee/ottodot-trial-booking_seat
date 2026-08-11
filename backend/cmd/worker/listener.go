package main

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    "time"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/observability"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// shutdownTimeout caps how long the listener is given to finish in flight
// scrapes once the signal arrives.
//
// It is short because a scrape is one query. Anything still running past this
// was never going to finish.
const shutdownTimeout = 10 * time.Second

// metricsAddress is where the listener binds.
//
// It binds every interface rather than the loopback one, and the reason is worth
// being exact about, because the project rule reads the other way. This process
// runs inside a container, and a container that binds 127.0.0.1 is reachable by
// nothing at all, including the port its own compose file publishes.
//
// The loopback restriction is real and it is enforced one layer out, in compose,
// where the port is published as `127.0.0.1:9002:9002`. The container boundary is
// what makes "every interface" mean "the container's own network", which is the
// narrow thing the rule is asking for.
func metricsAddress(port int) string {
    return fmt.Sprintf(":%d", port)
}

// buildListener wires liveness and the exposition onto the metrics port.
//
// The queue depth is read at scrape time rather than tracked as jobs move,
// because the queue is the only thing that knows it. The attempt cap is bound in
// here once, so a scrape does not have to know the runner's policy.
//
// Param:
// runner - *worker.Runner (whose counters are published)
// pools - *database.Pools (the primary, where the queue lives)
// watch - bootstrap.Observability (the registry the scrape renders)
// address - string (host and port)
//
// Return:
//   - the server, which the caller starts and shuts down
//   - an error when a collaborator is missing, refused here rather than as a
//     panic on the first scrape
func buildListener(runner *worker.Runner, pools *database.Pools, watch bootstrap.Observability, address string) (*http.Server, error) {
    jobs := queue.NewPostgresQueue(pools.Primary())

    handler, err := worker.NewListenerHandler(runner.Counters(), func(ctx context.Context) (queue.Depth, error) {
        return jobs.Depth(ctx, queue.DepthRequest{
            Now:         time.Now(),
            MaxAttempts: worker.DefaultSettings().MaxAttempts,
        })
    }, watch.Exposition)
    if err != nil {
        return nil, fmt.Errorf("the metrics listener could not be wired: %w", err)
    }

    return worker.NewListener(address, handler), nil
}

// serveMetrics starts the listener and reports a failure that is not the
// ordinary shutdown.
func serveMetrics(listener *http.Server, logger *observability.Logger) {
    if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        logger.Error("the metrics listener stopped", observability.FieldReason, err.Error())
    }
}

// stopListening gives in flight scrapes a bounded chance to finish.
//
// A failure here is written down and not returned. The process is stopping
// either way, and the only thing an error means is that one scrape was cut off.
func stopListening(listener *http.Server, logger *observability.Logger) {
    ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
    defer cancel()

    if err := listener.Shutdown(ctx); err != nil {
        logger.Error("the metrics listener did not close cleanly", observability.FieldReason, err.Error())
    }
}
