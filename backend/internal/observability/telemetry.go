package observability

import "errors"

// The kinds of event the client may report.
const (
    EventPageLoad    = "page_load"
    EventApiError    = "api_error"
    EventFunnelStep  = "funnel_step"
    EventCacheLookup = "cache_lookup"
)

// MaxBatchEvents caps one post.
//
// A browser batches every ten seconds, so a normal batch holds single figures.
// The cap is what stops one caller turning the endpoint into a way to spend this
// service's cpu, and it is a refusal rather than a truncation so a client that
// hits it finds out.
const MaxBatchEvents = 50

// MaxPageLoadSeconds caps a reported duration.
//
// The number arrives from a browser, so it is a claim rather than a measurement.
// A histogram fed an absurd value is not wrong so much as useless: one bogus
// sample drags every quantile with it and the panel stops describing anybody.
const MaxPageLoadSeconds = 120.0

// ErrTelemetryRefused means a batch was not usable at all.
var ErrTelemetryRefused = errors.New("observability: the telemetry batch was refused")

// Event is one thing the client saw.
//
// Every field is a label value or a duration. There is no identifier on it and
// no free text, which is what lets this endpoint be open to any signed in parent
// without it becoming a way to write into the monitoring system.
type Event struct {
    Kind    string  `json:"kind"`
    Route   string  `json:"route,omitempty"`
    Code    string  `json:"code,omitempty"`
    Step    string  `json:"step,omitempty"`
    Result  string  `json:"result,omitempty"`
    Seconds float64 `json:"seconds,omitempty"`
}

// Batch is what one post carries.
type Batch struct {
    Events []Event `json:"events"`
}

// Tally is what a batch turned into.
//
// Dropped events are counted and reported rather than being silently ignored,
// because the two failures this endpoint can have look identical from the
// outside: a client sending nothing, and a client sending things this service
// does not recognise. The count tells them apart.
type Tally struct {
    Accepted int
    Dropped  int
}

// Telemetry turns client events into metrics.
type Telemetry struct {
    metrics *FrontendMetrics
}

// NewTelemetry wires the converter.
//
// Param:
// metrics - *FrontendMetrics (where accepted events land, nil for nowhere)
//
// Return:
//   - the converter
func NewTelemetry(metrics *FrontendMetrics) *Telemetry {
    return &Telemetry{metrics: metrics}
}

// Record converts one batch.
//
// Note:
//   - an event this service does not recognise is dropped, not rejected. One
//     stale field from a client that has not been reloaded must not throw away
//     the nine good events posted alongside it.
//
// Param:
// batch - Batch (what the client posted)
//
// Return:
//   - the tally of what was kept and what was dropped
//   - ErrTelemetryRefused when the batch is empty or over the cap, which are the
//     two cases where nothing usable arrived at all
func (telemetry *Telemetry) Record(batch Batch) (Tally, error) {
    if len(batch.Events) == 0 || len(batch.Events) > MaxBatchEvents {
        return Tally{}, ErrTelemetryRefused
    }

    var tally Tally

    for _, event := range batch.Events {
        if telemetry.record(event) {
            tally.Accepted++

            continue
        }

        tally.Dropped++
    }

    return tally, nil
}

// record converts one event, and reports whether it was one this service knows.
func (telemetry *Telemetry) record(event Event) bool {
    switch event.Kind {
    case EventPageLoad:
        if !KnownRoute(event.Route) || event.Seconds <= 0 || event.Seconds > MaxPageLoadSeconds {
            return false
        }

        telemetry.metrics.PageLoad(event.Route, event.Seconds)

        return true

    case EventApiError:
        if !KnownCode(event.Code) {
            return false
        }

        telemetry.metrics.ApiError(event.Code)

        return true

    case EventFunnelStep:
        if !KnownStep(event.Step) {
            return false
        }

        telemetry.metrics.FunnelStep(event.Step)

        return true

    case EventCacheLookup:
        if !KnownClientResult(event.Result) {
            return false
        }

        telemetry.metrics.ClientCacheLookup(event.Result)

        return true

    default:
        return false
    }
}
