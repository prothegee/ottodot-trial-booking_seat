package main

import (
	"strconv"
	"strings"
	"testing"

	"ottodot-trial-booking/backend/internal/payment"
)

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
	t.Run("unit: both values exist for the linker to overwrite", func(t *testing.T) {
		// The Containerfile passes -X main.buildVersion and -X main.buildCommit.
		// A symbol the linker cannot find is ignored without a word, so the
		// stamp would silently do nothing and every image would report `dev`.
		if buildVersion == "" || buildCommit == "" {
			t.Fatalf("expected defaults a local build can report, got %q and %q", buildVersion, buildCommit)
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
