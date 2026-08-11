package operations

import (
    "encoding/json"
    "net/http"
    "runtime"
)

// ServiceName is what this binary calls itself. It is a constant rather than a
// configuration value, because a service that can be told what it is cannot be
// used to work out what is deployed.
const ServiceName = "ottodot-trial-booking-api"

// unknownValue is what an unstamped build reports. It is a word rather than an
// empty string, so a body always has the same shape and a reader can see that
// the value is missing rather than wonder whether the field is.
const unknownValue = "unknown"

// Identity is the build this process came from.
//
// Nothing here is read from the environment at request time. Every value is
// stamped at link time, which is what makes this route an answer about the
// binary rather than about the machine it happens to be running on.
type Identity struct {
    Service string `json:"service"`
    Version string `json:"version"`
    Commit  string `json:"commit"`
    BuiltAt string `json:"built_at"`
    Runtime string `json:"runtime"`
}

// NewIdentity fills in the parts this process can know for itself.
//
// Note:
//   - the runtime comes from the Go runtime rather than from a build flag, so
//     it cannot disagree with what is actually executing.
//   - no environment variable, no connection url, and no host name is reported.
//     This route needs no token, so anything it says is said to everyone.
//
// Param:
// version - string (stamped at link time, "dev" for a local build)
// commit - string (stamped at link time)
// builtAt - string (stamped at link time, an RFC 3339 instant)
//
// Return:
//   - the identity, with anything unstamped reported as unknown
func NewIdentity(version string, commit string, builtAt string) Identity {
    return Identity{
        Service: ServiceName,
        Version: orUnknown(version),
        Commit:  orUnknown(commit),
        BuiltAt: orUnknown(builtAt),
        Runtime: runtime.Version(),
    }
}

// Handle answers one version request.
func (identity Identity) Handle(response http.ResponseWriter, _ *http.Request) {
    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", "no-store")
    response.WriteHeader(http.StatusOK)

    _ = json.NewEncoder(response).Encode(identity)
}

// orUnknown keeps an unstamped field readable rather than empty.
func orUnknown(value string) string {
    if value == "" {
        return unknownValue
    }

    return value
}
