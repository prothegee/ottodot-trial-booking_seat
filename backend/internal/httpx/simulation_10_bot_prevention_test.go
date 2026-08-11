package httpx_test

import (
    "context"
    "net/http"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/httpx"
)

// Simulation 10: bot prevention layers.
//
// The question this answers is not "is there a rate limiter". It is whether a
// booking request meets the layers in the right order, and whether each one
// answers with a refusal a client can act on rather than a generic failure.
//
// The order is what makes it worth testing as one run. Each layer is cheaper
// than the one below it, so a request that was never going to be honoured is
// turned away before it costs anything:
//
//	the token          no database read, a signature check
//	ownership          one directory read
//	the rate limit     one bucket, before any domain work
//	the bot signals    a string comparison and some arithmetic
//	the hold cap       inside the transaction, where it can be true
//	the duplicate      a unique index, where it cannot be raced
//
// The strongest property here is structural and is not a layer at all: a bot
// cannot occupy a seat without settling money, and uq_booking_active stops one
// child holding two live bookings for the same class. Everything below is about
// griefing and flooding.

// bookingsHeld counts what the repository is actually holding, which is how the
// flood case proves a refusal never reached storage.
func (fixture *stage) bookingsHeld(t *testing.T) int {
    t.Helper()

    listed, err := fixture.bookings.Worklist(context.Background(), booking.WorklistRequest{Limit: 200})
    if err != nil {
        t.Fatalf("cannot count bookings: %v", err)
    }

    return len(listed)
}

