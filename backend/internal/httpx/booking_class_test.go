package httpx_test

import (
    "net/http"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/httpx"
)

// readOne reads one booking back through the api.
func (fixture *stage) readOne(t *testing.T, bookingID string) bookingWire {
    t.Helper()

    recorder := fixture.send(t, request{
        method: http.MethodGet, path: "/api/v1/bookings/" + bookingID, parent: parentOne,
    })

    if recorder.Code != http.StatusOK {
        t.Fatalf("reading a booking answered %d", recorder.Code)
    }

    var read bookingWire

    decodeInto(t, recorder, &read)

    return read
}

func TestABookingNamesTheClassItIsFor(t *testing.T) {
    t.Run("integration: a hold carries the subject, the title, and when it starts", func(t *testing.T) {
        // Without these a parent's own list is a column of references and seat
        // numbers, with no way to tell one booking from another.
        fixture := newStage(t, stageOptions{})

        granted := fixture.holdOne(t, studentOne, classOpen)

        if granted.ClassSubject != "science" || granted.ClassTitle != "Science trial" {
            t.Fatalf("the hold names %q / %q", granted.ClassSubject, granted.ClassTitle)
        }

        if granted.ClassStartsAt == nil {
            t.Fatal("the hold says nothing about when the class starts")
        }
    })

    t.Run("integration: reading one booking names the same class", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        granted := fixture.holdOne(t, studentOne, classOpen)
        read := fixture.readOne(t, granted.ID)

        if read.ClassSubject != granted.ClassSubject || read.ClassTitle != granted.ClassTitle {
            t.Fatalf("the read names %q / %q and the hold named %q / %q",
                read.ClassSubject, read.ClassTitle, granted.ClassSubject, granted.ClassTitle)
        }
    })

    t.Run("integration: a settled payment answers with the class named", func(t *testing.T) {
        // The payment screen navigates on this answer, and a booking that
        // arrived unnamed would flash a blank heading before the next read.
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        status, confirmed := fixture.payOnce(t, granted.ID, checkout.TrialPriceCents,
            "33333333-3333-4333-8333-333333333333")

        if status != http.StatusOK {
            t.Fatalf("a settled charge answered %d", status)
        }

        if confirmed.ClassTitle != "Science trial" {
            t.Fatalf("the confirmed booking names %q", confirmed.ClassTitle)
        }
    })

    t.Run("behaviour: every booking in the list names its own class", func(t *testing.T) {
        // Two bookings on two different classes. One read of the catalogue
        // answers both, and the point of the test is that the names are not
        // swapped or copied from the first row.
        fixture := newStage(t, stageOptions{})

        fixture.holdOne(t, studentOne, classOpen)
        fixture.holdOne(t, studentTwo, classOne)

        named := make(map[string]string)

        listed, status := fixture.listFor(t, parentOne)
        if status != http.StatusOK {
            t.Fatalf("the parent's own list answered %d", status)
        }

        for _, one := range listed.Bookings {
            named[one.ClassID] = one.ClassTitle
        }

        if named[classOpen] != "Science trial" {
            t.Fatalf("the science booking names %q", named[classOpen])
        }

        if named[classOne] != "Math trial" {
            t.Fatalf("the math booking names %q", named[classOne])
        }
    })

    t.Run("behaviour: a cancelled booking still says what it was for", func(t *testing.T) {
        // A parent looking at a list of finished bookings needs the name more
        // than one looking at a single live booking does.
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method: http.MethodDelete, path: "/api/v1/bookings/" + granted.ID, parent: parentOne,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("cancelling answered %d", recorder.Code)
        }

        var withdrawn bookingWire

        decodeInto(t, recorder, &withdrawn)

        if withdrawn.ClassTitle != "Science trial" {
            t.Fatalf("the cancelled booking names %q", withdrawn.ClassTitle)
        }
    })

    t.Run("edge: a catalogue that cannot be read still answers the booking", func(t *testing.T) {
        // The class description decides nothing. Refusing the whole read
        // because it was unavailable would hide a confirmed seat behind a
        // lookup that only puts a heading on a card.
        fixture := newStage(t, stageOptions{})
        granted := fixture.holdOne(t, studentOne, classOpen)

        fixture.catalogue.FailNext()

        read := fixture.readOne(t, granted.ID)

        if read.ID != granted.ID {
            t.Fatalf("the booking read back as %q", read.ID)
        }

        if read.ClassTitle != "" || read.ClassStartsAt != nil {
            t.Fatalf("an unreadable catalogue still produced %q", read.ClassTitle)
        }
    })

    t.Run("edge: the class carries no seat count, which moves after the booking is made", func(t *testing.T) {
        // A booking is a record of what happened. A count on it would be a
        // second place to read one from, and the two would disagree the moment
        // somebody else books.
        fixture := newStage(t, stageOptions{})

        granted := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/bookings/" + granted.ID, parent: parentOne,
        })

        for _, absent := range []string{"seats_remaining", "capacity", "duration_minutes"} {
            if strings.Contains(recorder.Body.String(), absent) {
                t.Fatalf("the booking body carries %q: %s", absent, recorder.Body.String())
            }
        }
    })

    t.Run("edge: a resolver with nothing to read from is refused at construction", func(t *testing.T) {
        if _, err := httpx.NewClassNames(nil); err == nil {
            t.Fatal("a resolver with no catalogue was accepted, and would answer with a nil dereference")
        }
    })
}
