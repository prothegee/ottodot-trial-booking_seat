// Command worker consumes the job queue.
//
// It is a second process rather than a goroutine inside the api, and that is
// the one design decision this file exists to express. The api can restart or
// scale without touching background work, the queue lives in the database so
// nothing in flight dies with a process, and a worker falling over is visible
// as a worker falling over rather than as slow requests.
//
// It serves no public route. The listener on the metrics port carries liveness
// and the exposition, so Prometheus can scrape it and a restart loop shows up
// on a graph.
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// startupTimeout caps how long the process waits for both pools. Past it, a
// wrong address is a startup failure with a clear message rather than a process
// that hangs and looks alive.
const startupTimeout = 30 * time.Second

// shutdownTimeout caps how long the listener is given to finish in flight
// scrapes once the signal arrives.
const shutdownTimeout = 10 * time.Second

// buildRunner wires the queue to both handlers.
//
// Everything here is the primary pool. A worker decides whether a hold has
// lapsed and whether money has gone back, and both are decisions, so neither
// may read a replica that is a moment behind.
func buildRunner(pools *database.Pools, settings config.Config) (*worker.Runner, error) {
    bookingService, err := booking.NewService(booking.NewPostgresRepository(pools.Primary()), booking.DefaultSettings())
    if err != nil {
        return nil, fmt.Errorf("the booking service: %w", err)
    }

    paymentService, err := payment.NewService(
        payment.NewPostgresRepository(pools.Primary()),
        payment.NewMockProvider(),
        payment.Settings{})
    if err != nil {
        return nil, fmt.Errorf("the payment service: %w", err)
    }

    expireHold, err := worker.NewExpireHoldHandler(bookingService)
    if err != nil {
        return nil, fmt.Errorf("the expiry handler: %w", err)
    }

    reconcile, err := worker.NewReconcileRefundHandler(bookingService, paymentService, recordRefund)
    if err != nil {
        return nil, fmt.Errorf("the reconciliation handler: %w", err)
    }

    handlers := worker.Registry{}

    if err := handlers.Register(queue.KindExpireHold, expireHold); err != nil {
        return nil, fmt.Errorf("registering the expiry handler: %w", err)
    }

    if err := handlers.Register(queue.KindReconcileRefund, reconcile); err != nil {
        return nil, fmt.Errorf("registering the reconciliation handler: %w", err)
    }

    runnerSettings := worker.DefaultSettings()
    runnerSettings.PollInterval = settings.Worker.PollInterval
    runnerSettings.OnError = func(err error) { log.Printf("worker: %v", err) }

    return worker.NewRunner(queue.NewPostgresQueue(pools.Primary()), handlers, runnerSettings)
}

// buildVersion and buildCommit are stamped at link time by
// containers/Containerfile.worker. They are values rather than constants for
// exactly that reason, and the defaults are what a local go build produces.
var (
    buildVersion = "dev"
    buildCommit  = "unknown"
)

// metricsAddress is where the listener binds.
//
// It binds every interface rather than the loopback one, and the reason is
// worth being exact about, because the project rule reads the other way. This
// process runs inside a container, and a container that binds 127.0.0.1 is
// reachable by nothing at all, including the port its own compose file
// publishes.
//
// The loopback restriction is real and it is enforced one layer out, in
// compose, where the port is published as `127.0.0.1:9002:9002`. The container
// boundary is what makes "every interface" mean "the container's own network",
// which is the narrow thing the rule is asking for.
func metricsAddress(port int) string {
    return fmt.Sprintf(":%d", port)
}

// refundLine is what gets written down when money goes back.
//
// A refund has no table, so this line is where the reference lives until the
// operator surface in phase 6 gives it a home. It carries the attempt and the
// two provider references, and nothing about a person: no parent, no child, no
// class, and no amount.
func refundLine(refund payment.Refund) string {
    return fmt.Sprintf("worker: refund settled, attempt %s, charge %s, refund %s",
        refund.AttemptID, refund.ProviderRef, refund.RefundRef)
}

// recordRefund puts that line where an operator will find it.
func recordRefund(refund payment.Refund) {
    log.Print(refundLine(refund))
}

// serveMetrics starts the listener and reports a failure that is not the
// ordinary shutdown.
func serveMetrics(listener *http.Server) {
    if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Printf("worker: the metrics listener stopped: %v", err)
    }
}

// run is the whole process, written to return an error rather than to exit, so
// every failure leaves through one place.
func run() error {
    settings, err := config.LoadFromEnvironment()
    if err != nil {
        return fmt.Errorf("the configuration was refused: %w", err)
    }

    startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
    defer cancelStartup()

    pools, err := database.Open(startupCtx, database.Settings{
        PrimaryURL:     settings.Database.PrimaryURL.Reveal(),
        ReplicaURL:     settings.Database.ReplicaURL.Reveal(),
        MaxConnections: settings.Database.MaxConnections,
        ConnectTimeout: settings.Database.ConnectTimeout,
    })
    if err != nil {
        return fmt.Errorf("the database is not reachable: %w", err)
    }

    defer pools.Close()

    runner, err := buildRunner(pools, settings)
    if err != nil {
        return fmt.Errorf("the worker could not be wired: %w", err)
    }

    jobs := queue.NewPostgresQueue(pools.Primary())

    handler, err := worker.NewListenerHandler(runner.Counters(), func(ctx context.Context) (queue.Depth, error) {
        return jobs.Depth(ctx, queue.DepthRequest{
            Now:         time.Now(),
            MaxAttempts: worker.DefaultSettings().MaxAttempts,
        })
    })
    if err != nil {
        return fmt.Errorf("the metrics listener could not be wired: %w", err)
    }

    address := metricsAddress(settings.Worker.MetricsPort)
    listener := worker.NewListener(address, handler)

    go serveMetrics(listener)

    log.Printf("worker: version %s, commit %s, consuming the queue, metrics on %s", buildVersion, buildCommit, address)

    // The signal context is what ends the loop. Everything below it unwinds in
    // order: stop claiming, then stop answering scrapes, then close the pools.
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    if err := runner.Run(ctx); err != nil {
        return fmt.Errorf("the worker stopped: %w", err)
    }

    shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
    defer cancelShutdown()

    if err := listener.Shutdown(shutdownCtx); err != nil {
        log.Printf("worker: the metrics listener did not close cleanly: %v", err)
    }

    log.Print("worker: stopped")

    return nil
}

func main() {
    if err := run(); err != nil {
        log.Printf("worker: %v", err)
        os.Exit(1)
    }
}
