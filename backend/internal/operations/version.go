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

// UnknownValue is what a build no source could name reports. It is a word rather
// than an empty string, so a body always has the same shape and a reader can see
// that the value is missing rather than wonder whether the field is.
//
// It is exported because the worker reports the same two values in its first log
// line, and one word for "nobody knew" is easier to search for than two.
const UnknownValue = "unknown"

// Identity is the build this process came from.
//
// Every value is resolved once at startup and never at request time. Which
// source answered is decided by the process that builds this, and the order is
// written down there.
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
// version - string (already resolved, empty when no source stated one)
// commit - string (already resolved, empty when no source stated one)
// builtAt - string (already resolved, an RFC 3339 instant or empty)
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

// orUnknown keeps a field no source could answer readable rather than empty.
func orUnknown(value string) string {
    if value == "" {
        return UnknownValue
    }

    return value
}
