package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics is every number this service publishes, in the four groups the
// monitoring plan is written in.
//
// The groups are separate types rather than one flat struct because they answer
// different questions and are read by different rows of the dashboard. Access
// and transaction failures are the two the brief calls out as important, so they
// are their own groups rather than being mixed into the application numbers.
//
// Every method on every group is safe on a nil receiver, and that is what lets a
// package take *Metrics and record unconditionally. The alternative is a nil
// check before each call, which is the sort of thing that is right in nine places
// and missing in the tenth.
type Metrics struct {
    Access      *AccessMetrics
    Transaction *TransactionMetrics
    Application *ApplicationMetrics
    Frontend    *FrontendMetrics
}

// NewMetrics builds all four groups and registers them.
//
// Note:
//   - it panics rather than returning an error when a name is registered twice,
//     which is what MustRegister does and what is wanted here. A duplicate metric
//     name is a mistake in this file, not a runtime condition, and it is better
//     found on the first line of a test run than served as a half empty scrape.
//
// Param:
// registry - prometheus.Registerer (from NewRegistry, or a fresh one in a test)
//
// Return:
//   - the metrics, ready to record
func NewMetrics(registry prometheus.Registerer) *Metrics {
    return &Metrics{
        Access:      newAccessMetrics(registry),
        Transaction: newTransactionMetrics(registry),
        Application: newApplicationMetrics(registry),
        Frontend:    newFrontendMetrics(registry),
    }
}
