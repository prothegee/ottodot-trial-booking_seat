package httpx_test

import (
    "net/http"
    "testing"

    "ottodot-trial-booking/backend/internal/catalogue"
)

// Test 11: a conditional request served without a database read.
//
// The goal is exact and it is measured rather than asserted: when the class list
// has not changed, this service does no work. A repeat request costs one store
// read and zero database queries, and the reader counts its own calls so the
// second half of that sentence is provable rather than plausible.
//
// The other half of the design is the version counter. A cached body alone would
// let a client hold a tag that never changes, so every mutation bumps a counter
// and the tag is built from it. That is what makes an invalidation visible to a
// client that is holding an identical body.

func TestSimulation11ConditionalRequestServedWithoutADatabaseRead(t *testing.T) {
    fixture := newStage(t, stageOptions{})

    // The first read of the class list. Nothing is cached, so the reader is
    // asked and the answer is published with a tag.
    first := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

    if first.Code != http.StatusOK {
        t.Fatalf("the first read answered %d: %s", first.Code, first.Body.String())
    }

    tag := first.Header().Get("ETag")

    if tag == "" {
        t.Fatal("the first read carries no ETag, so no client can ever revalidate")
    }

    if first.Header().Get("Cache-Control") == "no-store" {
        t.Fatal("the class list is marked no-store, which makes the whole mechanism pointless")
    }

    readsAfterFirst := fixture.catalogue.Reads()

    if readsAfterFirst != 1 {
        t.Fatalf("the first read cost %d database reads, wanted 1", readsAfterFirst)
    }

    t.Run("stage 1: a client holding the tag is answered 304 with no body", func(t *testing.T) {
        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/classes", parent: parentOne, ifNoneMatch: tag,
        })

        if recorder.Code != http.StatusNotModified {
            t.Fatalf("a matching tag answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if recorder.Body.Len() != 0 {
            t.Fatalf("a 304 carried %d bytes of body", recorder.Body.Len())
        }

        if recorder.Header().Get("ETag") != tag {
            t.Fatalf("the 304 answered with tag %q, so the client cannot revalidate again",
                recorder.Header().Get("ETag"))
        }

        // The assertion the whole test exists for.
        if fixture.catalogue.Reads() != readsAfterFirst {
            t.Fatalf("a conditional request cost %d database reads",
                fixture.catalogue.Reads()-readsAfterFirst)
        }
    })

    t.Run("stage 2: a client with no tag is served the stored body, still without a database read", func(t *testing.T) {
        recorder := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

        if recorder.Code != http.StatusOK {
            t.Fatalf("a fresh client answered %d", recorder.Code)
        }

        if recorder.Header().Get("ETag") != tag {
            t.Fatalf("the stored body was republished with tag %q rather than %q",
                recorder.Header().Get("ETag"), tag)
        }

        if fixture.catalogue.Reads() != readsAfterFirst {
            t.Fatalf("serving a stored body cost %d database reads",
                fixture.catalogue.Reads()-readsAfterFirst)
        }
    })

    t.Run("stage 3: a booking invalidates the list, and the next read gets a new tag", func(t *testing.T) {
        // A hold changes what every other parent should see, so the write path
        // invalidates both cached documents on its way out.
        fixture.holdOne(t, studentOne, classOpen)

        // The catalogue is a fake and does not recompute itself, so the seat
        // count is moved here to stand in for what the replica would report.
        fixture.catalogue.AddClass(seatsRemainingIn(t, fixture, classOpen, 3))

        recorder := fixture.send(t, request{
            method: http.MethodGet, path: "/api/v1/classes", parent: parentOne, ifNoneMatch: tag,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("a stale tag answered %d, so the parent would keep the old seat count", recorder.Code)
        }

        if recorder.Header().Get("ETag") == tag {
            t.Fatal("the tag did not change after an invalidation")
        }

        if fixture.catalogue.Reads() <= readsAfterFirst {
            t.Fatal("the list was rebuilt without reading the database, which means it was not rebuilt")
        }
    })

    t.Run("stage 4: an invalidation changes the tag even when the body is identical", func(t *testing.T) {
        fresh := newStage(t, stageOptions{})

        before := fresh.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})
        firstTag := before.Header().Get("ETag")

        // A cancel invalidates, and it puts the seat straight back, so the body
        // the next read produces is byte for byte what it was. Only the version
        // counter tells them apart, which is exactly why the tag carries one.
        granted := fresh.holdOne(t, studentOne, classOpen)

        cancelled := fresh.send(t, request{
            method: http.MethodDelete,
            path:   "/api/v1/bookings/" + granted.ID,
            parent: parentOne,
        })

        if cancelled.Code != http.StatusOK {
            t.Fatalf("the cancel answered %d: %s", cancelled.Code, cancelled.Body.String())
        }

        after := fresh.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

        if after.Body.String() != before.Body.String() {
            t.Fatal("the bodies differ, so this case is not testing what it says it is")
        }

        if after.Header().Get("ETag") == firstTag {
            t.Fatal("an identical body reused its tag, so a client could hold it past an invalidation")
        }
    })

    t.Run("edge: one class is cached apart from the list", func(t *testing.T) {
        fresh := newStage(t, stageOptions{})

        listTag := fresh.send(t, request{
            method: http.MethodGet, path: "/api/v1/classes", parent: parentOne,
        }).Header().Get("ETag")

        classTag := fresh.send(t, request{
            method: http.MethodGet, path: "/api/v1/classes/" + classOpen, parent: parentOne,
        }).Header().Get("ETag")

        if listTag == "" || classTag == "" {
            t.Fatalf("a cacheable read carries no tag: list %q, class %q", listTag, classTag)
        }

        if listTag == classTag {
            t.Fatal("the list and one class share a tag, so one would be served for the other")
        }
    })

    t.Run("edge: a wildcard is honoured, because it means any representation", func(t *testing.T) {
        fresh := newStage(t, stageOptions{})

        fresh.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

        recorder := fresh.send(t, request{
            method: http.MethodGet, path: "/api/v1/classes", parent: parentOne, ifNoneMatch: "*",
        })

        if recorder.Code != http.StatusNotModified {
            t.Fatalf("a wildcard answered %d", recorder.Code)
        }
    })

    t.Run("edge: nothing that decides anything is cacheable", func(t *testing.T) {
        fresh := newStage(t, stageOptions{})

        granted := fresh.holdOne(t, studentOne, classOpen)

        for _, path := range []string{
            "/api/v1/students",
            "/api/v1/bookings/" + granted.ID,
            "/api/v1/bookings/" + granted.ID + "/events",
        } {
            recorder := fresh.send(t, request{method: http.MethodGet, path: path, parent: parentOne})

            if recorder.Header().Get("ETag") != "" {
                t.Fatalf("%s carries an ETag, and nothing on a deciding path may be revalidated", path)
            }
        }
    })
}

// seatsRemainingIn rebuilds one class with a different seat count.
//
// The fake catalogue is a map rather than a projection, so a test that changes a
// booking has to move the number itself. Doing it in one helper keeps the reason
// visible: the real reader recomputes this from the bookings table.
func seatsRemainingIn(t *testing.T, fixture *stage, classID string, remaining int16) catalogue.Class {
    t.Helper()

    listed, err := fixture.catalogue.Class(t.Context(), classID)
    if err != nil {
        t.Fatalf("cannot read the class: %v", err)
    }

    listed.SeatsRemaining = remaining

    return listed
}
