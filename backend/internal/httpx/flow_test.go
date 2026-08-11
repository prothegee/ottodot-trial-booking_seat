package httpx_test

import (
    "net/http"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/payment"
)

// payOnce settles one booking through the api.
func (fixture *stage) payOnce(t *testing.T, bookingID string, cents int, key string) (int, bookingWire) {
    t.Helper()

    recorder := fixture.send(t, request{
        method:         http.MethodPost,
        path:           "/api/v1/bookings/" + bookingID + "/payments",
        body:           payBody(cents),
        parent:         parentOne,
        idempotencyKey: key,
    })

    var settled bookingWire

    if recorder.Code == http.StatusOK {
        decodeInto(t, recorder, &settled)
    }

    return recorder.Code, settled
}

func TestPayingThroughTheApi(t *testing.T) {
    t.Run("integration: a settled charge confirms the booking and names the seat", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        status, confirmed := fixture.payOnce(t, granted.ID, checkout.TrialPriceCents,
            "22222222-2222-4222-8222-222222222222")

        if status != http.StatusOK {
            t.Fatalf("a settled charge answered %d", status)
        }

        if confirmed.Status != string(booking.StatusConfirmed) {
            t.Fatalf("the booking reads %s", confirmed.Status)
        }

        if confirmed.SeatNo == nil || *confirmed.SeatNo != 1 {
            t.Fatalf("the booking carries seat %v, wanted the lowest free one", confirmed.SeatNo)
        }

        if confirmed.HoldExpiresAt != nil {
            t.Fatal("a confirmed booking still carries a hold deadline")
        }
    })

    t.Run("behaviour: a declined charge answers 402 and ends the booking", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        if err := fixture.provider.ForceOutcome(granted.ID, payment.OutcomeDeclined); err != nil {
            t.Fatalf("cannot pin the outcome: %v", err)
        }

        status, _ := fixture.payOnce(t, granted.ID, checkout.TrialPriceCents,
            "22222222-2222-4222-8222-222222222222")

        if status != http.StatusPaymentRequired {
            t.Fatalf("a decline answered %d, wanted 402", status)
        }

        read := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/bookings/" + granted.ID, parent: parentOne,
        })

        var after bookingWire

        decodeInto(t, read, &after)

        if after.Status != string(booking.StatusPaymentFailed) {
            t.Fatalf("the booking reads %s after a decline", after.Status)
        }
    })

    t.Run("behaviour: money that settled with no seat left answers 409 seat_lost", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        winner := fixture.holdOne(t, studentOne, classOne)
        loser := fixture.holdOne(t, studentTwo, classOne)

        if status, _ := fixture.payOnce(t, winner.ID, checkout.TrialPriceCents,
            "22222222-2222-4222-8222-222222222222"); status != http.StatusOK {
            t.Fatalf("the winning payment answered %d", status)
        }

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + loser.ID + "/payments",
            body:           payBody(checkout.TrialPriceCents),
            parent:         parentOne,
            idempotencyKey: "33333333-3333-4333-8333-333333333333",
        })

        if recorder.Code != http.StatusConflict {
            t.Fatalf("a lost seat answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if failureOf(t, recorder).Error.Code != httpx.CodeSeatLost {
            t.Fatalf("answered %q", failureOf(t, recorder).Error.Code)
        }
    })

    t.Run("behaviour: the demonstration amount declines locally and is refused otherwise", func(t *testing.T) {
        local := newStage(t, stageOptions{Development: true})
        granted := local.holdOne(t, studentOne, classOpen)

        if status, _ := local.payOnce(t, granted.ID, checkout.TrialPriceCents+1,
            "22222222-2222-4222-8222-222222222222"); status != http.StatusPaymentRequired {
            t.Fatalf("the demonstration decline answered %d in development", status)
        }

        promoted := newStage(t, stageOptions{})
        other := promoted.holdOne(t, studentOne, classOpen)

        if status, _ := promoted.payOnce(t, other.ID, checkout.TrialPriceCents+1,
            "22222222-2222-4222-8222-222222222222"); status != http.StatusBadRequest {
            t.Fatalf("the demonstration amount answered %d outside development", status)
        }
    })

    t.Run("edge: an amount this service does not charge never reaches the provider", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        status, _ := fixture.payOnce(t, granted.ID, 1, "22222222-2222-4222-8222-222222222222")

        if status != http.StatusBadRequest {
            t.Fatalf("paying one cent answered %d", status)
        }

        if fixture.provider.Charges() != 0 {
            t.Fatalf("%d charges were made for an amount this service does not accept", fixture.provider.Charges())
        }
    })

    t.Run("edge: another parent's booking cannot be paid for", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + granted.ID + "/payments",
            body:           payBody(checkout.TrialPriceCents),
            parent:         parentTwo,
            idempotencyKey: "44444444-4444-4444-8444-444444444444",
        })

        if recorder.Code != http.StatusForbidden {
            t.Fatalf("paying somebody else's booking answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if fixture.provider.Charges() != 0 {
            t.Fatal("a charge was attempted for a booking the caller does not own")
        }
    })
}

