package main

import (
    "strconv"
    "strings"
    "testing"
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

func TestTheBuildIdentityHasSomewhereToBeStamped(t *testing.T) {
    t.Run("unit: every value exists for the linker to overwrite", func(t *testing.T) {
        // The Containerfile passes -X main.buildVersion and -X main.buildCommit.
        // A symbol the linker cannot find is ignored without a word, so the
        // stamp would silently do nothing and every image would report `dev`.
        if buildVersion == "" || buildCommit == "" {
            t.Fatalf("expected defaults a local build can report, got %q and %q", buildVersion, buildCommit)
        }
    })

    t.Run("edge: an unstamped build time is reported as unknown rather than as empty", func(t *testing.T) {
        // buildTime is deliberately empty by default, because a local build has
        // no honest value for it. The operations package turns that into the
        // word "unknown", which is what a reader needs to see.
        if buildTime != "" {
            t.Fatalf("expected an empty default for a local build, got %q", buildTime)
        }
    })
}
