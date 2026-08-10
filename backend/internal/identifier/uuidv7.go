// Package identifier mints the identifiers this service stores.
//
// It is its own package because bookings, payment attempts, audit events, and
// refresh tokens all need one, and none of those should own the format.
//
// The format is UUIDv7: 48 bits of unix milliseconds followed by randomness.
// Two things follow from that choice. Sorting by id sorts roughly by creation
// time, so the primary key index keeps appending at its right hand edge instead
// of writing into random pages the way UUIDv4 does. And the migration needs no
// extension and no database side default, because the application is the only
// thing that mints an id.
package identifier

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

// uuidLength is the byte count of a raw uuid, before it is written as text.
const uuidLength = 16

// timestampBytes is how many leading bytes carry the millisecond. The remaining
// ten are random, minus the version and variant bits written over them.
const timestampBytes = 6

// NewUUIDv7 mints an identifier from the current time and the system source of
// randomness.
//
// Return:
//   - the identifier in the usual 8-4-4-4-12 text form
//   - an error when randomness is unavailable, which is never ignored, because
//     an identifier built from a failed read would not be unique
func NewUUIDv7() (string, error) {
	return newUUIDv7At(time.Now(), rand.Reader)
}

// newUUIDv7At is the whole implementation with its two inputs handed in, which
// is what lets a test pin the timestamp and fail the randomness on purpose.
//
// Param:
// moment - time.Time (the instant encoded into the leading 48 bits)
// entropy - io.Reader (where the remaining bytes come from)
//
// Return:
//   - the identifier in text form
//   - an error when the clock is before the unix epoch or randomness fails
func newUUIDv7At(moment time.Time, entropy io.Reader) (string, error) {
	milliseconds := moment.UnixMilli()
	if milliseconds < 0 {
		return "", errors.New("the clock is before the unix epoch, an identifier cannot be built from it")
	}

	var raw [uuidLength]byte

	if _, err := io.ReadFull(entropy, raw[timestampBytes:]); err != nil {
		return "", fmt.Errorf("cannot read randomness for an identifier: %w", err)
	}

	raw[0] = byte(milliseconds >> 40)
	raw[1] = byte(milliseconds >> 32)
	raw[2] = byte(milliseconds >> 24)
	raw[3] = byte(milliseconds >> 16)
	raw[4] = byte(milliseconds >> 8)
	raw[5] = byte(milliseconds)

	// Version 7 in the high nibble of byte 6, variant 10 in the top two bits of
	// byte 8. Both are written over random bytes, which is why they come after
	// the read rather than before it.
	raw[6] = (raw[6] & 0x0f) | 0x70
	raw[8] = (raw[8] & 0x3f) | 0x80

	return textForm(raw), nil
}

// textForm writes the sixteen bytes as the hyphenated text every column of type
// uuid accepts.
func textForm(raw [uuidLength]byte) string {
	encoded := make([]byte, hex.EncodedLen(uuidLength))
	hex.Encode(encoded, raw[:])

	return fmt.Sprintf("%s-%s-%s-%s-%s",
		encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}
