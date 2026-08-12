package faults

import (
    "encoding/json"
    "errors"
    "net/http"
    "time"
)

// The routes this surface answers on.
//
// They are outside `/api/v1` on purpose. This is not part of the contract the
// client is written against, it is a development tool, and putting it under the
// versioned prefix would suggest otherwise to anybody reading the route table.
const (
    ArmPath     = "POST /dev/faults"
    ListPath    = "GET /dev/faults"
    DisarmPath  = "DELETE /dev/faults"
    RoutePrefix = "/dev/faults"
)

// armBody is what a caller posts to arm a point.
//
// The lifetime arrives in seconds rather than as a duration string, so the
// request can be written with curl on the spot without anybody having to
// remember a format.
type armBody struct {
    Point        string `json:"point"`
    Count        int    `json:"count"`
    TTLInSeconds int    `json:"ttl_seconds"`
}

// listBody is what the list route answers with.
type listBody struct {
    Armed  []Armed  `json:"armed"`
    Points []string `json:"points"`
}

// refusalBody is the failure shape, kept to a code and a message so it reads the
// same way the rest of the api's refusals do.
type refusalBody struct {
    Error struct {
        Code    string `json:"code"`
        Message string `json:"message"`
    } `json:"error"`
}

// Handler serves the three fault routes.
//
// It is registered only when every guard has already passed, which is decided by
// the process that wires it and not here. That order matters: when the surface
// is off, these routes are not on the mux at all, so an unauthorised caller gets
// a 404 rather than a 403 that would confirm the surface exists.
type Handler struct {
    registry *Registry
}

// NewHandler wires the routes to a registry.
//
// Param:
// registry - *Registry (what is armed, never nil in a running service)
//
// Return:
//   - the handler
func NewHandler(registry *Registry) *Handler {
    return &Handler{registry: registry}
}

// Register puts the three routes on a mux.
//
// Note:
//   - the caller is responsible for wrapping these in the admin role check and
//     the write rate limit, exactly like every other mutating route. This method
//     registers handlers and enforces nothing about who is calling them.
//
// Param:
// mux - *http.ServeMux (where the routes go)
// wrap - func(http.Handler) http.Handler (the guard chain, nil for none)
func (handler *Handler) Register(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
    if wrap == nil {
        wrap = func(next http.Handler) http.Handler { return next }
    }

    mux.Handle(ArmPath, wrap(http.HandlerFunc(handler.arm)))
    mux.Handle(ListPath, wrap(http.HandlerFunc(handler.list)))
    mux.Handle(DisarmPath, wrap(http.HandlerFunc(handler.disarm)))
}

// arm makes one point fail.
func (handler *Handler) arm(response http.ResponseWriter, request *http.Request) {
    var body armBody

    if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
        refuse(response, http.StatusBadRequest, "invalid_request", "that request body was not readable")

        return
    }

    armed, err := handler.registry.Arm(ArmRequest{
        Point:    body.Point,
        Count:    body.Count,
        Lifetime: time.Duration(body.TTLInSeconds) * time.Second,
    })

    switch {
    case errors.Is(err, ErrUnknownPoint):
        // The list of real points goes back with the refusal. A typo in a
        // demonstration script should be fixable from the answer rather than
        // from the source.
        writeJSON(response, http.StatusBadRequest, listBody{Armed: handler.registry.List(), Points: Points()})

        return

    case errors.Is(err, ErrInvalidRequest):
        refuse(response, http.StatusBadRequest, "invalid_request", "the count or the lifetime is outside its bounds")

        return

    case err != nil:
        refuse(response, http.StatusInternalServerError, "internal_error", "the point could not be armed")

        return
    }

    writeJSON(response, http.StatusOK, armed)
}

// list is what is armed right now, and what could be.
func (handler *Handler) list(response http.ResponseWriter, _ *http.Request) {
    writeJSON(response, http.StatusOK, listBody{Armed: handler.registry.List(), Points: Points()})
}

// disarm clears everything, and answers the same way whether anything was armed
// or not. A recovery step that could fail because there was nothing to recover
// from would be worse than useless.
func (handler *Handler) disarm(response http.ResponseWriter, _ *http.Request) {
    handler.registry.Disarm()

    writeJSON(response, http.StatusOK, listBody{Armed: []Armed{}, Points: Points()})
}

// writeJSON answers with a body and no caching at all.
func writeJSON(response http.ResponseWriter, status int, body any) {
    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", "no-store")
    response.WriteHeader(status)

    _ = json.NewEncoder(response).Encode(body)
}

// refuse answers with the same envelope shape the rest of the api uses.
func refuse(response http.ResponseWriter, status int, code string, message string) {
    var body refusalBody

    body.Error.Code = code
    body.Error.Message = message

    writeJSON(response, status, body)
}
