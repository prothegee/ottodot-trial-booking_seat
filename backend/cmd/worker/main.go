// Command worker consumes the job queue.
//
// It is a second process rather than a goroutine inside the api, and that is the
// one design decision this directory exists to express. The api can restart or
// scale without touching background work, the queue lives in the database so
// nothing in flight dies with a process, and a worker falling over is visible as
// a worker falling over rather than as slow requests.
//
// It serves no public route. The listener on the metrics port carries liveness
// and the exposition, so Prometheus can scrape it and a restart loop shows up on
// a graph.
//
// It is split by what each file assembles, the same way the api is:
//
//	build.go       what this binary was built from
//	handlers.go    the two job kinds and what runs them
//	runner.go      the claim loop and its policy
//	listener.go    the metrics socket, and how it is given up
//	refunds.go     where a settled refund is written down
//	process.go     the order all of that happens in
package main

import (
    "log"
    "os"
)

func main() {
    if err := run(); err != nil {
        log.Printf("worker: %v", err)
        os.Exit(1)
    }
}
