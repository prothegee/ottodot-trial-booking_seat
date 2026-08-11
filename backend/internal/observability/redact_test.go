package observability_test

import (
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/observability"
)

func TestRedact(t *testing.T) {
    t.Run("unit: the three headers named in the data rules lose their value", func(t *testing.T) {
        for _, field := range []string{"Cookie", "Set-Cookie", "Authorization"} {
            if !observability.SensitiveField(field) {
                t.Errorf("%s is not treated as sensitive, and it carries a whole session", field)
            }
        }
    })

    t.Run("edge: a field name is matched whatever it is punctuated as", func(t *testing.T) {
        // A redaction that can be stepped around by writing the name slightly
        // differently is not a redaction. All four of these are the same header.
        for _, spelling := range []string{"Set-Cookie", "set_cookie", "setcookie", "  SET-COOKIE  "} {
            if !observability.SensitiveField(spelling) {
                t.Errorf("%q was not recognised as the same field", spelling)
            }
        }
    })

    t.Run("unit: an ordinary field keeps its value", func(t *testing.T) {
        // Redaction has to leave the identifiers alone. A log with every value
        // replaced is as useless as no log at all, and identifiers are what the
        // data rules explicitly allow.
        bookingID := "0192a000-0000-7000-8000-000000000031"

        if got := observability.RedactValue("booking_id", bookingID); got != bookingID {
            t.Fatalf("the booking id came back as %q, and an id is what makes a log searchable", got)
        }
    })

    t.Run("edge: a header written inside prose is caught too", func(t *testing.T) {
        // This is the case the field list cannot catch. A wrapped driver error
        // can carry a whole request in its message, and by then it is one string
        // with no fields in it at all.
        line := observability.RedactText(`dial failed for GET /api/v1/classes, Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.abc`)

        if strings.Contains(line, "eyJhbGciOiJIUzI1NiJ9") {
            t.Fatalf("the token survived the scrub: %s", line)
        }

        if !strings.Contains(line, "Authorization:") {
            t.Fatalf("the header name should stay, so a reader can tell one was present: %s", line)
        }
    })

    t.Run("edge: an email address anywhere in a line is replaced", func(t *testing.T) {
        // An address is the one piece of personal data in this service with a
        // shape, so it is the one that can be caught after the fact. A name has
        // no shape, which is why the rule for names is that no log field exists
        // to put one in.
        line := observability.RedactText("no account matched parent.one@example.test on sign in")

        if strings.Contains(line, "@example.test") {
            t.Fatalf("the address survived the scrub: %s", line)
        }

        if !strings.Contains(line, "no account matched") {
            t.Fatalf("the rest of the line should be readable: %s", line)
        }
    })

    t.Run("edge: a value on a sensitive field is replaced whole, not pattern matched", func(t *testing.T) {
        // A cookie value has no shape at all, so nothing about it could be
        // matched. The field name is the only thing that identifies it, which is
        // why the field list exists alongside the patterns.
        if got := observability.RedactValue("cookie", "ottodot_access=abcdef123456; Path=/"); got != observability.Redacted {
            t.Fatalf("the cookie came back as %q", got)
        }
    })

    t.Run("unit: an empty string passes through untouched", func(t *testing.T) {
        if got := observability.RedactText(""); got != "" {
            t.Fatalf("an empty line came back as %q", got)
        }
    })
}
