package httpx_test

import (
    "encoding/base64"
    "encoding/json"
    "net/http"
    "regexp"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/httpx"
)

/*
Simulation 14: nothing sensitive leaks.

    drive a full booking, sign in to confirmed
    capture every output: the token payload, the log lines, the exposition,
    and every error body
    scan all four for an email, a name, or a child

Asserts: the seeded parent email, the parent name, and the child name appear in
none of the four outputs, cookie and authorization values are redacted in logs,
and no metric label holds a uuid.

The four outputs are chosen because they are the four ways something gets out of
this service without anybody deciding to send it. A response body is written on
purpose and is reviewed. A log line, a metric label, an error message, and a
token payload are all written as a side effect of something else, which is
exactly where a name ends up by accident.
*/

// personalData is everything seeded into this stage that must never appear in an
// output nobody chose to put it in.
//
// The child names are here as well as the parents', and that is the point of
// listing them by hand: a name has no shape, so nothing can pattern match one.
// The only way to check is to know what the names are.
var personalData = []string{
    "alice.tan@example.test",
    "budi.santoso@example.test",
    "ops.admin@example.test",
    "Alice Tan",
    "Budi Santoso",
    "Adi Tan",
    "Bella Tan",
    "Citra Santoso",
    "example.test",
}

// identifierPattern is what a leaked identifier looks like in a metric label.
var identifierPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// scanForPersonalData fails the test naming the surface and the value it carried.
func scanForPersonalData(t *testing.T, surface string, output string) {
    t.Helper()

    for _, leaked := range personalData {
        if strings.Contains(strings.ToLower(output), strings.ToLower(leaked)) {
            t.Errorf("%s carries %q, which is personal data on a surface nobody chose to put it on", surface, leaked)
        }
    }
}

// tokenPayload decodes the middle segment of a signed token.
//
// A JWT payload is base64 and not encryption. Anybody holding the token can read
// it, including the browser it was sent to, which is why the claim set is the
// first thing this case looks at.
func tokenPayload(t *testing.T, token string) string {
    t.Helper()

    segments := strings.Split(token, ".")
    if len(segments) != 3 {
        t.Fatalf("the token has %d segments", len(segments))
    }

    decoded, err := base64.RawURLEncoding.DecodeString(segments[1])
    if err != nil {
        t.Fatalf("the payload is not base64: %v", err)
    }

    return string(decoded)
}

func TestSimulation14NothingSensitiveLeaks(t *testing.T) {
    t.Run("integration: a full booking leaves nothing personal on any of the four surfaces", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        // A whole booking, sign in to confirmed, so every surface has had
        // something happen on it.
        held := fixture.holdOne(t, studentOne, classOpen)

        paid := fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f000-0000-7000-8000-00000000f001",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        if paid.Code != http.StatusOK {
            t.Fatalf("the payment answered %d: %s", paid.Code, paid.Body.String())
        }

        // Surface one: the access token payload.
        scanForPersonalData(t, "the access token payload",
            tokenPayload(t, fixture.tokenFor(t, parentOne, "parent")))

        // Surface two: everything written to the log.
        scanForPersonalData(t, "the log", fixture.logged.String())

        // Surface three: the exposition.
        scanForPersonalData(t, "the exposition", fixture.exposition(t))

        // Surface four: the error bodies. Each of these is a different refusal
        // path, and every one of them is a chance to name somebody.
        for _, refusal := range []request{
            {method: http.MethodGet, path: "/api/v1/bookings/" + held.ID, parent: parentTwo},
            {method: http.MethodGet, path: "/api/v1/classes/" + classOpen + "/roster", parent: parentOne},
            {method: http.MethodGet, path: "/api/v1/admin/bookings", parent: parentOne},
            {method: http.MethodPost, path: "/api/v1/bookings", parent: parentOne,
                idempotencyKey: "0192f000-0000-7000-8000-00000000f002",
                body:           `{"student_id":"` + studentOther + `","class_id":"` + classOpen + `"}`},
            {method: http.MethodGet, path: "/api/v1/bookings/00000000-0000-7000-8000-000000000000", parent: parentOne},
        } {
            recorded := fixture.send(t, refusal)

            if recorded.Code < 400 {
                t.Fatalf("%s %s was expected to be refused, got %d", refusal.method, refusal.path, recorded.Code)
            }

            scanForPersonalData(t, "the body of "+refusal.method+" "+refusal.path, recorded.Body.String())
        }
    })

    t.Run("edge: no label on the exposition holds an identifier", func(t *testing.T) {
        // A metric label carrying a uuid is one time series per booking. It is a
        // leak of who booked what to anybody who can reach the monitoring
        // system, and it is a way to run that system out of memory on the way.
        fixture := newStage(t, stageOptions{})

        held := fixture.holdOne(t, studentOne, classOpen)

        fixture.send(t, request{
            method:         http.MethodPost,
            path:           "/api/v1/bookings/" + held.ID + "/payments",
            parent:         parentOne,
            idempotencyKey: "0192f000-0000-7000-8000-00000000f003",
            body:           `{"amount_cents":4500,"currency":"SGD","filled_in_ms":4000}`,
        })

        fixture.send(t, request{method: http.MethodGet, path: "/api/v1/bookings/" + held.ID, parent: parentTwo})

        published := fixture.exposition(t)

        for _, line := range strings.Split(published, "\n") {
            if strings.HasPrefix(line, "#") {
                continue
            }

            if found := identifierPattern.FindString(line); found != "" {
                t.Fatalf("the series %q carries the identifier %q", line, found)
            }
        }
    })

    t.Run("edge: a cookie and an authorization header are redacted in the log", func(t *testing.T) {
        // Redaction happens at the writer, so this drives the failing path that
        // actually writes a line and then checks that the request's own headers
        // did not travel with it.
        fixture := newStage(t, stageOptions{})

        fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/bookings/00000000-0000-7000-8000-000000000000",
            parent: parentOne,
        })

        written := fixture.logged.String()

        for _, header := range []string{"ottodot_access=", "Bearer "} {
            if strings.Contains(written, header) {
                t.Fatalf("the log carries %q, which is a whole session", header)
            }
        }
    })

    t.Run("edge: an internal error tells the client a code and a request id and nothing else", func(t *testing.T) {
        // The one code a client cannot act on. The id is the whole of what it is
        // told, and the detail belongs in the log line that id leads to.
        fixture := newStage(t, stageOptions{})

        fixture.catalogue.FailNext()

        recorded := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/classes", parent: parentOne})

        if recorded.Code != http.StatusInternalServerError {
            t.Fatalf("the broken read answered %d: %s", recorded.Code, recorded.Body.String())
        }

        var envelope struct {
            Error map[string]any `json:"error"`
        }

        if err := json.NewDecoder(recorded.Body).Decode(&envelope); err != nil {
            t.Fatalf("the body is not readable: %v", err)
        }

        if envelope.Error["code"] != httpx.CodeInternalError {
            t.Fatalf("the code is %v", envelope.Error["code"])
        }

        if envelope.Error["request_id"] == "" || envelope.Error["request_id"] == nil {
            t.Fatal("the body carries no request id, so the log line it should lead to cannot be found")
        }

        for field := range envelope.Error {
            switch field {
            case "code", "message", "request_id":
            default:
                t.Errorf("the body carries an extra field %q on the one refusal that must say the least", field)
            }
        }
    })
}
