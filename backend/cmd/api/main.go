// Command api serves the http surface.
//
// It owns no rule. Every decision this binary makes has already been made in a
// package that can be tested without a socket, and this directory is the one
// place that says which implementation of each of those a running service uses.
// That is what a composition root is, and it is why nothing here has a test of
// its own beyond the few pure functions that ended up here.
//
// It is split by what each file assembles rather than kept as one wiring file,
// so a reviewer looking for how the session is put together does not have to
// read how the queue is:
//
//	build.go          what this binary was built from
//	dependencies.go   the two stores held open for the whole run
//	session.go        authentication
//	checkout.go       the seat, the money, and the queue
//	reads.go          the two advisory readers, and the pool they use
//	guards.go         the response cache, the rate limits, and the two refusals
//	operations.go     what liveness, readiness, and version answer
//	faults.go         the development only injection surface
//	routes.go         the router, assembled from all of the above
//	listener.go       the socket, and how it is given up
//	process.go        the order all of that happens in
package main

import (
    "log"
    "os"
)

func main() {
    if err := run(); err != nil {
        log.Printf("api: %v", err)
        os.Exit(1)
    }
}
