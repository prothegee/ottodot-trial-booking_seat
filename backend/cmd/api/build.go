package main

import (
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/operations"
)

// buildVersion, buildCommit, and buildTime are stamped at link time by
// containers/Containerfile.api.
//
// They are values rather than constants for exactly that reason: a constant
// cannot be set with -X. They start empty rather than at "dev", because empty is
// how this file says nobody stamped anything, and a word like "dev" would end
// the search before the sources that do know have been asked.
var (
    buildVersion = ""
    buildCommit  = ""
    buildTime    = ""
)

// buildIdentity is what /version answers with.
//
// Note:
//   - the linker wins. A stamped binary describes itself, and nothing on the
//     machine it happens to be running on may say otherwise.
//   - configuration is next, and it is where a run from source gets its version:
//     BUILD_VERSION in config.json, which is the one committed place the number
//     is written down.
//   - what the toolchain and the binary itself record is last. Those two need
//     nothing written down anywhere, which is why a plain `go run` still names
//     its commit and the moment it was built.
//   - nothing else about the process goes in it. No environment variable, no
//     connection string, and no address: this route is unauthenticated, so
//     anything on it is public.
//
// Param:
// build - config.BuildSettings (what config.json or the environment states)
//
// Return:
//   - the identity, with anything no source could name reported as unknown
func buildIdentity(build config.BuildSettings) operations.Identity {
    return operations.NewIdentity(
        operations.FirstStated(buildVersion, build.Version),
        operations.FirstStated(buildCommit, build.Commit, operations.CommitFromBuildRecord()),
        operations.FirstStated(buildTime, operations.BuiltAtFromExecutable()),
    )
}
