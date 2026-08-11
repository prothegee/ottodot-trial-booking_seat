package operations

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "sort"
    "time"
)

// probeTimeout caps how long one dependency check may take.
//
// A readiness probe runs on a timer, so an answer that arrives late is worse
// than an answer of "no": the orchestrator is still waiting when the next probe
// fires, and the two pile up.
const probeTimeout = 2 * time.Second

// The three states a readiness answer can be in.
//
// Degraded exists because "ready" and "not ready" are not enough here. The
// replica being down costs nothing that decides anything, so answering unready
// would take a working service out of rotation, and answering ready would hide
// a real problem.
const (
    StatusReady       = "ready"
    StatusDegraded    = "degraded"
    StatusUnavailable = "unavailable"
)

// The two words a single check answers with. They are deliberately the only two
// values: anything more descriptive would be a place for a host name or a driver
// message to end up in a public body.
const (
    checkOK   = "ok"
    checkDown = "down"
)

// ErrNoChecks means a readiness handler was built with nothing to check, which
// would answer ready forever and mean nothing.
var ErrNoChecks = errors.New("operations: readiness needs at least one required dependency")

// Dependency is one thing this service reaches out to.
type Dependency struct {
    // Name is what appears in the body. It is a fixed word chosen here, never
    // an address, so nothing about the deployment leaks through this route.
    Name string

    // Required decides what a failure means. A required dependency being down
    // makes the service unready. An advisory one being down makes it degraded,
    // because everything it serves can fall back to the primary.
    Required bool

    // Probe reports whether the dependency can be reached right now.
    Probe func(ctx context.Context) error
}

// Report is the readiness body.
//
// The checks are a map rather than a list because a client reads one by name,
// and the names are a closed set this service chose.
type Report struct {
    Status string            `json:"status"`
    Checks map[string]string `json:"checks"`
}

// Readiness answers whether this process should be sent traffic.
type Readiness struct {
    dependencies []Dependency
}

// NewReadiness wires the checks.
//
// Param:
// dependencies - []Dependency (what to check, and what a failure of each means)
//
// Return:
//   - the handler source
//   - ErrNoChecks when nothing required was given, refused here rather than as
//     a route that answers ready no matter what is broken
func NewReadiness(dependencies []Dependency) (*Readiness, error) {
    required := 0

    for _, dependency := range dependencies {
        if dependency.Name == "" || dependency.Probe == nil {
            return nil, ErrNoChecks
        }

        if dependency.Required {
            required++
        }
    }

    if required == 0 {
        return nil, ErrNoChecks
    }

    ordered := make([]Dependency, len(dependencies))
    copy(ordered, dependencies)

    sort.Slice(ordered, func(first int, second int) bool {
        return ordered[first].Name < ordered[second].Name
    })

    return &Readiness{dependencies: ordered}, nil
}

// Check runs every probe and reports what it found.
//
// Every probe runs, including after one has already failed. An operator reading
// this body wants to know everything that is down, not the first thing that was
// checked, and stopping early is how a second outage stays invisible until the
// first is fixed.
//
// Param:
// ctx - context.Context (cancelling it abandons the remaining probes)
//
// Return:
//   - the report
//   - the status to answer with: 200 for ready or degraded, 503 for unavailable
func (readiness *Readiness) Check(ctx context.Context) (Report, int) {
    report := Report{Status: StatusReady, Checks: make(map[string]string, len(readiness.dependencies))}

    for _, dependency := range readiness.dependencies {
        probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
        err := dependency.Probe(probeCtx)

        cancel()

        if err == nil {
            report.Checks[dependency.Name] = checkOK

            continue
        }

        report.Checks[dependency.Name] = checkDown

        if dependency.Required {
            report.Status = StatusUnavailable
        } else if report.Status == StatusReady {
            report.Status = StatusDegraded
        }
    }

    if report.Status == StatusUnavailable {
        return report, http.StatusServiceUnavailable
    }

    return report, http.StatusOK
}

// Handle answers one readiness request.
func (readiness *Readiness) Handle(response http.ResponseWriter, request *http.Request) {
    report, status := readiness.Check(request.Context())

    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("Cache-Control", "no-store")
    response.WriteHeader(status)

    // The body is a short map this package built, so a failed encode is not a
    // case that can happen, and answering a probe with a second failure about
    // the first one helps nobody.
    _ = json.NewEncoder(response).Encode(report)
}
