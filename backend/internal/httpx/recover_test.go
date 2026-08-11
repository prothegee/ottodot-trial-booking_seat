package httpx_test

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/httpx"
)

func TestRecoveringFromAPanic(t *testing.T) {
    t.Run("integration: a handler that falls over answers internal_error", func(t *testing.T) {
        var reported error

        handler := httpx.Chain(
            http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
                panic("the connection pool is nil")
            }),
            httpx.WithRequestID,
            httpx.Recover(func(_ string, err error) { reported = err }),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if recorder.Code != http.StatusInternalServerError {
            t.Fatalf("a panic answered %d", recorder.Code)
        }

        if failureOf(t, recorder).Error.Code != httpx.CodeInternalError {
            t.Fatalf("a panic answered %q", failureOf(t, recorder).Error.Code)
        }

        if reported == nil {
            t.Fatal("the panic was swallowed, so nothing would ever be looked at")
        }
    })

    t.Run("behaviour: the answer carries a request id and nothing else", func(t *testing.T) {
        handler := httpx.Chain(
            http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
                panic("row: student_id aaaa-bbbb, email alice.tan@example.test")
            }),
            httpx.WithRequestID,
            httpx.Recover(nil),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        body := failureOf(t, recorder)

        if body.Error.RequestID == "" {
            t.Fatal("an internal_error carries no request id, so nobody can find the log line")
        }

        for _, leak := range []string{"student_id", "alice.tan", "@", "row:"} {
            if strings.Contains(recorder.Body.String(), leak) {
                t.Fatalf("the body carries %q from the panic: %s", leak, recorder.Body.String())
            }
        }
    })

    t.Run("edge: a panic after the body started does not append an envelope to half a document", func(t *testing.T) {
        handler := httpx.Chain(
            http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
                response.WriteHeader(http.StatusOK)
                _, _ = response.Write([]byte(`{"classes":[`))

                panic("halfway through")
            }),
            httpx.WithRequestID,
            httpx.Recover(nil),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if recorder.Code != http.StatusOK {
            t.Fatalf("the status was rewritten to %d after it had already gone out", recorder.Code)
        }

        if strings.Contains(recorder.Body.String(), "internal_error") {
            t.Fatalf("an envelope was appended to a half written body: %s", recorder.Body.String())
        }
    })

    t.Run("edge: a handler that does not panic is untouched", func(t *testing.T) {
        handler := httpx.Chain(
            http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
                response.WriteHeader(http.StatusTeapot)
            }),
            httpx.WithRequestID,
            httpx.Recover(nil),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if recorder.Code != http.StatusTeapot {
            t.Fatalf("an ordinary answer was rewritten to %d", recorder.Code)
        }
    })
}

func TestChainingMiddleware(t *testing.T) {
    t.Run("unit: middleware runs in the order it is written", func(t *testing.T) {
        var order []string

        mark := func(name string) httpx.Middleware {
            return func(next http.Handler) http.Handler {
                return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
                    order = append(order, name)

                    next.ServeHTTP(response, request)
                })
            }
        }

        handler := httpx.Chain(
            http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
            mark("first"),
            mark("second"),
        )

        handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

        if strings.Join(order, ",") != "first,second,handler" {
            t.Fatalf("the chain ran as %v, and a chain that authenticates after rate limiting counts the wrong bucket",
                order)
        }
    })
}
