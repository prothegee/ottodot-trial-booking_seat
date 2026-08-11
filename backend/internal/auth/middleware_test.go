package auth_test

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

// testOrigin is the one origin the guard serves in these cases.
const testOrigin = "http://127.0.0.1:9001"

// unreachableDenylist is a denylist that cannot answer, which is the case where
// the service has to refuse rather than guess.
type unreachableDenylist struct{}

func (unreachableDenylist) Deny(ctx context.Context, tokenID string, until time.Time) error {
    return errors.New("the denylist is unreachable")
}

func (unreachableDenylist) IsDenied(ctx context.Context, tokenID string, now time.Time) (bool, error) {
    return false, errors.New("the denylist is unreachable")
}

// guardStage is a guard with a pinned clock and a signer a test can sign with.
type guardStage struct {
    guard    *auth.Guard
    signer   *auth.Signer
    denylist auth.Denylist
    now      time.Time
}

// newGuardStage builds the guard over whichever denylist the case needs.
func newGuardStage(t *testing.T, denylist auth.Denylist) *guardStage {
    t.Helper()

    stage := &guardStage{signer: newTestSigner(t), denylist: denylist, now: claimsMoment}

    guard, err := auth.NewGuard(stage.signer, denylist, auth.GuardSettings{
        FrontendOrigin: testOrigin,
        Clock:          func() time.Time { return stage.now },
    })
    if err != nil {
        t.Fatalf("cannot build the guard: %v", err)
    }

    stage.guard = guard

    return stage
}

// signedRequest builds a request carrying an access cookie for these claims.
func (stage *guardStage) signedRequest(t *testing.T, method string, claims auth.Claims) *http.Request {
    t.Helper()

    token, err := stage.signer.Sign(claims)
    if err != nil {
        t.Fatalf("cannot sign: %v", err)
    }

    request := httptest.NewRequest(method, "/api/v1/bookings", nil)
    request.AddCookie(&http.Cookie{Name: auth.AccessCookieName, Value: token})
    request.Header.Set("Origin", testOrigin)

    return request
}

// codeOf reads the error code out of an envelope the guard wrote.
func codeOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
    t.Helper()

    var body struct {
        Error struct {
            Code string `json:"code"`
        } `json:"error"`
    }

    if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
        t.Fatalf("the refusal is not the envelope: %v, body %s", err, recorder.Body.String())
    }

    return body.Error.Code
}

// recordingHandler is what runs when the guard lets a request through.
func recordingHandler(reached *bool, identity *auth.Identity) http.Handler {
    return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        *reached = true

        if held, carried := auth.IdentityFrom(request.Context()); carried {
            *identity = held
        }

        response.WriteHeader(http.StatusOK)
    })
}

func TestGuardConstruction(t *testing.T) {
    t.Run("edge: a guard with no origin to trust is refused rather than built", func(t *testing.T) {
        // A guard built with an empty origin would compare every mutation
        // against "" and let nothing through, or worse, be edited into letting
        // everything through.
        signer := newTestSigner(t)

        if _, err := auth.NewGuard(signer, auth.NewMemoryDenylist(), auth.GuardSettings{}); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing origin to be refused, got %v", err)
        }

        if _, err := auth.NewGuard(nil, auth.NewMemoryDenylist(), auth.GuardSettings{FrontendOrigin: testOrigin}); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing signer to be refused, got %v", err)
        }

        if _, err := auth.NewGuard(signer, nil, auth.GuardSettings{FrontendOrigin: testOrigin}); !errors.Is(err, auth.ErrInvalidRequest) {
            t.Fatalf("expected a missing denylist to be refused, got %v", err)
        }
    })
}

func TestAuthenticatingARequest(t *testing.T) {
    t.Run("integration: a live token establishes the identity behind the request", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if !reached {
            t.Fatalf("expected the request to be let through, got %d", recorder.Code)
        }

        if identity.ParentID != liveClaims().Subject {
            t.Fatalf("expected %s, got %s", liveClaims().Subject, identity.ParentID)
        }

        if identity.TokenID != liveClaims().TokenID {
            t.Fatal("expected the token id to be carried, so a sign out has something to withdraw")
        }

        if !identity.ExpiresAt.Equal(liveClaims().Expiry()) {
            t.Fatal("expected the expiry to be carried, so a sign out knows how long to withdraw for")
        }
    })

    t.Run("edge: a request with no cookie is refused as an invalid token", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodGet, "/api/v1/bookings", nil)

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if reached {
            t.Fatal("a request with no token reached the handler")
        }

        if recorder.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", recorder.Code)
        }

        if codeOf(t, recorder) != auth.CodeTokenInvalid {
            t.Fatalf("expected %s, got %s", auth.CodeTokenInvalid, codeOf(t, recorder))
        }
    })

    t.Run("edge: an expired token reports token_expired, which is what the client refreshes on", func(t *testing.T) {
        // This is the one refusal the client acts on rather than shows. Getting
        // it wrong turns a silent refresh into a sign in screen.
        stage := newGuardStage(t, auth.NewMemoryDenylist())
        stage.now = liveClaims().Expiry().Add(time.Second)

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if reached {
            t.Fatal("an expired token reached the handler")
        }

        if codeOf(t, recorder) != auth.CodeTokenExpired {
            t.Fatalf("expected %s, got %s", auth.CodeTokenExpired, codeOf(t, recorder))
        }
    })

    t.Run("behaviour: a withdrawn token is refused even though its signature is still good", func(t *testing.T) {
        // This is what makes sign out real. Without it a signed out parent
        // keeps working for up to one access token lifetime.
        denylist := auth.NewMemoryDenylist()
        stage := newGuardStage(t, denylist)

        if err := denylist.Deny(context.Background(), liveClaims().TokenID, liveClaims().Expiry()); err != nil {
            t.Fatalf("cannot withdraw the token: %v", err)
        }

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if reached {
            t.Fatal("a withdrawn token reached the handler")
        }

        if codeOf(t, recorder) != auth.CodeTokenInvalid {
            t.Fatalf("expected %s, got %s", auth.CodeTokenInvalid, codeOf(t, recorder))
        }
    })

    t.Run("edge: a denylist that cannot answer refuses rather than guesses", func(t *testing.T) {
        // Guessing here means honouring a token somebody has already signed
        // out of, which is the one direction this must not fail in.
        stage := newGuardStage(t, unreachableDenylist{})

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if reached {
            t.Fatal("a request was let through while the denylist was unreadable")
        }
    })

    t.Run("edge: a forged token is refused", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodGet, "/api/v1/bookings", nil)
        request.AddCookie(&http.Cookie{Name: auth.AccessCookieName, Value: "not.a.token"})

        stage.guard.Authenticate(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if reached {
            t.Fatal("a forged token reached the handler")
        }
    })

    t.Run("edge: nothing outside this package can put an identity in the context", func(t *testing.T) {
        // The context key is an unexported type, so a handler cannot invent an
        // identity for itself. A middleware is only worth having if what it
        // establishes cannot be forged behind it.
        if _, carried := auth.IdentityFrom(context.Background()); carried {
            t.Fatal("a bare context carried an identity")
        }
    })
}

