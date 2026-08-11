// Package operations serves the three routes nothing business related touches:
// liveness, readiness, and build identity.
//
// They are unversioned and sit at the root while every business route carries
// `/api/v1`, and the split is deliberate. A container orchestrator, a load
// balancer, and a deployment script all hard code these paths, and a path that
// moves with an api version is a path that breaks a probe on the day a contract
// changes.
//
// Nothing here answers with a host, a port, a connection url, or a credential.
// These routes need no token, so everything they say is said to everyone.
package operations

import (
    "net/http"
)

// The three routes this package serves.
const (
    HealthPath    = "GET /healthz"
    ReadinessPath = "GET /readyz"
    VersionPath   = "GET /version"
)

// Liveness answers whether this process is running.
//
// It touches no dependency, and that is the whole point of it. An api whose
// database is down is still alive, and restarting it would not bring the
// database back. Wiring a dependency into liveness is how one slow database
// turns into every api instance being killed and restarted at once.
func Liveness(response http.ResponseWriter, _ *http.Request) {
    response.Header().Set("Content-Type", "text/plain; charset=utf-8")
    response.Header().Set("Cache-Control", "no-store")
    response.WriteHeader(http.StatusOK)

    _, _ = response.Write([]byte("ok\n"))
}
