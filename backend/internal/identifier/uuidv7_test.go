package identifier

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// textShape is the form every uuid column accepts. Postgres rejects anything
// else, so this is worth pinning rather than assuming.
var textShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// failingReader stands in for a randomness source that has stopped working.
type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("no randomness available")
}

func TestAMintedIdentifierHasTheRightShape(t *testing.T) {
	t.Run("unit: the text form is what a uuid column accepts", func(t *testing.T) {
		minted, err := NewUUIDv7()
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		if !textShape.MatchString(minted) {
			t.Fatalf("the identifier is not in the 8-4-4-4-12 form: %q", minted)
		}
	})

	t.Run("unit: the version is 7 and the variant is the standard one", func(t *testing.T) {
		minted, err := NewUUIDv7()
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		// Character 14 is the version nibble, character 19 the variant nibble.
		// A wrong version here would still store fine and would still look like
		// a uuid, which is exactly why it is asserted.
		if minted[14] != '7' {
			t.Fatalf("expected version 7, got %q in %q", minted[14], minted)
		}

		if !strings.ContainsRune("89ab", rune(minted[19])) {
			t.Fatalf("expected the 10 variant, got %q in %q", minted[19], minted)
		}
	})
}

func TestIdentifiersSortInTheOrderTheyWereMinted(t *testing.T) {
	t.Run("unit: a later millisecond produces a larger identifier", func(t *testing.T) {
		earlier, err := newUUIDv7At(time.UnixMilli(1_700_000_000_000), bytes.NewReader(make([]byte, 16)))
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		later, err := newUUIDv7At(time.UnixMilli(1_700_000_001_000), bytes.NewReader(make([]byte, 16)))
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		if !(earlier < later) {
			t.Fatalf("expected %q to sort before %q", earlier, later)
		}
	})

	t.Run("edge: two identifiers minted in the same millisecond still differ", func(t *testing.T) {
		moment := time.UnixMilli(1_700_000_000_000)

		first, err := NewUUIDv7()
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		second, err := NewUUIDv7()
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		if first == second {
			t.Fatalf("two identifiers came out identical: %q", first)
		}

		// The same instant handed in twice must still differ, which is what the
		// randomness after the timestamp is for.
		fixedFirst, err := newUUIDv7At(moment, randomBytesFor(t, 1))
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		fixedSecond, err := newUUIDv7At(moment, randomBytesFor(t, 2))
		if err != nil {
			t.Fatalf("expected an identifier, got: %v", err)
		}

		if fixedFirst == fixedSecond {
			t.Fatalf("the same millisecond produced the same identifier twice: %q", fixedFirst)
		}
	})
}

func TestAnIdentifierIsRefusedRatherThanGuessed(t *testing.T) {
	t.Run("edge: a broken randomness source is reported", func(t *testing.T) {
		_, err := newUUIDv7At(time.UnixMilli(1_700_000_000_000), failingReader{})
		if err == nil {
			t.Fatal("expected the failed read to be reported, got an identifier instead")
		}

		if !strings.Contains(err.Error(), "randomness") {
			t.Fatalf("the error does not name the cause: %v", err)
		}
	})

	t.Run("edge: a clock before the unix epoch is refused", func(t *testing.T) {
		_, err := newUUIDv7At(time.UnixMilli(-1), bytes.NewReader(make([]byte, 16)))
		if err == nil {
			t.Fatal("expected a negative millisecond to be refused")
		}
	})

	t.Run("edge: a short randomness source is refused, never padded", func(t *testing.T) {
		// Nine bytes when ten are needed. Silently accepting this would produce
		// identifiers with a predictable tail.
		_, err := newUUIDv7At(time.UnixMilli(1_700_000_000_000), bytes.NewReader(make([]byte, 9)))
		if err == nil {
			t.Fatal("expected a short read to be refused")
		}
	})
}

// randomBytesFor builds a repeatable ten byte source, so a test can hand the
// same instant two different tails.
func randomBytesFor(t *testing.T, fill byte) *bytes.Reader {
	t.Helper()

	tail := make([]byte, uuidLength-timestampBytes)
	for index := range tail {
		tail[index] = fill
	}

	return bytes.NewReader(tail)
}
