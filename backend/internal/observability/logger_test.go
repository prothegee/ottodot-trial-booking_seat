package observability_test

import (
    "bytes"
    "encoding/json"
    "log/slog"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/observability"
)

// captured is one written line, read back the way an operator's tooling reads it.
func captured(t *testing.T, written *bytes.Buffer) map[string]any {
    t.Helper()

    line := strings.TrimSpace(written.String())
    if line == "" {
        t.Fatal("nothing was written")
    }

    var fields map[string]any

    if err := json.Unmarshal([]byte(line), &fields); err != nil {
        t.Fatalf("the line is not json, so nothing can search it: %v", err)
    }

    return fields
}

func TestLogger(t *testing.T) {
    t.Run("integration: a state change carries the request id and the booking id", func(t *testing.T) {
        // This is the whole reason the log exists. A parent reports a failure by
        // reading a request id off a screen, and that id has to lead to what
        // happened to their booking.
        var written bytes.Buffer

        observability.NewLogger(&written, slog.LevelInfo).BookingStateChanged(observability.StateChange{
            RequestID: "req-0001",
            BookingID: "0192a000-0000-7000-8000-000000000031",
            StudentID: "0192a000-0000-7000-8000-000000000011",
            ClassID:   "0192a000-0000-7000-8000-000000000021",
            From:      "pending_payment",
            To:        "confirmed",
            SeatNo:    2,
        })

        fields := captured(t, &written)

        if fields[observability.FieldRequestID] != "req-0001" {
            t.Errorf("the request id is %v", fields[observability.FieldRequestID])
        }

        if fields[observability.FieldBookingID] != "0192a000-0000-7000-8000-000000000031" {
            t.Errorf("the booking id is %v", fields[observability.FieldBookingID])
        }

        if fields[observability.FieldFrom] != "pending_payment" || fields[observability.FieldTo] != "confirmed" {
            t.Errorf("the transition reads %v to %v", fields[observability.FieldFrom], fields[observability.FieldTo])
        }
    })

    t.Run("edge: a sensitive field loses its value at the writer", func(t *testing.T) {
        // The call site here does exactly the wrong thing on purpose. Redaction
        // that depended on the call site behaving would be a convention, and a
        // convention is what fails on the one line nobody reviewed.
        var written bytes.Buffer

        observability.NewLogger(&written, slog.LevelInfo).Info("sign in refused",
            slog.String("cookie", "ottodot_access=abcdef123456"),
            slog.String("authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.abc"))

        line := written.String()

        if strings.Contains(line, "abcdef123456") || strings.Contains(line, "eyJhbGciOiJIUzI1NiJ9") {
            t.Fatalf("a secret reached the output: %s", line)
        }
    })

    t.Run("edge: an address inside the message is scrubbed", func(t *testing.T) {
        var written bytes.Buffer

        observability.NewLogger(&written, slog.LevelInfo).
            Warn("no account matched parent.one@example.test")

        if strings.Contains(written.String(), "@example.test") {
            t.Fatalf("the address reached the output: %s", written.String())
        }
    })

    t.Run("edge: a secret nested in a group is scrubbed too", func(t *testing.T) {
        // Nesting is the obvious way for a sensitive value to arrive without the
        // top level field name giving it away.
        var written bytes.Buffer

        observability.NewLogger(&written, slog.LevelInfo).Info("request refused",
            slog.Group("headers", slog.String("authorization", "Bearer abcdef123456")))

        if strings.Contains(written.String(), "abcdef123456") {
            t.Fatalf("a nested secret reached the output: %s", written.String())
        }
    })

    t.Run("edge: a value bound with With is scrubbed on every later line", func(t *testing.T) {
        // A logger built with a bound field would otherwise carry that value on
        // every line without any of them passing through the attribute walk.
        var written bytes.Buffer

        logger := observability.NewLogger(&written, slog.LevelInfo)

        logger.Info("first", slog.String("cookie", "session=abcdef123456"))
        logger.Info("second", slog.String("cookie", "session=abcdef123456"))

        if strings.Contains(written.String(), "abcdef123456") {
            t.Fatalf("a bound secret reached the output: %s", written.String())
        }
    })

    t.Run("unit: a number keeps its type rather than being turned into text", func(t *testing.T) {
        var written bytes.Buffer

        observability.NewLogger(&written, slog.LevelInfo).BookingStateChanged(observability.StateChange{
            RequestID: "req-0002",
            BookingID: "0192a000-0000-7000-8000-000000000031",
            From:      "pending_payment",
            To:        "confirmed",
            SeatNo:    3,
        })

        fields := captured(t, &written)

        seat, isNumber := fields[observability.FieldSeatNo].(float64)
        if !isNumber || seat != 3 {
            t.Fatalf("the seat number reads %v, and a number written as text cannot be compared in a query", fields[observability.FieldSeatNo])
        }
    })

    t.Run("unit: a nil logger writes nothing rather than panicking", func(t *testing.T) {
        var logger *observability.Logger

        logger.Info("nothing to write to")
        logger.Warn("nothing to write to")
        logger.Error("nothing to write to")
        logger.BookingStateChanged(observability.StateChange{})
    })
}
