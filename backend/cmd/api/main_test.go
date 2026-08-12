package main

import (
    "net/http"
    "strconv"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/operations"
)

func TestTheListenerIsReachableFromItsOwnContainer(t *testing.T) {
    t.Run("unit: the address is the port on every interface", func(t *testing.T) {
        // Binding 127.0.0.1 inside a container makes the port reachable by
        // nothing, including the compose file that publishes it. The loopback
        // restriction is enforced by that publishing, one layer out.
        if address := listenAddress(9000); address != ":9000" {
            t.Fatalf("expected the port on every interface, got %q", address)
        }
    })

    t.Run("edge: the port the settings carry is the port that is used", func(t *testing.T) {
        // A hardcoded port here would make the configured one a lie, and a port
        // clash would be fixed in two places.
        for _, port := range []int{9000, 1, 65535} {
            address := listenAddress(port)

            if !strings.HasSuffix(address, ":"+strconv.Itoa(port)) {
                t.Fatalf("expected port %d in the address, got %q", port, address)
            }
        }
    })
}

func TestThePortIsTakenBeforeAnythingSaysItIsServing(t *testing.T) {
    t.Run("behaviour: a free port is bound and the socket holds it", func(t *testing.T) {
        socket, err := bindListener(&http.Server{Addr: "127.0.0.1:0"})
        if err != nil {
            t.Fatalf("a free port was refused: %v", err)
        }

        defer socket.Close()

        if socket.Addr().String() == "" {
            t.Fatal("the socket reported no address to answer on")
        }
    })

    t.Run("edge: a port already held is refused rather than reported afterwards", func(t *testing.T) {
        // The bug this case exists for: ListenAndServe binds inside the goroutine
        // that serves, so this failure arrived after the process had written that
        // the api was serving. It then stayed up answering nothing, which reads
        // as healthy to a supervisor and to anybody watching the log.
        held, err := bindListener(&http.Server{Addr: "127.0.0.1:0"})
        if err != nil {
            t.Fatalf("a free port was refused: %v", err)
        }

        defer held.Close()

        second, err := bindListener(&http.Server{Addr: held.Addr().String()})
        if err == nil {
            second.Close()

            t.Fatal("a port already held was accepted, the clash would surface after the start")
        }
    })
}

// stampedAs replaces the link time values for one test and puts them back after.
//
// They are package variables because that is the only kind of symbol -X can
// write to, which also makes them the only way a test can stand in for a linker.
func stampedAs(t *testing.T, version string, commit string, builtAt string) {
    t.Helper()

    previousVersion, previousCommit, previousTime := buildVersion, buildCommit, buildTime

    t.Cleanup(func() {
        buildVersion, buildCommit, buildTime = previousVersion, previousCommit, previousTime
    })

    buildVersion, buildCommit, buildTime = version, commit, builtAt
}

func TestTheBuildIdentityHasSomewhereToBeStamped(t *testing.T) {
    t.Run("unit: an unstamped build starts silent so the next source is asked", func(t *testing.T) {
        // The bug this case exists for: these three defaulted to "dev" and
        // "unknown", which are words that look like answers. Configuration was
        // never consulted, and every run reported a build nobody could identify.
        if buildVersion != "" || buildCommit != "" || buildTime != "" {
            t.Fatalf("expected empty defaults, got %q, %q and %q", buildVersion, buildCommit, buildTime)
        }
    })

    t.Run("behaviour: an unstamped build takes its version from configuration", func(t *testing.T) {
        identity := buildIdentity(config.BuildSettings{Version: "0.1.0"})

        if identity.Version != "0.1.0" {
            t.Fatalf("expected the configured version, got %q", identity.Version)
        }
    })

    t.Run("behaviour: a stamped binary describes itself, whatever the machine says", func(t *testing.T) {
        // A released image knows what it is. A setting on the host it happens to
        // be running on is not allowed to rename it.
        stampedAs(t, "1.4.0", "abcdef1", "2026-08-11T09:00:00Z")

        identity := buildIdentity(config.BuildSettings{Version: "0.1.0", Commit: "9999999"})

        if identity.Version != "1.4.0" || identity.Commit != "abcdef1" {
            t.Fatalf("configuration overrode the linker, got %q and %q", identity.Version, identity.Commit)
        }

        if identity.BuiltAt != "2026-08-11T09:00:00Z" {
            t.Fatalf("expected the stamped build time, got %q", identity.BuiltAt)
        }
    })

    t.Run("behaviour: a build time nobody stamped is read off the binary itself", func(t *testing.T) {
        // The container build has no honest value to hand in, and the test
        // binary on disk does. Either way this field stops saying unknown.
        identity := buildIdentity(config.BuildSettings{})

        if identity.BuiltAt == operations.UnknownValue {
            t.Fatal("the running binary is on disk and reported no build time")
        }
    })

    t.Run("edge: a field no source can name is a word rather than a blank", func(t *testing.T) {
        // A blank field reads as a bug in the page showing it. The word says
        // plainly that nobody knew.
        identity := buildIdentity(config.BuildSettings{})

        if identity.Version != operations.UnknownValue {
            t.Fatalf("expected unknown for a version nobody stated, got %q", identity.Version)
        }
    })

    t.Run("edge: the service name is not configurable", func(t *testing.T) {
        // A service that can be told what it is cannot be used to work out what
        // is deployed.
        if identity := buildIdentity(config.BuildSettings{}); identity.Service != operations.ServiceName {
            t.Fatalf("expected %q, got %q", operations.ServiceName, identity.Service)
        }
    })
}
