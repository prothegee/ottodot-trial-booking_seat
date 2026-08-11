// Package observability is what this service publishes about itself: the metric
// registry both binaries scrape from, the structured log every state change is
// written to, and the redaction that log passes through on its way out.
//
// It owns no rule and decides nothing. A package that records what happened must
// never be able to change what happens, which is why nothing here returns an
// error a caller is expected to act on and why every recording call is safe on a
// nil receiver.
package observability

import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/collectors"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry builds the one registry a process publishes.
//
// It is a fresh registry rather than the library's global default, and that is
// the whole reason this function exists. A global registry cannot be built twice,
// so a test that wanted two independent sets of counters would either panic on
// the second registration or leak numbers from one case into the next.
//
// Two collectors are added to it, and they are where layer one of the monitoring
// plan comes from at no cost: the process collector reads this process's cpu,
// resident memory, and file descriptors, and the Go collector reads goroutines,
// the heap, and garbage collection pauses.
//
// Return:
//   - the registry, with the two runtime collectors already on it
func NewRegistry() *prometheus.Registry {
    registry := prometheus.NewRegistry()

    registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
    registry.MustRegister(collectors.NewGoCollector())

    return registry
}

// Handler serves the exposition on /metrics.
//
// Note:
//   - the route is deliberately unauthenticated, which is the same decision
//     /healthz and /readyz already carry. It is reachable only on the loopback
//     address compose publishes, and everything it exposes is a bounded
//     enumeration by rule, so there is nothing on it worth a token.
//   - a collector that fails is reported as a 500 rather than being skipped
//     quietly. A scrape that silently drops half the metrics reads as a healthy
//     service with nothing happening.
//
// Param:
// gatherer - prometheus.Gatherer (the registry, or a fake in a test)
//
// Return:
//   - the handler, ready to be registered
func Handler(gatherer prometheus.Gatherer) http.Handler {
    return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
        ErrorHandling: promhttp.HTTPErrorOnError,
    })
}
