package main

import (
    "fmt"
    "log/slog"
    "os"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
)

// run is the whole process, written to return an error rather than to exit, so
// every failure leaves through one place.
//
// The unwind order is the part worth reading. Claiming stops first, then the
// scrapes, then the pools, and that order is not interchangeable: stopping the
// pools first would fail whatever job was in flight, and stopping the scrapes
// first would leave the last minute of the worker's life invisible.
func run() error {
    settings, err := config.LoadFromEnvironment()
    if err != nil {
        return fmt.Errorf("the configuration was refused: %w", err)
    }

    watch := bootstrap.NewObservability(os.Stdout, slog.LevelInfo)

    startupCtx, cancelStartup := bootstrap.StartupContext()
    defer cancelStartup()

    pools, err := bootstrap.OpenDatabase(startupCtx, settings)
    if err != nil {
        return err
    }

    defer pools.Close()

    runner, err := buildRunner(pools, watch, settings)
    if err != nil {
        return fmt.Errorf("the worker could not be wired: %w", err)
    }

    address := metricsAddress(settings.Worker.MetricsPort)

    listener, err := buildListener(runner, pools, watch, address)
    if err != nil {
        return err
    }

    go serveMetrics(listener, watch.Logger)

    watch.Logger.Info("the worker is consuming the queue",
        "version", buildVersion,
        "commit", buildCommit,
        "metrics_address", address)

    ctx, stop := bootstrap.ShutdownSignal()
    defer stop()

    if err := runner.Run(ctx); err != nil {
        return fmt.Errorf("the worker stopped: %w", err)
    }

    stopListening(listener, watch.Logger)

    watch.Logger.Info("the worker has stopped")

    return nil
}
