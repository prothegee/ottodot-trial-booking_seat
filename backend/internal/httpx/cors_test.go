package httpx_test

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/httpx"
)

// The bug these cases exist for: this api served its client from a second port
// and sent no cors headers at all. curl was happy, every test passed, and a
// browser refused every call with a message that explained nothing.

// corsGuard builds a guard that serves the two development origins.
func corsGuard(t *testing.T) *auth.Guard {
    t.Helper()

    signer, err := auth.NewSigner("a-signing-key-long-enough-to-be-accepted")
    if err != nil {
        t.Fatalf("cannot build the signer: %v", err)
    }

    guard, err := auth.NewGuard(signer, auth.NewMemoryDenylist(), auth.GuardSettings{
        AllowedOrigins: []string{"http://127.0.0.1:9001", "http://localhost:9001"},
    })
    if err != nil {
        t.Fatalf("cannot build the guard: %v", err)
    }

    return guard
}

// corsCall sends one request through the layer and reports what came back.
func corsCall(t *testing.T, method string, origin string) *httptest.ResponseRecorder {
    t.Helper()

    reached := false

    handler := httpx.CrossOrigin(corsGuard(t), http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
        reached = true

        response.WriteHeader(http.StatusOK)
    }))

    request := httptest.NewRequest(method, "/api/v1/classes", nil)

    if origin != "" {
        request.Header.Set("Origin", origin)
    }

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, request)

    if method == http.MethodOptions && reached {
        t.Fatal("a preflight reached the handler behind it, which has nothing to answer it with")
    }

    return recorder
}

func TestCrossOrigin(t *testing.T) {
    t.Run("integration: a call from the client is told it may read the answer", func(t *testing.T) {
        recorder := corsCall(t, http.MethodGet, "http://127.0.0.1:9001")

        if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:9001" {
            t.Fatalf("expected the origin to be echoed, got %q", got)
        }

        // Without this the browser drops the answer, cookies and all, however
        // correct everything else is.
        if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
            t.Fatalf("expected credentials to be permitted, got %q", got)
        }
    })

    t.Run("both spellings of the loopback address are served", func(t *testing.T) {
        // The two are different origins to a browser, and a reviewer types
        // whichever one they think of.
        for _, origin := range []string{"http://127.0.0.1:9001", "http://localhost:9001"} {
            recorder := corsCall(t, http.MethodGet, origin)

            if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != origin {
                t.Fatalf("%s: expected the origin to be echoed, got %q", origin, got)
            }
        }
    })

    t.Run("integration: a preflight is answered here and never reaches a handler", func(t *testing.T) {
        recorder := corsCall(t, http.MethodOptions, "http://127.0.0.1:9001")

        if recorder.Code != http.StatusNoContent {
            t.Fatalf("expected 204, got %d", recorder.Code)
        }

        if got := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
            t.Fatalf("expected the writes to be named, got %q", got)
        }

        // A json body needs this header, and a browser asks before sending one.
        if got := recorder.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Content-Type") {
            t.Fatalf("expected Content-Type to be permitted, got %q", got)
        }

        if recorder.Header().Get("Access-Control-Max-Age") == "" {
            t.Fatal("a preflight with no max age is one preflight per write")
        }
    })

    t.Run("integration: every header a request may carry is named on the preflight", func(t *testing.T) {
        // The bug this case exists for: Idempotency-Key was read by two
        // handlers and permitted by none. The browser refused the preflight,
        // the write never left the page, and the only thing on screen was the
        // generic failure. A header this api reads and does not permit is a
        // route a browser cannot reach at all.
        recorder := corsCall(t, http.MethodOptions, "http://127.0.0.1:9001")
        permitted := recorder.Header().Get("Access-Control-Allow-Headers")

        for _, header := range []string{"Content-Type", "If-None-Match", httpx.IdempotencyKeyHeader} {
            if !strings.Contains(permitted, header) {
                t.Fatalf("expected %s to be permitted, got %q", header, permitted)
            }
        }
    })

    t.Run("edge: the permitted list is matched however the browser spells it", func(t *testing.T) {
        // A browser lowercases what it asks for, and the client sends
        // idempotency-key. Header names are case insensitive, so the comparison
        // a reader makes has to be too.
        recorder := corsCall(t, http.MethodOptions, "http://127.0.0.1:9001")
        permitted := strings.ToLower(recorder.Header().Get("Access-Control-Allow-Headers"))

        if !strings.Contains(permitted, strings.ToLower(httpx.IdempotencyKeyHeader)) {
            t.Fatalf("the header the client actually sends is not permitted, got %q", permitted)
        }
    })

    t.Run("edge: another site is given nothing to work with", func(t *testing.T) {
        recorder := corsCall(t, http.MethodGet, "http://somewhere-else.example.test")

        if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
            t.Fatalf("a foreign origin was permitted: %q", got)
        }

        if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
            t.Fatalf("a foreign origin was told it may send credentials: %q", got)
        }
    })

    t.Run("edge: a request with no origin at all is left alone", func(t *testing.T) {
        // curl, a probe, and every one of this repository's own scripts. None
        // of them is a browser, and none of them needs a header.
        recorder := corsCall(t, http.MethodGet, "")

        if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
            t.Fatalf("a request with no origin was answered with one: %q", got)
        }

        if recorder.Code != http.StatusOK {
            t.Fatalf("expected the handler to answer normally, got %d", recorder.Code)
        }
    })

    t.Run("edge: every answer varies on the origin, including a refusal", func(t *testing.T) {
        // Without this a cache in front of this service can hand one origin's
        // answer, headers and all, to a request from another.
        for _, origin := range []string{"http://127.0.0.1:9001", "http://somewhere-else.example.test", ""} {
            recorder := corsCall(t, http.MethodGet, origin)

            if !strings.Contains(recorder.Header().Get("Vary"), "Origin") {
                t.Fatalf("origin %q: the answer does not vary on the origin", origin)
            }
        }
    })

    t.Run("edge: an origin the guard refuses for a write is refused here too", func(t *testing.T) {
        // The two answers have to agree. A browser told a write is permitted
        // and then refused when it makes one is the worst of both.
        guard := corsGuard(t)

        for _, origin := range []string{"http://localhost:9002", "http://127.0.0.1:9001/", ""} {
            recorder := corsCall(t, http.MethodGet, origin)
            permitted := recorder.Header().Get("Access-Control-Allow-Origin") != ""

            if permitted != guard.OriginIsAllowed(origin) {
                t.Fatalf("origin %q: the cors layer and the origin check disagree", origin)
            }
        }
    })
}
