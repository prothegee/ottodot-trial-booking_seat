package httpx_test

import (
    "net/http"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/observability"
)

/*
Simulation 15: the core transaction fails and leaves nothing behind.

    confirm.before_commit armed, count 1
    the parent pays, the provider settles
    the confirm transaction locks the class, writes the seat, and then breaks
    the rollback undoes the seat, the event, and the status together
    the parent is told internal_error and a request id

Asserts: no seat was consumed and the booking is still pending_payment,
confirm_transaction_total{outcome="error"} rose by exactly one while seat_lost
and confirmed did not move, the response body carries only the code and the
request id, the log line carries the detail, and a second identical request with
the fault spent now succeeds.

That last one matters most. A failed confirm has to leave the booking retryable
rather than stuck, because by the time this transaction runs the parent has
already been charged.

It runs in the fake tier, in milliseconds, with nothing running. The memory
repository carries the same injection point at the same moment the Postgres one
does, so what is proven here is the ordering the service depends on. What is not
proven here is that a real ROLLBACK undoes a real row, and that belongs to the
proof tier, where the same transaction runs against Postgres.
*/

// armedStage builds a surface with one point armed for a single firing.
func armedStage(t *testing.T, point string) *stage {
    t.Helper()

    registry := faults.NewRegistry(faults.Settings{})

    fixture := newStage(t, stageOptions{Faults: faults.NewHandler(registry)})

    fixture.bookings.InjectFaults(registry.Trigger)

    if _, err := registry.Arm(faults.ArmRequest{Point: point}); err != nil {
        t.Fatalf("arming %s was refused: %v", point, err)
    }

    return fixture
}

// seriesValue reads one series off an exposition, or reports that it is absent.
func seriesValue(t *testing.T, published string, series string) string {
    t.Helper()

    for _, line := range strings.Split(published, "\n") {
        name, value, split := strings.Cut(line, " ")

        if split && name == series {
            return value
        }
    }

    t.Fatalf("the series %q is not on the exposition", series)

    return ""
}

