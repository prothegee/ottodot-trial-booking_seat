package main

import (
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/operations"
)

// buildVersion and buildCommit are stamped at link time by
// containers/Containerfile.worker.
//
// They are values rather than constants for exactly that reason: a constant
// cannot be set with -X. They start empty rather than at "dev", because empty is
// how this file says nobody stamped anything, and a word like "dev" would end
// the search before the sources that do know have been asked.
//
// The worker has no /version route to publish them on, so they reach an operator
// through its first log line and through nothing else.
var (
    buildVersion = ""
    buildCommit  = ""
)

// buildIdentity is the version and commit this worker reports at startup.
//
// The order is the api's order, because a stack whose two halves answer the
// question differently is worse than one that cannot answer it at all: the
// linker, then configuration, then what the toolchain recorded.
//
// Param:
// build - config.BuildSettings (what config.json or the environment states)
//
// Return:
//   - the version, or unknown when no source named one
//   - the commit, or unknown when no source named one
func buildIdentity(build config.BuildSettings) (version string, commit string) {
    version = operations.FirstStated(buildVersion, build.Version, operations.UnknownValue)
    commit = operations.FirstStated(buildCommit, build.Commit,
        operations.CommitFromBuildRecord(), operations.UnknownValue)

    return version, commit
}
