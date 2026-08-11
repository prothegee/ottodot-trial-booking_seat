package httpx

import (
    "encoding/json"
    "net/http"

    "ottodot-trial-booking/backend/internal/observability"
)

// maxTelemetryBody caps how much this route will read.
//
// The batch cap in the observability package limits how many events are
// accepted, and this limits how many bytes are read to find that out. Without
// it, a caller could post a very large document and this service would decode
// all of it before deciding it was too big.
const maxTelemetryBody = 16 << 10

// TelemetryHandler turns client events into metrics.
//
// It exists because a browser cannot be scraped. Everything else Prometheus
// knows about comes from a process this project runs, and the client is the one
// part of the system that has to report on itself.
//
// That difference is the reason this handler is written the way it is: it is
// behind the parent role like every other route, it reads a bounded body, and
// every label value in the batch is checked against a fixed list before it is
// used. A scrape target is something this project runs, and this endpoint is
// something anybody signed in can post to.
type TelemetryHandler struct {
    telemetry *observability.Telemetry
}

// NewTelemetryHandler wires the route.
//
// Param:
// telemetry - *observability.Telemetry (the converter, never nil)
//
// Return:
//   - the handler
//   - ErrIncompleteRoutes when there is no converter, refused here rather than
//     as a route that reads bodies and throws them away
func NewTelemetryHandler(telemetry *observability.Telemetry) (*TelemetryHandler, error) {
    if telemetry == nil {
        return nil, ErrIncompleteRoutes
    }

    return &TelemetryHandler{telemetry: telemetry}, nil
}

// record accepts one batch.
//
// Note:
//   - the answer is 204 with no body at all. The client swallows every failure
//     from this route on purpose, so there is nothing for it to read, and
//     sending a tally back would invite somebody to start acting on one.
//   - monitoring must never break a booking. That rule is the client's, and this
//     side keeps its half of it by never asking the client to do anything about
//     what happened here.
func (handler *TelemetryHandler) record(response http.ResponseWriter, request *http.Request) {
    var batch observability.Batch

    decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxTelemetryBody))

    if err := decoder.Decode(&batch); err != nil {
        Deny(response, request, observability.ErrTelemetryRefused)

        return
    }

    if _, err := handler.telemetry.Record(batch); err != nil {
        Deny(response, request, err)

        return
    }

    response.Header().Set("Cache-Control", noStorePolicy)
    response.WriteHeader(http.StatusNoContent)
}
