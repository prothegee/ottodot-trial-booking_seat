package main

import (
    "context"
    "errors"
    "fmt"
    "net/http"

    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/observability"
)

// listenAddress is where the api binds.
//
// It binds every interface rather than the loopback one, and the reason is worth
// being exact about, because the project rule reads the other way. This process
// runs inside a container, and a container that binds 127.0.0.1 is reachable by
// nothing at all, including the port its own compose file publishes.
//
// The loopback restriction is real and it is enforced one layer out, in compose,
// where the port is published as `127.0.0.1:9000:9000`. The container boundary is
// what makes "every interface" mean "the container's own network".
func listenAddress(port int) string {
    return fmt.Sprintf(":%d", port)
}

// newListener builds the http server the api runs.
//
// Every timeout is a configuration value rather than a constant, because a slow
// network and a fast one want different numbers and neither should need a
// rebuild. The idle timeout is derived rather than configured: it only has to
// outlast a read, and a third number to keep in step would be a third number to
// get wrong.
func newListener(settings config.ApiSettings, handler http.Handler) *http.Server {
    return &http.Server{
        Addr:         listenAddress(settings.Port),
        Handler:      handler,
        ReadTimeout:  settings.ReadTimeout,
        WriteTimeout: settings.WriteTimeout,
        IdleTimeout:  2 * settings.ReadTimeout,
    }
}

// serve starts the listener and reports a failure that is not the ordinary
// shutdown.
//
// http.ErrServerClosed is what Shutdown causes, so it is the success case here
// rather than a failure. Logging it would put a line that reads like a problem
// into every clean stop.
func serve(listener *http.Server, logger *observability.Logger) {
    if err := listener.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
        logger.Error("the listener stopped", observability.FieldReason, err.Error())
    }
}

// stopListening gives in flight requests a bounded chance to finish.
//
// A failure here is written down and not returned. The process is stopping
// either way, and the only thing an error means is that somebody's request was
// cut off, which is worth a line and is not worth a non-zero exit.
func stopListening(listener *http.Server, timeout config.ApiSettings, logger *observability.Logger) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout.ShutdownTimeout)
    defer cancel()

    if err := listener.Shutdown(ctx); err != nil {
        logger.Error("the listener did not close cleanly", observability.FieldReason, err.Error())
    }
}
