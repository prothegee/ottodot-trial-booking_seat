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
// The order below is the lifecycle and nothing else. Every question of what to
// build is answered in another file, which leaves this one short enough that the
// shape of a start and a stop can be read at a glance:
//
//	settings are loaded and refused as a whole, so one restart reports every
//	mistake rather than one restart per mistake
//
//	the recording surfaces are built before anything that could fail, so a
//	startup failure is written down the same way every later one is
//
//	the stores are opened under a deadline, so a wrong address is a failure
//	rather than a process that hangs and looks alive
//
//	the listener starts, and then this function does nothing at all until a
//	signal arrives
//
//	the unwind is the reverse: stop accepting, let in flight requests finish,
//	then give the stores back through the deferred close
func run() error {
    settings, err := config.LoadFromEnvironment()
    if err != nil {
        return fmt.Errorf("the configuration was refused: %w", err)
    }

    watch := bootstrap.NewObservability(os.Stdout, slog.LevelInfo)

    startupCtx, cancelStartup := bootstrap.StartupContext()
    defer cancelStartup()

    deps, err := openDependencies(startupCtx, settings)
    if err != nil {
        return err
    }

    defer deps.close()

    surface, bookings, err := buildSurface(deps, watch, settings)
    if err != nil {
        return fmt.Errorf("the api could not be wired: %w", err)
    }

    listener := newListener(settings.Api, surface)

    ctx, stop := bootstrap.ShutdownSignal()
    defer stop()

    // The gauges are sampled on their own timer rather than inside the scrape
    // handler, so /metrics has no database query on it and still answers when
    // the database is the thing that is broken.
    go sampleGauges(ctx, deps.pools, bookings, watch)

    go serve(listener, watch.Logger)

    watch.Logger.Info("the api is serving",
        "version", buildVersion,
        "commit", buildCommit,
        "address", listener.Addr)

    <-ctx.Done()

    stopListening(listener, settings.Api, watch.Logger)

    watch.Logger.Info("the api has stopped")

    return nil
}
