package operations_test

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"

    "ottodot-trial-booking/backend/internal/operations"
)

// Test 12: readiness reflects reality.
//
// The question this answers is not "does /readyz return 200". It is whether the
// answer tracks what is actually broken, in both directions:
//
//	a downed replica keeps the service in rotation, because every deciding read
//	already goes to the primary and taking a working service out costs an outage
//	nobody needed
//
//	a downed primary or a downed Redis takes it out, because neither a seat nor a
//	withdrawn token can be decided without them
//
//	liveness stays 200 throughout, because the process is fine and restarting it
//	would fix nothing
//
//	and nothing in any body names a host, a port, or a credential, because these
//	routes carry no token and answer everyone

// dependencyStack is the three dependencies with a switch on each, so a case
// reads like an operator pulling a cable.
type dependencyStack struct {
    mutex   sync.Mutex
    primary bool
    replica bool
    redis   bool
}

// newDependencyStack starts with everything up.
func newDependencyStack() *dependencyStack {
    return &dependencyStack{primary: true, replica: true, redis: true}
}

// probe builds the check for one dependency, reading the switch each time it is
// called rather than once at wiring, so a case can pull a cable mid-run.
func (stack *dependencyStack) probe(read func() bool) func(context.Context) error {
    return func(context.Context) error {
        stack.mutex.Lock()
        defer stack.mutex.Unlock()

        if read() {
            return nil
        }

        return probeFailure
    }
}

// dependencies wires the three in the shape the api uses.
func (stack *dependencyStack) dependencies() []operations.Dependency {
    return []operations.Dependency{
        {Name: "postgres_primary", Required: true, Probe: stack.probe(func() bool { return stack.primary })},
        {Name: "postgres_replica", Required: false, Probe: stack.probe(func() bool { return stack.replica })},
        {Name: "redis", Required: true, Probe: stack.probe(func() bool { return stack.redis })},
    }
}

// pull takes one dependency down.
func (stack *dependencyStack) pull(name string) {
    stack.mutex.Lock()
    defer stack.mutex.Unlock()

    switch name {
    case "postgres_primary":
        stack.primary = false
    case "postgres_replica":
        stack.replica = false
    case "redis":
        stack.redis = false
    }
}

// restore brings everything back.
func (stack *dependencyStack) restore() {
    stack.mutex.Lock()
    defer stack.mutex.Unlock()

    stack.primary = true
    stack.replica = true
    stack.redis = true
}

func TestSimulation12ReadinessReflectsReality(t *testing.T) {
    stack := newDependencyStack()

    readiness, err := operations.NewReadiness(stack.dependencies())
    if err != nil {
        t.Fatalf("cannot build readiness: %v", err)
    }

    handler, err := operations.NewHandler(readiness, operations.NewIdentity("0.1.0", "6b30337", "2026-08-10T14:00:00Z"))
    if err != nil {
        t.Fatalf("cannot build the handler: %v", err)
    }

    routes := http.NewServeMux()
    handler.Register(routes)

    // ask drives one route and hands back the status and the body.
    ask := func(path string) (int, string) {
        recorder := httptest.NewRecorder()
        routes.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

        return recorder.Code, recorder.Body.String()
    }

    // report decodes a readiness answer.
    report := func(body string) operations.Report {
        var decoded operations.Report

        if err := json.NewDecoder(strings.NewReader(body)).Decode(&decoded); err != nil {
            t.Fatalf("the readiness body is not readable: %v", err)
        }

        return decoded
    }

    // bodies is every readiness answer seen during the run. The leak check at
    // the end walks all of them, because a body written while something is
    // broken is the one most likely to carry a driver message.
    var bodies []string

    t.Run("stage 1: everything up is ready", func(t *testing.T) {
        stack.restore()

        status, body := ask("/readyz")
        bodies = append(bodies, body)

        if status != http.StatusOK || report(body).Status != operations.StatusReady {
            t.Fatalf("a healthy stack answered %d with %q", status, report(body).Status)
        }
    })

    t.Run("stage 2: the replica goes down and the service stays in rotation", func(t *testing.T) {
        stack.restore()
        stack.pull("postgres_replica")

        status, body := ask("/readyz")
        bodies = append(bodies, body)

        if status != http.StatusOK {
            t.Fatalf("a downed replica answered %d, taking a service out of rotation that can still decide everything", status)
        }

        answer := report(body)

        if answer.Status != operations.StatusDegraded {
            t.Fatalf("a downed replica reported %q, wanted degraded", answer.Status)
        }

        if answer.Checks["postgres_replica"] != "down" || answer.Checks["postgres_primary"] != "ok" {
            t.Fatalf("the checks read %+v, which does not match what was pulled", answer.Checks)
        }
    })

    t.Run("stage 3: the primary goes down and the service stops taking traffic", func(t *testing.T) {
        stack.restore()
        stack.pull("postgres_primary")

        status, body := ask("/readyz")
        bodies = append(bodies, body)

        if status != http.StatusServiceUnavailable {
            t.Fatalf("a downed primary answered %d, and no seat can be decided without it", status)
        }

        if report(body).Status != operations.StatusUnavailable {
            t.Fatalf("a downed primary reported %q", report(body).Status)
        }
    })

    t.Run("stage 4: redis goes down and the service stops taking traffic", func(t *testing.T) {
        stack.restore()
        stack.pull("redis")

        status, body := ask("/readyz")
        bodies = append(bodies, body)

        if status != http.StatusServiceUnavailable {
            t.Fatalf("a downed redis answered %d, and a withdrawn token cannot be checked without it", status)
        }
    })

    t.Run("stage 5: liveness stayed 200 through all of it", func(t *testing.T) {
        for _, broken := range []string{"postgres_primary", "postgres_replica", "redis"} {
            stack.restore()
            stack.pull(broken)

            status, _ := ask("/healthz")

            if status != http.StatusOK {
                t.Fatalf("liveness answered %d with %s down, and restarting the process would not fix it",
                    status, broken)
            }
        }

        stack.restore()
    })

    t.Run("stage 6: recovery is reported, so nothing has to be restarted to come back", func(t *testing.T) {
        stack.restore()
        stack.pull("postgres_primary")

        if status, _ := ask("/readyz"); status != http.StatusServiceUnavailable {
            t.Fatalf("the stack did not go unready, so recovery cannot be shown from here: %d", status)
        }

        stack.restore()

        status, body := ask("/readyz")
        bodies = append(bodies, body)

        if status != http.StatusOK || report(body).Status != operations.StatusReady {
            t.Fatalf("a recovered stack answered %d with %q", status, report(body).Status)
        }
    })

    t.Run("stage 7: no body ever named a host, a port, or a credential", func(t *testing.T) {
        _, version := ask("/version")
        bodies = append(bodies, version)

        leaks := []string{
            "10.1.2.3", "5432", "5433", "6379",
            "connection refused", "dial", "tcp",
            "password", "ottodot_development", "sslmode",
        }

        for _, body := range bodies {
            for _, leak := range leaks {
                if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
                    t.Fatalf("an operations body carries %q: %s", leak, body)
                }
            }
        }
    })
}
