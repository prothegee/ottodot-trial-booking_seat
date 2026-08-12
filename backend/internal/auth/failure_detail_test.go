package auth_test

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/observability"
)

// denying answers one refusal on a request that carries somewhere to write the
// detail down, which is what the middleware around the real routes provides.
func denying(err error) (*httptest.ResponseRecorder, *observability.FailureDetail) {
    carrying, detail := observability.WithFailureDetail(context.Background())

    request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil).WithContext(carrying)
    recorder := httptest.NewRecorder()

    auth.Deny(recorder, request, err)

    return recorder, detail
}

func TestRecordingTheFailureBehindARefusedSignIn(t *testing.T) {
    t.Run("integration: a sign in that fails for an unmapped reason is written down", func(t *testing.T) {
        recorder, detail := denying(errors.New(`pq: relation "parents" does not exist`))

        if recorder.Code != http.StatusInternalServerError {
            t.Fatalf("an unmapped failure answered %d", recorder.Code)
        }

        if detail.Err() == nil {
            t.Fatal("the sign in answered 500 and left no reason, which is a status code and nothing else")
        }
    })

    t.Run("behaviour: the reason is recorded and never sent", func(t *testing.T) {
        recorder, _ := denying(errors.New(`select from parents where email = alice.tan@example.test`))

        for _, leak := range []string{"parents", "alice.tan", "@", "select"} {
            if strings.Contains(recorder.Body.String(), leak) {
                t.Fatalf("the body carries %q from the failure: %s", leak, recorder.Body.String())
            }
        }
    })

    t.Run("behaviour: a refusal this package meant is not recorded", func(t *testing.T) {
        _, detail := denying(auth.ErrNoSuchParent)

        if detail.Err() != nil {
            t.Fatalf("an unknown address was reported as a fault: %v", detail.Err())
        }
    })

    t.Run("edge: a request carrying nowhere to record is answered the same", func(t *testing.T) {
        recorder := httptest.NewRecorder()

        auth.Deny(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil),
            errors.New("the pool is closed"))

        if recorder.Code != http.StatusInternalServerError {
            t.Fatalf("a request with no carrier answered %d", recorder.Code)
        }
    })
}
