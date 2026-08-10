package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// This file is in the package rather than beside it, because the case worth
// covering is what happens when randomness fails, and that source is handed to
// an unexported function on purpose.

// failingReader is a source of randomness that is not available.
type failingReader struct{}

func (failingReader) Read(into []byte) (int, error) {
	return 0, errors.New("no randomness today")
}

// shortSource gives fewer bytes than asked for and then stops, which is the
// quieter failure: a token built from it would be shorter than intended and
// nothing would say so.
func shortSource() *bytes.Reader {
	return bytes.NewReader([]byte{0x01})
}

func TestRefreshTokenMinting(t *testing.T) {
	t.Run("unit: a minted token decodes to the full width of randomness", func(t *testing.T) {
		token, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("cannot mint a refresh token: %v", err)
		}

		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("the token is not base64url: %v", err)
		}

		if len(raw) != refreshTokenBytes {
			t.Fatalf("expected %d bytes of randomness, got %d", refreshTokenBytes, len(raw))
		}
	})

	t.Run("unit: two tokens are never the same", func(t *testing.T) {
		first, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("cannot mint the first token: %v", err)
		}

		second, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("cannot mint the second token: %v", err)
		}

		if first == second {
			t.Fatal("two minted tokens were identical, which means the source is not random")
		}
	})

	t.Run("edge: the token is url safe, so it survives a cookie unescaped", func(t *testing.T) {
		token, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("cannot mint a refresh token: %v", err)
		}

		if strings.ContainsAny(token, "+/=") {
			t.Fatalf("the token carries a character a cookie would escape: %s", token)
		}
	})

	t.Run("edge: unavailable randomness is a failure, never a weak token", func(t *testing.T) {
		if _, err := newRefreshTokenFrom(failingReader{}); err == nil {
			t.Fatal("expected a failed read to be reported rather than swallowed")
		}
	})

	t.Run("edge: a short read is a failure, not a shorter token", func(t *testing.T) {
		// This is the one that would pass unnoticed. io.ReadFull is what turns
		// it into an error instead of a token with one random byte and
		// thirty-one zeros.
		if _, err := newRefreshTokenFrom(shortSource()); err == nil {
			t.Fatal("expected a short read to be reported")
		}
	})
}

func TestRefreshTokenHashing(t *testing.T) {
	t.Run("unit: the same token always hashes to the same bytes", func(t *testing.T) {
		// The lookup hashes what arrived and compares, so this is the property
		// the whole storage rule stands on.
		token := "a-token-that-arrived"

		if !bytes.Equal(HashRefreshToken(token), HashRefreshToken(token)) {
			t.Fatal("expected one token to hash to one value")
		}
	})

	t.Run("unit: two tokens hash to different bytes", func(t *testing.T) {
		if bytes.Equal(HashRefreshToken("first"), HashRefreshToken("second")) {
			t.Fatal("expected two tokens to hash apart")
		}
	})

	t.Run("edge: the hash is the width the bytea column holds", func(t *testing.T) {
		if len(HashRefreshToken("anything")) != 32 {
			t.Fatalf("expected a 32 byte digest, got %d", len(HashRefreshToken("anything")))
		}
	})

	t.Run("edge: the token itself never appears in what is stored", func(t *testing.T) {
		token := "the-token-that-must-not-be-stored"

		if bytes.Contains(HashRefreshToken(token), []byte(token)) {
			t.Fatal("the stored value contains the token it was built from")
		}
	})
}
