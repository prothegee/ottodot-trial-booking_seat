package main

import (
    "net/http"
    "strconv"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/operations"
    "ottodot-trial-booking/backend/internal/payment"
)

func TestTheMetricsPortIsTakenBeforeTheWorkerAnnouncesItself(t *testing.T) {
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

    t.Run("edge: a metrics port already held is refused rather than reported afterwards", func(t *testing.T) {
        // A held metrics port means a second worker is already running, and two
        // consumers of one queue is worth refusing. Binding inside the serving
        // goroutine reported it only after this process had started consuming.
        held, err := bindListener(&http.Server{Addr: "127.0.0.1:0"})
        if err != nil {
            t.Fatalf("a free port was refused: %v", err)
        }

        defer held.Close()

        second, err := bindListener(&http.Server{Addr: held.Addr().String()})
        if err == nil {
            second.Close()

            t.Fatal("a port already held was accepted, a second worker would consume the same queue")
        }
    })
}

func TestTheMetricsListenerIsReachableFromItsOwnContainer(t *testing.T) {
    t.Run("unit: the address is the port on every interface", func(t *testing.T) {
        // Binding 127.0.0.1 inside a container makes the port reachable by
        // nothing, including the compose file that publishes it. The loopback
        // restriction is enforced by that publishing, one layer out.
        if address := metricsAddress(9002); address != ":9002" {
            t.Fatalf("expected the port on every interface, got %q", address)
        }
    })

    t.Run("edge: the port the settings carry is the port that is used", func(t *testing.T) {
        // A hardcoded port here would make the configured one a lie, and a
        // port clash would be fixed in two places.
        for _, port := range []int{9002, 1, 65535} {
            address := metricsAddress(port)

            if !strings.HasSuffix(address, ":"+strconv.Itoa(port)) {
                t.Fatalf("expected port %d in the address, got %q", port, address)
            }
        }
    })
}

func TestTheBuildIdentityHasSomewhereToBeStamped(t *testing.T) {
    t.Run("unit: an unstamped build starts silent so the next source is asked", func(t *testing.T) {
        // The bug this case exists for: these two defaulted to "dev" and
        // "unknown", words that look like answers and stop configuration from
        // ever being consulted.
        if buildVersion != "" || buildCommit != "" {
            t.Fatalf("expected empty defaults, got %q and %q", buildVersion, buildCommit)
        }
    })

    t.Run("behaviour: an unstamped worker takes its version from configuration", func(t *testing.T) {
        version, _ := buildIdentity(config.BuildSettings{Version: "0.1.0"})

        if version != "0.1.0" {
            t.Fatalf("expected the configured version, got %q", version)
        }
    })

    t.Run("behaviour: a stamped worker describes itself, whatever the machine says", func(t *testing.T) {
        previousVersion, previousCommit := buildVersion, buildCommit

        t.Cleanup(func() {
            buildVersion, buildCommit = previousVersion, previousCommit
        })

        buildVersion, buildCommit = "1.4.0", "abcdef1"

        version, commit := buildIdentity(config.BuildSettings{Version: "0.1.0", Commit: "9999999"})

        if version != "1.4.0" || commit != "abcdef1" {
            t.Fatalf("configuration overrode the linker, got %q and %q", version, commit)
        }
    })

    t.Run("edge: a value no source can name is a word rather than a blank", func(t *testing.T) {
        // This goes straight into a log line, where an empty field reads as a
        // logger that dropped it.
        version, commit := buildIdentity(config.BuildSettings{})

        if version != operations.UnknownValue {
            t.Fatalf("expected unknown for a version nobody stated, got %q", version)
        }

        if commit == "" {
            t.Fatal("the commit was reported as an empty log field")
        }
    })
}

func TestTheRefundLineCarriesNothingAboutAPerson(t *testing.T) {
    t.Run("unit: the line names the attempt and both references", func(t *testing.T) {
        line := refundLine(payment.Refund{
            AttemptID:   "0192e009-0000-7000-8000-000000000001",
            ProviderRef: "mock_charge",
            RefundRef:   "mock_refund_charge",
        })

        for _, expected := range []string{"0192e009-0000-7000-8000-000000000001", "mock_charge", "mock_refund_charge"} {
            if !strings.Contains(line, expected) {
                t.Fatalf("expected %q in the line, got %q", expected, line)
            }
        }
    })

    t.Run("edge: the amount is not written down", func(t *testing.T) {
        // A log gets pasted into a chat window. What somebody paid is not
        // needed to trace a refund, and the attempt row already holds it.
        line := refundLine(payment.Refund{
            AttemptID:   "0192e009-0000-7000-8000-000000000001",
            ProviderRef: "mock_charge",
            RefundRef:   "mock_refund_charge",
            Amount:      payment.Amount{Cents: 4500, Currency: payment.DefaultCurrency},
        })

        if strings.Contains(line, "4500") || strings.Contains(line, payment.DefaultCurrency) {
            t.Fatalf("expected no amount in the line, got %q", line)
        }
    })
}
