package operations

import "net/http"

// Handler serves the three operations routes.
type Handler struct {
    readiness *Readiness
    identity  Identity
}

// NewHandler wires the routes.
//
// Param:
// readiness - *Readiness (what /readyz checks, never nil)
// identity - Identity (what /version reports)
//
// Return:
//   - the handler
//   - ErrNoChecks when there is no readiness source, refused here rather than
//     as a route that answers ready no matter what is broken
func NewHandler(readiness *Readiness, identity Identity) (*Handler, error) {
    if readiness == nil {
        return nil, ErrNoChecks
    }

    return &Handler{readiness: readiness, identity: identity}, nil
}

// Register puts the three routes on a mux.
//
// Note:
//   - none of them is authenticated, and none of them may be. A liveness probe
//     runs before this service has a session to offer, and a readiness probe
//     that needed a working database to answer could never report that the
//     database is down.
//
// Param:
// mux - *http.ServeMux (where the routes are registered)
func (handler *Handler) Register(mux *http.ServeMux) {
    mux.HandleFunc(HealthPath, Liveness)
    mux.HandleFunc(ReadinessPath, handler.readiness.Handle)
    mux.HandleFunc(VersionPath, handler.identity.Handle)
}