func TestRequiringARole(t *testing.T) {
    t.Run("integration: the role a route is for is let through", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        claims := liveClaims()
        claims.Role = auth.RoleAdmin

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.RequireRole(auth.RoleAdmin, recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, claims))

        if !reached {
            t.Fatalf("expected an admin to reach an admin route, got %d", recorder.Code)
        }

        if !identity.IsAdmin() {
            t.Fatal("expected the identity to report itself as admin")
        }
    })

    t.Run("behaviour: a parent reaching an admin route is refused", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.RequireRole(auth.RoleAdmin, recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if reached {
            t.Fatal("a parent reached an admin route")
        }

        if recorder.Code != http.StatusForbidden {
            t.Fatalf("expected 403, got %d", recorder.Code)
        }

        if codeOf(t, recorder) != auth.CodeForbiddenRole {
            t.Fatalf("expected %s, got %s", auth.CodeForbiddenRole, codeOf(t, recorder))
        }
    })

    t.Run("edge: a role check without a token is refused as a token failure, not a role one", func(t *testing.T) {
        // The role check wraps authentication rather than replacing it, so an
        // admin route cannot be wired in a way that checks the role and skips
        // the token.
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/queue", nil)

        stage.guard.RequireRole(auth.RoleAdmin, recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if reached {
            t.Fatal("an unauthenticated request reached an admin route")
        }

        if recorder.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", recorder.Code)
        }
    })

    t.Run("edge: the refusal names no role, so it teaches nobody what to look for", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()

        stage.guard.RequireRole(auth.RoleAdmin, recordingHandler(&reached, &identity)).
            ServeHTTP(recorder, stage.signedRequest(t, http.MethodGet, liveClaims()))

        if body := recorder.Body.String(); strings.Contains(body, auth.RoleAdmin) {
            t.Fatalf("the refusal names the role that would have worked: %s", body)
        }
    })
}

func TestCheckingTheOrigin(t *testing.T) {
    t.Run("integration: a mutation from the origin this service serves is let through", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil)
        request.Header.Set("Origin", testOrigin)

        stage.guard.CheckOrigin(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if !reached {
            t.Fatalf("expected the mutation to be let through, got %d", recorder.Code)
        }
    })

    t.Run("behaviour: a mutation from another site is refused", func(t *testing.T) {
        // Cookies travel automatically, so without this another site can make
        // a parent's browser issue a write that carries a real identity.
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil)
        request.Header.Set("Origin", "http://somewhere-else.example.test")

        stage.guard.CheckOrigin(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if reached {
            t.Fatal("a cross origin write reached the handler")
        }

        if recorder.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", recorder.Code)
        }
    })

    t.Run("edge: a mutation with no origin at all is refused", func(t *testing.T) {
        // Every browser sends one on a write, so an absent header means a
        // caller that is not the browser this service serves.
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        var (
            reached  bool
            identity auth.Identity
        )

        recorder := httptest.NewRecorder()
        request := httptest.NewRequest(http.MethodPost, "/api/v1/bookings", nil)

        stage.guard.CheckOrigin(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

        if reached {
            t.Fatal("a write with no origin reached the handler")
        }
    })

    t.Run("edge: a read is let through with no origin, or the first page load would fail", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
            var (
                reached  bool
                identity auth.Identity
            )

            recorder := httptest.NewRecorder()
            request := httptest.NewRequest(method, "/api/v1/classes", nil)

            stage.guard.CheckOrigin(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

            if !reached {
                t.Fatalf("a %s was refused for having no origin", method)
            }
        }
    })

    t.Run("edge: every write method is checked, not only post", func(t *testing.T) {
        stage := newGuardStage(t, auth.NewMemoryDenylist())

        for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
            var (
                reached  bool
                identity auth.Identity
            )

            recorder := httptest.NewRecorder()
            request := httptest.NewRequest(method, "/api/v1/bookings/one", nil)
            request.Header.Set("Origin", "http://somewhere-else.example.test")

            stage.guard.CheckOrigin(recordingHandler(&reached, &identity)).ServeHTTP(recorder, request)

            if reached {
                t.Fatalf("a cross origin %s was let through", method)
            }
        }
    })
}
