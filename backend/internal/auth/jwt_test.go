package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
)

// testSecret is long enough to be accepted and obviously not a real key.
const testSecret = "test-only-signing-key-that-is-long-enough"

// newTestSigner builds a signer, failing the test rather than the caller.
func newTestSigner(t *testing.T) *auth.Signer {
	t.Helper()

	signer, err := auth.NewSigner(testSecret)
	if err != nil {
		t.Fatalf("cannot build a signer: %v", err)
	}

	return signer
}

// payloadOf decodes the middle segment of a token into a plain map, which is
// what anybody holding the token can do.
func payloadOf(t *testing.T, token string) map[string]any {
	t.Helper()

	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		t.Fatalf("expected three segments, got %d", len(segments))
	}

	decoded, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		t.Fatalf("the payload is not base64url: %v", err)
	}

	var claims map[string]any

	if err := json.Unmarshal(decoded, &claims); err != nil {
		t.Fatalf("the payload is not json: %v", err)
	}

	return claims
}

func TestSignerConstruction(t *testing.T) {
	t.Run("edge: a secret shorter than a sha256 block is refused", func(t *testing.T) {
		// The whole security of HS256 is that the key is not guessable. A key
		// short enough to brute force offline turns every token into a
		// forgeable one.
		if _, err := auth.NewSigner("short"); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected a short secret to be refused, got %v", err)
		}
	})

	t.Run("edge: an empty secret is refused", func(t *testing.T) {
		if _, err := auth.NewSigner(""); !errors.Is(err, auth.ErrInvalidRequest) {
			t.Fatalf("expected an empty secret to be refused, got %v", err)
		}
	})
}

func TestSignAndVerify(t *testing.T) {
	t.Run("unit: a signed token verifies and reads back what went in", func(t *testing.T) {
		signer := newTestSigner(t)
		claims := liveClaims()

		token, err := signer.Sign(claims)
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		read, err := signer.Verify(token, claimsMoment.Add(time.Minute))
		if err != nil {
			t.Fatalf("cannot verify: %v", err)
		}

		if read != claims {
			t.Fatalf("expected %+v, got %+v", claims, read)
		}
	})

	t.Run("unit: a claim set this service would not issue is never signed", func(t *testing.T) {
		// Signing something that fails on its first use puts the failure at the
		// far end of the system from the bug.
		signer := newTestSigner(t)
		claims := liveClaims()
		claims.TokenID = ""

		if _, err := signer.Sign(claims); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected an unsignable claim set to be refused, got %v", err)
		}
	})

	t.Run("edge: a token signed with another key does not verify", func(t *testing.T) {
		signer := newTestSigner(t)

		other, err := auth.NewSigner("a-completely-different-key-of-sufficient-length")
		if err != nil {
			t.Fatalf("cannot build the second signer: %v", err)
		}

		token, err := other.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		if _, err := signer.Verify(token, claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected a foreign signature to be refused, got %v", err)
		}
	})

	t.Run("edge: a tampered payload does not verify", func(t *testing.T) {
		// The classic attempt: keep the signature, change the role to admin.
		// The signature covers the payload, so the mac no longer matches.
		signer := newTestSigner(t)

		token, err := signer.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		segments := strings.Split(token, ".")

		promoted := liveClaims()
		promoted.Role = auth.RoleAdmin

		encoded, err := json.Marshal(promoted)
		if err != nil {
			t.Fatalf("cannot encode the tampered claims: %v", err)
		}

		forged := segments[0] + "." + base64.RawURLEncoding.EncodeToString(encoded) + "." + segments[2]

		if _, err := signer.Verify(forged, claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected a tampered payload to be refused, got %v", err)
		}
	})

	t.Run("edge: a token claiming algorithm none is refused", func(t *testing.T) {
		// The forgery this check exists for: rewrite the header to say there
		// is no algorithm, leave the signature empty, and hope the verifier
		// takes the payload at its word.
		signer := newTestSigner(t)

		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))

		encoded, err := json.Marshal(liveClaims())
		if err != nil {
			t.Fatalf("cannot encode the claims: %v", err)
		}

		forged := header + "." + base64.RawURLEncoding.EncodeToString(encoded) + "."

		if _, err := signer.Verify(forged, claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected alg none to be refused, got %v", err)
		}
	})

	t.Run("edge: a token claiming a different algorithm is refused", func(t *testing.T) {
		signer := newTestSigner(t)

		token, err := signer.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		segments := strings.Split(token, ".")
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

		if _, err := signer.Verify(header+"."+segments[1]+"."+segments[2], claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected a swapped algorithm to be refused, got %v", err)
		}
	})

	t.Run("edge: an expired token reports expiry, which is the one failure a client acts on", func(t *testing.T) {
		signer := newTestSigner(t)
		claims := liveClaims()

		token, err := signer.Sign(claims)
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		if _, err := signer.Verify(token, claims.Expiry()); !errors.Is(err, auth.ErrTokenExpired) {
			t.Fatalf("expected an expired token to report expiry, got %v", err)
		}
	})

	t.Run("edge: a tampered token that is also expired reports invalid, not expired", func(t *testing.T) {
		// Order matters. Telling the holder of a forgery that the only
		// remaining problem is the clock tells them what to fix next.
		signer := newTestSigner(t)
		claims := liveClaims()

		token, err := signer.Sign(claims)
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		forged := token[:len(token)-1] + flipLastCharacter(token)

		if _, err := signer.Verify(forged, claims.Expiry().Add(time.Hour)); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected a forged token to be refused before its clock is judged, got %v", err)
		}
	})

	t.Run("edge: a token with the wrong number of segments is refused", func(t *testing.T) {
		signer := newTestSigner(t)

		for _, malformed := range []string{"", ".", "one.two", "one.two.three.four"} {
			if _, err := signer.Verify(malformed, claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
				t.Fatalf("expected %q to be refused, got %v", malformed, err)
			}
		}
	})

	t.Run("edge: a signature that is not base64url is refused rather than panicking", func(t *testing.T) {
		signer := newTestSigner(t)

		token, err := signer.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		segments := strings.Split(token, ".")

		if _, err := signer.Verify(segments[0]+"."+segments[1]+".not base64", claimsMoment); !errors.Is(err, auth.ErrTokenInvalid) {
			t.Fatalf("expected a malformed signature to be refused, got %v", err)
		}
	})
}

