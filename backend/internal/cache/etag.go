// Package cache turns a repeated read into a cheap one, and never into a wrong
// one.
//
// Only two things in this service are cacheable, the class list and one class,
// and both are advisory by construction: the number of seats on a screen is a
// hint that saves a parent a wasted click, and the only authority on a seat is
// the confirm transaction. That is what makes caching safe here. Nothing that
// decides anything is ever stored.
//
// The package is split so the tag can be read without a store in front of it.
// Building and comparing a tag is arithmetic over bytes, in this file. The store
// is the only part that reaches out of the process, and it has two
// implementations behind one interface so the same suite runs against both.
package cache

import (
    "crypto/sha256"
    "encoding/hex"
    "strconv"
    "strings"
)

// digestLength is how much of the payload hash goes into the tag.
//
// Sixteen hex characters is 64 bits. A tag is compared against one other tag for
// one key, never searched, so this is about accidental collision between two
// versions of the same document, not about anyone forging one.
const digestLength = 16

// weakPrefix marks a validator that only promises the two bodies are equivalent
// rather than identical. Nothing here ever writes one, and comparison strips it
// so a proxy that added one does not turn every request into a miss.
const weakPrefix = "W/"

// BuildETag turns a version and a body into the validator a client sends back.
//
// The shape is `"<version>-<digest>"`, and it carries both halves on purpose.
// The digest alone would repeat a tag when a body changes back to something it
// held before. The version alone would repeat a tag if a counter were ever reset
// or rolled over. Together, a repeat needs both to collide at once.
//
// Param:
// version - uint64 (the counter for this key, bumped by every mutation)
// payload - []byte (the exact bytes that will be sent)
//
// Return:
//   - the tag, quoted, ready to go on an ETag header
func BuildETag(version uint64, payload []byte) string {
    digest := sha256.Sum256(payload)

    return `"` + strconv.FormatUint(version, 10) + "-" + hex.EncodeToString(digest[:])[:digestLength] + `"`
}

// ETagMatches reports whether an If-None-Match header covers the tag on hand.
//
// A header may carry a list, and it may carry `*`, which means "any current
// representation". Both are handled here rather than by every caller, because a
// caller that only compares one string answers 200 to a client that was entitled
// to a 304.
//
// Param:
// header - string (the raw If-None-Match value, possibly empty or a list)
// tag - string (the tag this response would carry)
//
// Return:
//   - true when the client already holds this representation
//   - false when it does not, including when either side is empty
func ETagMatches(header string, tag string) bool {
    if header == "" || tag == "" {
        return false
    }

    if strings.TrimSpace(header) == "*" {
        return true
    }

    wanted := normaliseETag(tag)

    for _, candidate := range strings.Split(header, ",") {
        if normaliseETag(candidate) == wanted {
            return true
        }
    }

    return false
}

// normaliseETag strips the weak marker and the surrounding space so two tags
// that mean the same thing compare equal.
func normaliseETag(tag string) string {
    trimmed := strings.TrimSpace(tag)

    return strings.TrimPrefix(trimmed, weakPrefix)
}
