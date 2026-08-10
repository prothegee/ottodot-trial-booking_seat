package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/auth"
)

// cookiesOn reads what a handler put on the response, by name.
func cookiesOn(t *testing.T, recorder *httptest.ResponseRecorder) map[string]*http.Cookie {
	t.Helper()

	written := make(map[string]*http.Cookie)

	for _, cookie := range recorder.Result().Cookies() {
		written[cookie.Name] = cookie
	}

	return written
}

// issuedForCookies is one session with both tokens and their expiries.
func issuedForCookies() auth.Issued {
	return auth.Issued{
		AccessToken:      "an.access.token",
		AccessExpiresAt:  claimsMoment.Add(15 * time.Minute),
		RefreshToken:     "an-opaque-refresh-token",
		RefreshExpiresAt: claimsMoment.Add(720 * time.Hour),
	}
}

func TestWritingTheSessionCookies(t *testing.T) {
	t.Run("integration: both cookies are written with the tokens they carry", func(t *testing.T) {
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, issuedForCookies(), claimsMoment)

		written := cookiesOn(t, recorder)

		if written[auth.AccessCookieName].Value != "an.access.token" {
			t.Fatalf("expected the access token, got %q", written[auth.AccessCookieName].Value)
		}

		if written[auth.RefreshCookieName].Value != "an-opaque-refresh-token" {
			t.Fatalf("expected the refresh token, got %q", written[auth.RefreshCookieName].Value)
		}
	})

	t.Run("edge: every cookie is HttpOnly, so no script in the page can read a token", func(t *testing.T) {
		// This is the whole reason tokens live in cookies here. An injected
		// script can read localStorage and it cannot read this.
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, issuedForCookies(), claimsMoment)

		for name, cookie := range cookiesOn(t, recorder) {
			if !cookie.HttpOnly {
				t.Fatalf("%s is readable by a script", name)
			}
		}
	})

	t.Run("edge: every cookie is SameSite strict, which is the first csrf defence", func(t *testing.T) {
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, issuedForCookies(), claimsMoment)

		for name, cookie := range cookiesOn(t, recorder) {
			if cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("%s would travel on a request another site started", name)
			}
		}
	})

	t.Run("edge: the secure flag follows configuration, so a local run over http still works", func(t *testing.T) {
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: false}).
			Write(recorder, issuedForCookies(), claimsMoment)

		for name, cookie := range cookiesOn(t, recorder) {
			if cookie.Secure {
				t.Fatalf("%s was marked secure while configuration said otherwise", name)
			}
		}
	})

	t.Run("edge: the refresh cookie is scoped to the auth group and never to a business route", func(t *testing.T) {
		// Scoping it at all is what keeps a refresh token off every class list
		// read. Scoping it to the group rather than the refresh route alone is
		// what lets sign out see the token it has to revoke.
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, issuedForCookies(), claimsMoment)

		written := cookiesOn(t, recorder)

		if written[auth.RefreshCookieName].Path != auth.RefreshCookiePath {
			t.Fatalf("expected %s, got %s", auth.RefreshCookiePath, written[auth.RefreshCookieName].Path)
		}

		if written[auth.AccessCookieName].Path != auth.AccessCookiePath {
			t.Fatalf("expected the access cookie on every route, got %s", written[auth.AccessCookieName].Path)
		}
	})

	t.Run("edge: a cookie never outlives the token it carries", func(t *testing.T) {
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, issuedForCookies(), claimsMoment)

		written := cookiesOn(t, recorder)

		if written[auth.AccessCookieName].MaxAge != int((15 * time.Minute).Seconds()) {
			t.Fatalf("expected 900 seconds, got %d", written[auth.AccessCookieName].MaxAge)
		}

		if written[auth.RefreshCookieName].MaxAge != int((720 * time.Hour).Seconds()) {
			t.Fatalf("expected 30 days in seconds, got %d", written[auth.RefreshCookieName].MaxAge)
		}
	})

	t.Run("edge: a token that has already expired writes a cookie the browser drops", func(t *testing.T) {
		// Zero would mean a session cookie that survives until the tab closes,
		// which is the opposite of what an expired token wants.
		recorder := httptest.NewRecorder()

		expired := issuedForCookies()
		expired.AccessExpiresAt = claimsMoment.Add(-time.Second)

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).
			Write(recorder, expired, claimsMoment)

		if cookiesOn(t, recorder)[auth.AccessCookieName].MaxAge != -1 {
			t.Fatal("expected an already expired token to be written as a cookie to drop")
		}
	})
}

func TestClearingTheSessionCookies(t *testing.T) {
	t.Run("integration: clearing empties both values and negates both ages", func(t *testing.T) {
		// Both, because a browser that honours one and not the other must not
		// be left holding a working token.
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).Clear(recorder)

		written := cookiesOn(t, recorder)

		for _, name := range []string{auth.AccessCookieName, auth.RefreshCookieName} {
			if written[name].Value != "" {
				t.Fatalf("%s still carries a value", name)
			}

			if written[name].MaxAge != -1 {
				t.Fatalf("%s was not marked for removal", name)
			}
		}
	})

	t.Run("edge: clearing uses the same paths, or the browser would keep the originals", func(t *testing.T) {
		// A cookie is identified by name and path. Clearing the refresh cookie
		// on / would write a second, empty cookie and leave the real one in
		// place.
		recorder := httptest.NewRecorder()

		auth.NewCookieWriter(auth.CookieSettings{Secure: true}).Clear(recorder)

		written := cookiesOn(t, recorder)

		if written[auth.RefreshCookieName].Path != auth.RefreshCookiePath {
			t.Fatalf("expected %s, got %s", auth.RefreshCookiePath, written[auth.RefreshCookieName].Path)
		}

		if written[auth.AccessCookieName].Path != auth.AccessCookiePath {
			t.Fatalf("expected %s, got %s", auth.AccessCookiePath, written[auth.AccessCookieName].Path)
		}
	})
}

func TestReadingACookie(t *testing.T) {
	t.Run("unit: a cookie that is there reads back", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		request.AddCookie(&http.Cookie{Name: auth.AccessCookieName, Value: "a.token"})

		if auth.CookieValue(request, auth.AccessCookieName) != "a.token" {
			t.Fatal("expected the cookie value")
		}
	})

	t.Run("edge: a cookie that is absent reads as empty rather than as a failure", func(t *testing.T) {
		// A first visit has no cookies. A caller that needs one says so itself.
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)

		if auth.CookieValue(request, auth.AccessCookieName) != "" {
			t.Fatal("expected an absent cookie to read as empty")
		}
	})
}