func TestSimulation10BotPreventionLayers(t *testing.T) {
    t.Run("layer 1: no token is refused before anything else happens", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings",
            body:           holdBody(studentOne, classOpen),
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusUnauthorized {
            t.Fatalf("an anonymous booking answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if fixture.bookingsHeld(t) != 0 {
            t.Fatal("an anonymous booking reached the repository")
        }
    })

    t.Run("layer 2: another parent's child is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings",
            body:           holdBody(studentOther, classOpen),
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusForbidden {
            t.Fatalf("booking another parent's child answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if failureOf(t, recorder).Error.Code != httpx.CodeNotYourChild {
            t.Fatalf("answered %q", failureOf(t, recorder).Error.Code)
        }

        if fixture.bookingsHeld(t) != 0 {
            t.Fatal("a request for another parent's child reached the repository")
        }
    })

    t.Run("layer 3: a flood empties the bucket without touching the repository", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        before := fixture.bookingsHeld(t)
        refused := false

        // The write bucket is 12 deep. Every one of these names a child the
        // caller does not own, so any that got past the limiter would be refused
        // by the ownership check instead, and neither writes anything.
        for attempt := 1; attempt <= 40; attempt++ {
            recorder := fixture.send(t, request{
                method:         http.MethodPost,
                path:           "/api/v1/bookings",
                body:           holdBody(studentOther, classOpen),
                parent:         parentOne,
                idempotencyKey: "11111111-1111-4111-8111-111111111111",
            })

            if recorder.Code == http.StatusTooManyRequests {
                refused = true

                body := failureOf(t, recorder)

                if body.Error.Code != httpx.CodeRateLimited {
                    t.Fatalf("the refusal answered %q", body.Error.Code)
                }

                if body.Error.RetryAfterSeconds < 1 {
                    t.Fatal("the refusal told the caller to wait zero seconds, which is a retry loop")
                }

                if recorder.Header().Get("Retry-After") == "" {
                    t.Fatal("the refusal carries no Retry-After header")
                }

                break
            }
        }

        if !refused {
            t.Fatal("forty writes in one instant were all allowed")
        }

        if fixture.bookingsHeld(t) != before {
            t.Fatal("the flood wrote a booking")
        }
    })

    t.Run("layer 4: a filled honeypot is refused, and told nothing about why", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/bookings",
            body: `{"student_id":"` + studentOne + `","class_id":"` + classOpen +
                `","website":"http://cheap-pills.example","filled_in_ms":4200,"captcha_token":"` + captcha.TokenPass + `"}`,
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("a filled honeypot answered %d: %s", recorder.Code, recorder.Body.String())
        }

        body := failureOf(t, recorder)

        if body.Error.Code != httpx.CodeInvalidRequest {
            t.Fatalf("answered %q, and a bot told which check caught it gets past that check next time", body.Error.Code)
        }

        if fixture.bookingsHeld(t) != 0 {
            t.Fatal("a filled honeypot reached the repository")
        }
    })

    t.Run("layer 4: a form submitted faster than a person can fill it is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/bookings",
            body: `{"student_id":"` + studentOne + `","class_id":"` + classOpen +
                `","website":"","filled_in_ms":120,"captcha_token":"` + captcha.TokenPass + `"}`,
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("a form filled in 120ms answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if fixture.bookingsHeld(t) != 0 {
            t.Fatal("an impossibly fast submission reached the repository")
        }
    })

    t.Run("layer 4: a refused challenge is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/bookings",
            body: `{"student_id":"` + studentOne + `","class_id":"` + classOpen +
                `","website":"","filled_in_ms":4200,"captcha_token":"` + captcha.TokenFail + `"}`,
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("a refused challenge answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if fixture.verifier.Refusals() != 1 {
            t.Fatalf("the verifier counted %d refusals", fixture.verifier.Refusals())
        }
    })

    t.Run("layer 4: an unreachable challenge provider is a pass, not a refusal", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/bookings",
            body: `{"student_id":"` + studentOne + `","class_id":"` + classOpen +
                `","website":"","filled_in_ms":4200,"captcha_token":"` + captcha.TokenUnavailable + `"}`,
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusCreated {
            t.Fatalf("a booking was refused because a third party was down: %d %s",
                recorder.Code, recorder.Body.String())
        }
    })

    t.Run("layer 5: a parent at the hold cap is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        // Three holds is the cap. The two children across the two classes are
        // what makes a fourth reachable at all.
        for _, held := range []struct {
            student string
            class   string
        }{
            {studentOne, classOpen},
            {studentOne, classOne},
            {studentTwo, classOpen},
        } {
            fixture.holdOne(t, held.student, held.class)
        }

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings",
            body:           holdBody(studentTwo, classOne),
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusConflict {
            t.Fatalf("a fourth hold answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if failureOf(t, recorder).Error.Code != httpx.CodeTooManyHolds {
            t.Fatalf("answered %q", failureOf(t, recorder).Error.Code)
        }
    })

    t.Run("layer 6: a second booking for the same child names the first one", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        first := fixture.holdOne(t, studentOne, classOpen)

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings",
            body:           holdBody(studentOne, classOpen),
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusConflict {
            t.Fatalf("a duplicate answered %d: %s", recorder.Code, recorder.Body.String())
        }

        body := failureOf(t, recorder)

        if body.Error.Code != httpx.CodeAlreadyBooked {
            t.Fatalf("answered %q", body.Error.Code)
        }

        if body.Error.BookingID != first.ID {
            t.Fatalf("the duplicate points at %q, the parent's booking is %s", body.Error.BookingID, first.ID)
        }

        if fixture.bookingsHeld(t) != 1 {
            t.Fatalf("%d bookings exist after a duplicate was refused", fixture.bookingsHeld(t))
        }
    })

    t.Run("layer 7: a request that passes every layer books a seat", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        granted := fixture.holdOne(t, studentOne, classOpen)

        if granted.Status != string(booking.StatusPendingPayment) {
            t.Fatalf("the hold reads %s", granted.Status)
        }

        if granted.HoldExpiresAt == nil {
            t.Fatal("the hold carries no deadline, so the payment screen has no countdown")
        }

        if granted.SeatNo != nil {
            t.Fatalf("a hold carries seat %d, and a hold is not a seat", *granted.SeatNo)
        }
    })

    t.Run("edge: a write with no idempotency key is refused before the child is even looked up", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodPost,
            path:   "/api/v1/bookings",
            body:   holdBody(studentOne, classOpen),
            parent: parentOne,
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("a write with no key answered %d: %s", recorder.Code, recorder.Body.String())
        }
    })

    t.Run("edge: a body carrying a field this api does not have is refused", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings",
            body:           `{"student_id":"` + studentOne + `","class_id":"` + classOpen + `","amount_cents":1}`,
            parent:         parentOne,
            idempotencyKey: "11111111-1111-4111-8111-111111111111",
        })

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("an unknown field answered %d: %s", recorder.Code, recorder.Body.String())
        }
    })
}
