package httpx_test

import (
    "net/http"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/httpx"
)

// parentBookingsPath is the address without the method, which is what a request
// in this file asks for.
var parentBookingsPath = httpx.ParentBookingsPath[len("GET "):]

// listFor reads one parent's own bookings through the api.
func (fixture *stage) listFor(t *testing.T, parentID string) (*bookingListWire, int) {
    t.Helper()

    recorder := fixture.send(t, request{
        method: http.MethodGet,
        path:   parentBookingsPath,
        parent: parentID,
    })

    if recorder.Code != http.StatusOK {
        return nil, recorder.Code
    }

    var listed bookingListWire

    decodeInto(t, recorder, &listed)

    return &listed, recorder.Code
}

// bookingListWire is the parent's own list as the client reads it.
type bookingListWire struct {
    Bookings []bookingWire `json:"bookings"`
}

func TestAParentCanFindTheirOwnBookingsAgain(t *testing.T) {
    t.Run("integration: the list answers what this parent booked", func(t *testing.T) {
        // The screen that made a booking is gone and the address it lived at
        // went with it. This route is the only way back to it.
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        listed, status := fixture.listFor(t, parentOne)
        if status != http.StatusOK {
            t.Fatalf("the list answered %d", status)
        }

        if len(listed.Bookings) != 1 {
            t.Fatalf("%d bookings were listed after one hold", len(listed.Bookings))
        }

        if listed.Bookings[0].ID != granted.ID {
            t.Fatalf("the list names %s, wanted %s", listed.Bookings[0].ID, granted.ID)
        }

        if listed.Bookings[0].Status != string(booking.StatusPendingPayment) {
            t.Fatalf("the booking reads %s, so the screen cannot tell a parent it is waiting for payment",
                listed.Bookings[0].Status)
        }
    })

    t.Run("behaviour: another parent's booking is never in the list", func(t *testing.T) {
        // The scoping is the whole of the authorisation on this route. There is
        // no identifier in the request to check, so this case is what proves the
        // query is the check.
        fixture := newStage(t, stageOptions{})
        fixture.holdOne(t, studentOne, classOpen)

        theirs := fixture.send(t, request{
            method:         http.MethodPost,
            path:           parentBookingsPath,
            body:           holdBody(studentOther, classOpen),
            parent:         parentTwo,
            idempotencyKey: "33333333-3333-4333-8333-333333333333",
        })

        if theirs.Code != http.StatusCreated {
            t.Fatalf("the second parent's hold answered %d: %s", theirs.Code, theirs.Body.String())
        }

        listed, status := fixture.listFor(t, parentOne)
        if status != http.StatusOK {
            t.Fatalf("the list answered %d", status)
        }

        if len(listed.Bookings) != 1 {
            t.Fatalf("%d bookings were listed, wanted only this parent's one", len(listed.Bookings))
        }

        if listed.Bookings[0].StudentID != studentOne {
            t.Fatalf("the list names child %s, who is not on this account", listed.Bookings[0].StudentID)
        }
    })

    t.Run("behaviour: a finished booking is still listed", func(t *testing.T) {
        // A parent opens this to find out what happened, and what happened is
        // often that it did not go through. Listing only live bookings would
        // hide exactly the one they came to look at.
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        withdrawn := fixture.send(t, request{
            method: http.MethodDelete,
            path:   "/api/v1/bookings/" + granted.ID,
            parent: parentOne,
        })

        if withdrawn.Code != http.StatusOK {
            t.Fatalf("the cancellation answered %d", withdrawn.Code)
        }

        listed, status := fixture.listFor(t, parentOne)
        if status != http.StatusOK {
            t.Fatalf("the list answered %d", status)
        }

        if len(listed.Bookings) != 1 || listed.Bookings[0].Status != string(booking.StatusCancelled) {
            t.Fatalf("a cancelled booking is not in the list: %+v", listed.Bookings)
        }
    })

    t.Run("edge: a parent who has booked nothing gets an empty list, not null", func(t *testing.T) {
        // A screen that had to guard against null before it could render an
        // empty state is a screen that renders nothing the first time somebody
        // signs in.
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   parentBookingsPath,
            parent: parentOne,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("a parent with no bookings answered %d", recorder.Code)
        }

        if !strings.Contains(recorder.Body.String(), `"bookings":[]`) {
            t.Fatalf("an empty list reads as %s", recorder.Body.String())
        }
    })

    t.Run("edge: nobody signed in is refused rather than answered", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})
        fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{method: http.MethodGet, path: parentBookingsPath})

        if recorder.Code != http.StatusUnauthorized {
            t.Fatalf("an anonymous list answered %d", recorder.Code)
        }
    })

    t.Run("unit: the list is never cached", func(t *testing.T) {
        // It carries the same statuses the single booking read does, and a
        // stale pending_payment after a card cleared looks exactly like the
        // payment was lost.
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   parentBookingsPath,
            parent: parentOne,
        })

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("the list is served as %q", recorder.Header().Get("Cache-Control"))
        }
    })
}