func TestReadingThroughTheApi(t *testing.T) {
    t.Run("integration: the students route lists only the caller's children", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/students", parent: parentOne})

        if recorder.Code != http.StatusOK {
            t.Fatalf("the students route answered %d", recorder.Code)
        }

        body := recorder.Body.String()

        if !strings.Contains(body, studentOne) || !strings.Contains(body, studentTwo) {
            t.Fatalf("the caller's own children are missing: %s", body)
        }

        if strings.Contains(body, studentOther) {
            t.Fatalf("another parent's child is listed: %s", body)
        }
    })

    t.Run("integration: a booking's audit trail reads back", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/bookings/" + granted.ID + "/events", parent: parentOne,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("the trail answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if !strings.Contains(recorder.Body.String(), "pending_payment") {
            t.Fatalf("the trail does not record the hold: %s", recorder.Body.String())
        }
    })

    t.Run("edge: another parent's booking cannot be read", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/bookings/" + granted.ID, parent: parentTwo,
        })

        if recorder.Code != http.StatusForbidden {
            t.Fatalf("reading somebody else's booking answered %d", recorder.Code)
        }
    })

    t.Run("edge: a booking nobody has answers the same as one that is not yours", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/bookings/99999999-9999-7999-8999-999999999999",
            parent: parentOne,
        })

        if failureOf(t, recorder).Error.Code != httpx.CodeInvalidRequest {
            t.Fatalf("an unknown booking answered %q, which tells a caller which ids exist",
                failureOf(t, recorder).Error.Code)
        }
    })
}

func TestTheOperatorRoutes(t *testing.T) {
    t.Run("integration: an admin reads the queue depth", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        fixture.holdOne(t, studentOne, classOpen)

        // The release job is scheduled past the hold deadline, so the clock is
        // moved past it. Before that instant the job is neither due, nor held,
        // nor parked, which is the honest reading of a queue depth: it counts
        // what a worker could act on now.
        fixture.now = stageMoment.Add(time.Hour)

        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/admin/queue", parent: adminParent, role: auth.RoleAdmin,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("the queue route answered %d: %s", recorder.Code, recorder.Body.String())
        }

        var depth struct {
            Ready   int `json:"ready"`
            Claimed int `json:"claimed"`
            Parked  int `json:"parked"`
        }

        decodeInto(t, recorder, &depth)

        // The hold scheduled its own release, so there is exactly one job and it
        // is not due yet.
        if depth.Ready+depth.Claimed+depth.Parked != 1 {
            t.Fatalf("the queue reports %+v after one hold", depth)
        }
    })

    t.Run("integration: the worklist narrows to one status", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/admin/bookings?status=pending_payment",
            parent: adminParent,
            role:   auth.RoleAdmin,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("the worklist answered %d: %s", recorder.Code, recorder.Body.String())
        }

        var listed struct {
            Bookings []bookingWire `json:"bookings"`
        }

        decodeInto(t, recorder, &listed)

        if len(listed.Bookings) != 1 {
            t.Fatalf("%d bookings were listed after one hold", len(listed.Bookings))
        }
    })

    t.Run("edge: a status this service never had is refused rather than answered empty", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/admin/bookings?status=refunded",
            parent: adminParent,
            role:   auth.RoleAdmin,
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("an unknown status answered %d, and an empty list reads as nothing to do", recorder.Code)
        }
    })

    t.Run("edge: a page size above the maximum is refused rather than quietly lowered", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/admin/bookings?limit=100000",
            parent: adminParent,
            role:   auth.RoleAdmin,
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("an oversized page answered %d, and a silently shortened list reads as the whole answer",
                recorder.Code)
        }
    })
}
