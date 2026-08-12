package httpx_test

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/httpx"
)

func TestReportingTheFailureBehindA500(t *testing.T) {
    denying := func(err error) http.Handler {
        return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
            httpx.Deny(response, request, err)
        })
    }

    t.Run("integration: an unmapped failure is written down with its request id", func(t *testing.T) {
        var (
            reportedID  string
            reportedErr error
        )

        handler := httpx.Chain(
            denying(errors.New(`pq: relation "parents" does not exist`)),
            httpx.WithRequestID,
            httpx.ReportFailures(func(requestID string, err error) {
                reportedID = requestID
                reportedErr = err
            }),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil))

        if recorder.Code != http.StatusInternalServerError {
            t.Fatalf("an unmapped failure answered %d", recorder.Code)
        }

        if reportedErr == nil {
            t.Fatal("the 500 was answered and never written down, so nobody can find out why")
        }

        if reportedID == "" || reportedID != failureOf(t, recorder).Error.RequestID {
            t.Fatalf("the report is filed under %q and the client was told %q",
                reportedID, failureOf(t, recorder).Error.RequestID)
        }
    })

    t.Run("behaviour: the detail is logged and never sent", func(t *testing.T) {
        handler := httpx.Chain(
            denying(errors.New(`insert into bookings: alice.tan@example.test`)),
            httpx.WithRequestID,
            httpx.ReportFailures(nil),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil))

        for _, leak := range []string{"bookings:", "alice.tan", "@", "insert"} {
            if strings.Contains(recorder.Body.String(), leak) {
                t.Fatalf("the body carries %q from the failure: %s", leak, recorder.Body.String())
            }
        }
    })

    t.Run("behaviour: a refusal this service meant is not reported", func(t *testing.T) {
        reports := 0

        handler := httpx.Chain(
            denying(booking.ErrClassFull),
            httpx.WithRequestID,
            httpx.ReportFailures(func(string, error) { reports++ }),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil))

        if reports != 0 {
            t.Fatalf("a full class was reported %d time(s), which buries the ones nobody meant", reports)
        }
    })

    t.Run("edge: a request that never fails reports nothing", func(t *testing.T) {
        reports := 0

        handler := httpx.Chain(
            http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
                response.WriteHeader(http.StatusNoContent)
            }),
            httpx.WithRequestID,
            httpx.ReportFailures(func(string, error) { reports++ }),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/classes", nil))

        if reports != 0 {
            t.Fatalf("an answer that succeeded was reported %d time(s)", reports)
        }
    })

    t.Run("edge: nowhere to report to is not a failure of its own", func(t *testing.T) {
        handler := httpx.Chain(
            denying(errors.New("the pool is closed")),
            httpx.WithRequestID,
            httpx.ReportFailures(nil),
        )

        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil))

        if recorder.Code != http.StatusInternalServerError {
            t.Fatalf("a nil reporter changed the answer to %d", recorder.Code)
        }
    })
}
