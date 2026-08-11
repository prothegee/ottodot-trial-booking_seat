// Package bootstrap is the infrastructure both binaries start from.
//
// It exists because the api and the worker open the same three things in the
// same way: the database pools, the Redis client, and the registry every metric
// and every log line goes through. Neither of those is a domain rule, and both
// were written twice before this package existed, which is how two processes end
// up disagreeing about a connect timeout.
//
// Nothing here decides anything about a booking. It opens connections, it builds
// the recording surfaces, and it hands them back. What each binary does with
// them is that binary's own composition root.
package bootstrap

import (
    "io"
    "log/slog"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"

    "ottodot-trial-booking/backend/internal/observability"
)

// Observability is everything a process publishes about itself.
//
// The three are built together and handed out together, because they have to
// share one registry. Two registries in one process would mean a scrape that
// returns half the numbers, and the half it returns would depend on which
// handler happened to be wired to the route.
type Observability struct {
    // Registry is the one set of collectors this process publishes.
    Registry *prometheus.Registry

    // Metrics is every number, in the four groups the monitoring plan uses.
    Metrics *observability.Metrics

    // Logger writes structured lines through the redaction rules.
    Logger *observability.Logger

    // Exposition is the handler Prometheus scrapes.
    Exposition http.Handler
}

// NewObservability builds the registry, the metrics, and the logger.
//
// Param:
// logTo - io.Writer (where log lines go, os.Stdout in a running service)
// level - slog.Level (the lowest level written)
//
// Return:
//   - the three surfaces, sharing one registry
func NewObservability(logTo io.Writer, level slog.Level) Observability {
    registry := observability.NewRegistry()

    return Observability{
        Registry:   registry,
        Metrics:    observability.NewMetrics(registry),
        Logger:     observability.NewLogger(logTo, level),
        Exposition: observability.Handler(registry),
    }
}
