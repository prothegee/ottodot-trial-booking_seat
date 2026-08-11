package main

import "ottodot-trial-booking/backend/internal/operations"

// buildVersion, buildCommit, and buildTime are stamped at link time by
// containers/Containerfile.api.
//
// They are values rather than constants for exactly that reason: a constant
// cannot be set with -X, and the defaults below are what a local go build
// produces. A running service that reports "dev" and "unknown" is telling the
// truth about how it was built.
var (
    buildVersion = "dev"
    buildCommit  = "unknown"
    buildTime    = ""
)

// buildIdentity is what /version answers with.
//
// Nothing else about the process goes in it. No environment variable, no
// connection string, and no address: this route is unauthenticated, so anything
// on it is public.
func buildIdentity() operations.Identity {
    return operations.NewIdentity(buildVersion, buildCommit, buildTime)
}
