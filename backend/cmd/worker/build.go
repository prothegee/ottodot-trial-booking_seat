package main

// buildVersion and buildCommit are stamped at link time by
// containers/Containerfile.worker.
//
// They are values rather than constants for exactly that reason: a constant
// cannot be set with -X, and the defaults below are what a local go build
// produces. A running worker that reports "dev" and "unknown" is telling the
// truth about how it was built.
//
// The worker has no /version route to publish them on, so they reach an operator
// through its first log line and through nothing else.
var (
    buildVersion = "dev"
    buildCommit  = "unknown"
)
