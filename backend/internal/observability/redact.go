package observability

import (
    "regexp"
    "strings"
)

// Redacted is what a scrubbed value is replaced with.
//
// The value is replaced rather than the whole field being dropped, so a reader
// can still see that a header was present. "there was no Authorization header"
// and "there was one and it is not shown" are different facts, and losing the
// difference makes an auth problem harder to read, not safer.
const Redacted = "[redacted]"

// The field names whose value never reaches a log, whatever it holds.
//
// The list is matched without case and without punctuation, so `Set-Cookie`,
// `set_cookie`, and `setcookie` are all the same field. A redaction that can be
// stepped around by writing the name slightly differently is not a redaction.
var sensitiveFields = map[string]bool{
    "cookie":        true,
    "setcookie":     true,
    "authorization": true,
    "token":         true,
    "accesstoken":   true,
    "refreshtoken":  true,
    "password":      true,
    "secret":        true,
    "email":         true,
    "name":          true,
    "fullname":      true,
    "parentname":    true,
    "studentname":   true,
}

// headerInText finds a sensitive header written out inside ordinary prose.
//
// This is the case the field list cannot catch. A driver error or a wrapped
// failure can carry a whole request in its message, and by the time it reaches
// the logger it is one string with no fields in it at all.
//
// The value runs to the end of the line or to the next comma or semicolon, which
// is deliberately greedy. A header value contains spaces (`Bearer <token>` is
// two words) so stopping at the first one would replace the scheme and leave the
// token, which is the half that matters.
var headerInText = regexp.MustCompile(`(?i)\b(set-cookie|cookie|authorization)\b\s*[:=]\s*[^,;\n]*`)

// addressInText finds an email address anywhere in a line.
//
// Emails are the one piece of personal data in this service that has a shape, so
// they are the one that can be caught after the fact. Names have no shape, which
// is why the rule for those is that they are never handed to the logger in the
// first place, and why the leak test checks for the seeded names by hand.
var addressInText = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// SensitiveField reports whether a field's value must be replaced.
//
// Param:
// name - string (the field name, in any casing and with any punctuation)
//
// Return:
//   - true when the value must never be written
func SensitiveField(name string) bool {
    return sensitiveFields[normaliseFieldName(name)]
}

// RedactValue is what may be written for one field.
//
// Param:
// name - string (the field name)
// value - string (what the call site wanted to write)
//
// Return:
//   - Redacted when the field is on the list
//   - the value with any header or address inside it scrubbed otherwise
func RedactValue(name string, value string) string {
    if SensitiveField(name) {
        return Redacted
    }

    return RedactText(value)
}

// RedactText scrubs a free text string.
//
// It runs on every message and every value that is not already replaced whole,
// which is what "redaction at the writer" means: no call site can forget it,
// because no call site performs it.
//
// Param:
// text - string (anything on its way into a log line)
//
// Return:
//   - the same text with sensitive headers and email addresses replaced
func RedactText(text string) string {
    if text == "" {
        return text
    }

    scrubbed := headerInText.ReplaceAllStringFunc(text, func(match string) string {
        separator := strings.IndexAny(match, ":=")
        if separator < 0 {
            return Redacted
        }

        return match[:separator+1] + " " + Redacted
    })

    return addressInText.ReplaceAllString(scrubbed, Redacted)
}

// normaliseFieldName strips the punctuation and casing a field name can vary in.
func normaliseFieldName(name string) string {
    lowered := strings.ToLower(strings.TrimSpace(name))

    replacer := strings.NewReplacer("-", "", "_", "", ".", "", " ", "")

    return replacer.Replace(lowered)
}