func TestTheEncodedPayloadCarriesNothingSensitive(t *testing.T) {
	t.Run("edge: the payload holds the six agreed claims and nothing else", func(t *testing.T) {
		// A JWT payload is base64, not encryption. Anybody holding the token
		// reads it, including whoever picks it out of a shared screen
		// recording, so this asserts the exact key set rather than the absence
		// of one field.
		signer := newTestSigner(t)

		token, err := signer.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		payload := payloadOf(t, token)

		expected := map[string]bool{"sub": true, "role": true, "typ": true, "jti": true, "iat": true, "exp": true}

		if len(payload) != len(expected) {
			t.Fatalf("expected exactly %d claims, got %d: %v", len(expected), len(payload), payload)
		}

		for name := range payload {
			if !expected[name] {
				t.Fatalf("the payload carries an unexpected claim %q", name)
			}
		}
	})

	t.Run("edge: no email and no name reach the encoded payload", func(t *testing.T) {
		// The struct is closed, so this cannot fail today. It is asserted
		// anyway, because the change that would break it is somebody adding a
		// convenient field, and this is the line that stops it.
		signer := newTestSigner(t)

		token, err := signer.Sign(liveClaims())
		if err != nil {
			t.Fatalf("cannot sign: %v", err)
		}

		segments := strings.Split(token, ".")

		decoded, err := base64.RawURLEncoding.DecodeString(segments[1])
		if err != nil {
			t.Fatalf("the payload is not base64url: %v", err)
		}

		for _, forbidden := range []string{"@", "email", "name", "full_name", "child", "student"} {
			if strings.Contains(strings.ToLower(string(decoded)), forbidden) {
				t.Fatalf("the payload contains %q: %s", forbidden, decoded)
			}
		}
	})
}

// flipLastCharacter returns a character that is not the token's last one, so a
// forgery can be built without caring what was there.
func flipLastCharacter(token string) string {
	if strings.HasSuffix(token, "A") {
		return "B"
	}

	return "A"
}