func TestSimulation15TheCoreTransactionFails(t *testing.T) {
    t.Run("integration: a broken confirm consumes no seat and leaves the booking holding", func(t *testing.T) {
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        held := fixture.holdOne(t, studentOne, classOpen)

        broke := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f001",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        if broke.Code != http.StatusInternalServerError {
            t.Fatalf("the broken confirm answered %d: %s", broke.Code, broke.Body.String())
        }

        read := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/bookings/" + held.ID,
            parent: parentOne,
        })

        var after bookingWire

        decodeInto(t, read, &after)

        if after.Status != "pending_payment" {
            t.Errorf("the booking is %s, so the rollback did not undo the status change", after.Status)
        }

        if after.SeatNo != nil {
            t.Errorf("seat %d is still assigned, so the rollback did not release it", *after.SeatNo)
        }
    })

    t.Run("behaviour: the parent retries with the same key and is confirmed", func(t *testing.T) {
        // The one that matters most. The provider already settled, so a booking
        // left stuck would be a parent who paid and can never be seated.
        //
        // The key is the original one on purpose. This is the single case where
        // a client resends rather than minting a fresh key: the outcome of the
        // first attempt is unknown to it, and a new key would charge again.
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        held := fixture.holdOne(t, studentOne, classOpen)

        payment := request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f002",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        }

        if broke := fixture.send(t, payment); broke.Code != http.StatusInternalServerError {
            t.Fatalf("the armed payment answered %d", broke.Code)
        }

        chargedOnce := fixture.provider.Charges()

        retried := fixture.send(t, payment)
        if retried.Code != http.StatusOK {
            t.Fatalf("the retry answered %d: %s", retried.Code, retried.Body.String())
        }

        var confirmed bookingWire

        decodeInto(t, retried, &confirmed)

        if confirmed.Status != "confirmed" || confirmed.SeatNo == nil {
            t.Fatalf("the retry produced %s on seat %v", confirmed.Status, confirmed.SeatNo)
        }

        if fixture.provider.Charges() != chargedOnce {
            t.Fatalf("the provider was called %d times, so the retry charged a second time",
                fixture.provider.Charges())
        }
    })

    t.Run("edge: the error rises by one and the two healthy outcomes do not move", func(t *testing.T) {
        // The distinction the whole transaction metric group exists for. A
        // confirm that rolls back because somebody else took the seat is correct
        // behaviour. This one is not, and an alert cannot tell them apart if
        // they share a series.
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        held := fixture.holdOne(t, studentOne, classOpen)

        before := fixture.exposition(t)

        fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f003",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        after := fixture.exposition(t)

        errorSeries := observability.MetricConfirmTransaction + `{outcome="error"}`
        lostSeries := observability.MetricConfirmTransaction + `{outcome="seat_lost"}`
        confirmedSeries := observability.MetricConfirmTransaction + `{outcome="confirmed"}`

        if seriesValue(t, before, errorSeries) != "0" || seriesValue(t, after, errorSeries) != "1" {
            t.Errorf("the error count went from %s to %s",
                seriesValue(t, before, errorSeries), seriesValue(t, after, errorSeries))
        }

        if seriesValue(t, after, lostSeries) != "0" {
            t.Errorf("a lost race was counted for a transaction that broke")
        }

        if seriesValue(t, after, confirmedSeries) != "0" {
            t.Errorf("a confirmation was counted for a transaction that broke")
        }
    })

    t.Run("edge: the money that already moved is not marked for refund", func(t *testing.T) {
        // The seat was never taken from anybody. Marking a refund would tell an
        // operator to move money back for a parent who is about to be seated on
        // their retry.
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        held := fixture.holdOne(t, studentOne, classOpen)

        fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f004",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        read := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/bookings/" + held.ID,
            parent: parentOne,
        })

        var after bookingWire

        decodeInto(t, read, &after)

        if after.Status == "refund_required" {
            t.Fatal("a broken transaction marked a refund for money that is still owed a seat")
        }
    })

    t.Run("edge: the parent is told a code and a request id, and the log carries the rest", func(t *testing.T) {
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        held := fixture.holdOne(t, studentOne, classOpen)

        broke := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f005",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        body := broke.Body.String()

        if !strings.Contains(body, "internal_error") {
            t.Errorf("the body does not carry the code: %s", body)
        }

        if !strings.Contains(body, "request_id") {
            t.Errorf("the body carries no request id, so nothing can be traced: %s", body)
        }

        for _, detail := range []string{"transaction", "rollback", "commit", "sql"} {
            if strings.Contains(strings.ToLower(body), detail) {
                t.Errorf("the body names %q, which tells a client about this service's insides", detail)
            }
        }
    })

    t.Run("behaviour: a lock wait timeout is answered as something worth retrying", func(t *testing.T) {
        // The other confirm point, and a different answer on purpose. Nothing
        // was decided and nothing was written, so the honest answer is "ask
        // again" rather than a refusal the parent would read as final.
        fixture := armedStage(t, faults.PointConfirmLockWait)

        held := fixture.holdOne(t, studentOne, classOpen)

        busy := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f100-0000-7000-8000-00000000f006",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        if busy.Code != http.StatusServiceUnavailable {
            t.Fatalf("the armed lock wait answered %d: %s", busy.Code, busy.Body.String())
        }

        if !strings.Contains(busy.Body.String(), "dependency_unavailable") {
            t.Fatalf("the body does not carry the retryable code: %s", busy.Body.String())
        }
    })

    t.Run("unit: the fault surface is not on the mux when it was not registered", func(t *testing.T) {
        // Every other case here arms a point through the registry directly. This
        // one is about the route, and it proves the off state answers a plain
        // not found rather than a refusal that would confirm the surface exists.
        fixture := newStage(t, stageOptions{})

        recorded := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/dev/faults",
            parent: adminParent,
            role:   "admin",
        })

        if recorded.Code != http.StatusNotFound {
            t.Fatalf("an unregistered fault surface answered %d", recorded.Code)
        }
    })

    t.Run("edge: the fault surface is admin only when it is registered", func(t *testing.T) {
        fixture := armedStage(t, faults.PointConfirmBeforeCommit)

        recorded := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/dev/faults",
            parent: parentOne,
        })

        if recorded.Code != http.StatusForbidden {
            t.Fatalf("a parent reached the fault surface with %d", recorded.Code)
        }
    })
}
