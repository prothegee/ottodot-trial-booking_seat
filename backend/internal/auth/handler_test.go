package auth_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/auth"
)

// handlerStage is the four routes on a mux, over the same fakes the service
// tests use.
type handlerStage struct {
    *serviceStage

    mux *http.ServeMux
}

// newHandlerStage wires the routes.
func newHandlerStage(t *testing.T) *handlerStage {
    t.Helper()

    service := newServiceStage(t)

    guard, err := auth.NewGuard(service.signer, service.denylist, auth.GuardSettings{
        AllowedOrigins: []string{testOrigin},
        Clock:          func() time.Time { return service.now },
    })
    if err != nil {
        t.Fatalf("cannot build the guard: %v", err)
    }

    handler, err := auth.NewHandler(
        service.service,
        auth.NewCookieWriter(auth.CookieSettings{Secure: true}),
        guard)
    if err != nil {
        t.Fatalf("cannot build the handler: %v", err)
    }

    mux := http.NewServeMux()
    handler.Register(mux)

    return &handlerStage{serviceStage: service, mux: mux}
}

// call sends one request through the mux and returns what came back.
func (stage *handlerStage) call(request *http.Request) *httptest.ResponseRecorder {
    recorder := httptest.NewRecorder()
    stage.mux.ServeHTTP(recorder, request)

    return recorder
}

// signIn posts a login body with the right password and returns the response,
// cookies included.
func (stage *handlerStage) signIn(t *testing.T, email string) *httptest.ResponseRecorder {
    t.Helper()

    return stage.attemptSignIn(t, email, seededPassword)
}

// attemptSignIn posts whatever pair the caller wants to try.
func (stage *handlerStage) attemptSignIn(t *testing.T, email string, password string) *httptest.ResponseRecorder {
    t.Helper()

    body := strings.NewReader(`{"email":"` + email + `","password":"` + password + `"}`)

    request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
    request.Header.Set("Origin", testOrigin)
    request.Header.Set("Content-Type", "application/json")

    return stage.call(request)
}

// carry copies the cookies from one response onto the next request, which is
// what a browser does.
func carry(request *http.Request, from *httptest.ResponseRecorder) *http.Request {
    for _, cookie := range from.Result().Cookies() {
        if cookie.Value != "" {
            request.AddCookie(cookie)
        }
    }

    return request
}

func TestTheLoginRoute(t *testing.T) {
    t.Run("integration: a seeded email is signed in and both cookies come back", func(t *testing.T) {
        stage := newHandlerStage(t)

        response := stage.signIn(t, contractParentEmail)

        if response.Code != http.StatusNoContent {
            t.Fatalf("expected 204, got %d with body %s", response.Code, response.Body.String())
        }

        written := cookiesOn(t, response)

        if written[auth.AccessCookieName] == nil || written[auth.RefreshCookieName] == nil {
            t.Fatal("expected both cookies to be written")
        }
    })

    t.Run("edge: the answer carries no body, so no token is ever rendered", func(t *testing.T) {
        // The cookies are the answer. A body would be a second copy of
        // something the client is told never to read.
        stage := newHandlerStage(t)

        response := stage.signIn(t, contractParentEmail)

        if response.Body.Len() != 0 {
            t.Fatalf("expected an empty body, got %s", response.Body.String())
        }
    })

    t.Run("edge: an address nobody has gets the generic refusal, never a hint", func(t *testing.T) {
        // An endpoint that answers differently for a known address is an
        // endpoint that lists who has an account here.
        stage := newHandlerStage(t)

        response := stage.signIn(t, "nobody@example.test")

        if response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }

        if codeOf(t, response) != auth.CodeInvalidRequest {
            t.Fatalf("expected %s, got %s", auth.CodeInvalidRequest, codeOf(t, response))
        }

        if strings.Contains(strings.ToLower(response.Body.String()), "nobody@example.test") {
            t.Fatal("the refusal repeats the address back")
        }
    })

    t.Run("edge: a body carrying a field this service has no use for is refused", func(t *testing.T) {
        // A client sending a field this service does not read should be told,
        // not answered as if the value had been taken into account.
        stage := newHandlerStage(t)

        body := strings.NewReader(
            `{"email":"` + contractParentEmail + `","password":"` + seededPassword + `","remember":true}`)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
        request.Header.Set("Origin", testOrigin)

        if response := stage.call(request); response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }
    })

    t.Run("edge: the right email with the wrong password is refused, and says no more than that", func(t *testing.T) {
        stage := newHandlerStage(t)

        response := stage.attemptSignIn(t, contractParentEmail, "not-the-password")

        if response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }

        if len(response.Result().Cookies()) != 0 {
            t.Fatal("a refused sign in was given cookies")
        }

        // The same answer an unknown address gets. Any difference here is a way
        // to find out which addresses have accounts.
        unknown := newHandlerStage(t).attemptSignIn(t, "nobody@example.test", seededPassword)

        if response.Body.String() != unknown.Body.String() {
            t.Fatalf("a wrong password answers %q and an unknown address answers %q, so the two can be told apart",
                response.Body.String(), unknown.Body.String())
        }
    })

    t.Run("edge: a sign in with no password at all is refused", func(t *testing.T) {
        stage := newHandlerStage(t)

        body := strings.NewReader(`{"email":"` + contractParentEmail + `"}`)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
        request.Header.Set("Origin", testOrigin)

        response := stage.call(request)

        if response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }

        if len(response.Result().Cookies()) != 0 {
            t.Fatal("a sign in with no password was given cookies")
        }
    })

    t.Run("edge: a sign in from another site is refused before anything is read", func(t *testing.T) {
        stage := newHandlerStage(t)

        body := strings.NewReader(
            `{"email":"` + contractParentEmail + `","password":"` + seededPassword + `"}`)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
        request.Header.Set("Origin", "http://somewhere-else.example.test")

        response := stage.call(request)

        if response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }

        if len(response.Result().Cookies()) != 0 {
            t.Fatal("a cross origin sign in was given cookies")
        }
    })

    t.Run("edge: every auth answer is no-store", func(t *testing.T) {
        // None of it is shared between parents, and a cached session read is
        // one parent's account served to the next person on a shared machine.
        stage := newHandlerStage(t)

        if store := stage.signIn(t, contractParentEmail).Header().Get("Cache-Control"); store != "no-store" {
            t.Fatalf("expected no-store, got %q", store)
        }
    })
}

func TestTheSessionRoute(t *testing.T) {
    t.Run("integration: a signed in parent reads back their account", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        request := carry(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil), signedIn)

        response := stage.call(request)

        if response.Code != http.StatusOK {
            t.Fatalf("expected 200, got %d with body %s", response.Code, response.Body.String())
        }

        var session struct {
            ParentID    string `json:"parent_id"`
            DisplayName string `json:"display_name"`
            Role        string `json:"role"`
            Children    []struct {
                ID       string `json:"id"`
                FullName string `json:"full_name"`
            } `json:"children"`
        }

        if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
            t.Fatalf("cannot read the session: %v", err)
        }

        if session.ParentID != contractParentID || session.DisplayName != contractParentName {
            t.Fatalf("expected the seeded parent, got %+v", session)
        }

        if len(session.Children) != 1 || session.Children[0].FullName != contractChildName {
            t.Fatalf("expected the seeded child, got %+v", session.Children)
        }
    })

    t.Run("edge: the session answer carries no email and no token", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        request := carry(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil), signedIn)

        body := stage.call(request).Body.String()

        for _, forbidden := range []string{"@", "token", "email"} {
            if strings.Contains(strings.ToLower(body), forbidden) {
                t.Fatalf("the session answer contains %q: %s", forbidden, body)
            }
        }
    })

    t.Run("edge: the session read needs a token", func(t *testing.T) {
        stage := newHandlerStage(t)

        response := stage.call(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))

        if response.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", response.Code)
        }
    })

    t.Run("edge: an account with no children answers with an empty list, never null", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractLonelyParentEmail)

        request := carry(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil), signedIn)

        if body := stage.call(request).Body.String(); !strings.Contains(body, `"children":[]`) {
            t.Fatalf("expected an empty list, got %s", body)
        }
    })
}

func TestTheRefreshRoute(t *testing.T) {
    t.Run("integration: a refresh returns a new pair of cookies", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)
        before := cookiesOn(t, signedIn)[auth.RefreshCookieName].Value

        stage.now = stage.now.Add(20 * time.Minute)

        request := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil), signedIn)
        request.Header.Set("Origin", testOrigin)

        response := stage.call(request)

        if response.Code != http.StatusNoContent {
            t.Fatalf("expected 204, got %d with body %s", response.Code, response.Body.String())
        }

        after := cookiesOn(t, response)[auth.RefreshCookieName].Value

        if after == before {
            t.Fatal("the refresh cookie did not rotate")
        }
    })

    t.Run("behaviour: a refused refresh clears both cookies", func(t *testing.T) {
        // The parent is going back to the sign in screen either way, and
        // leaving a spent token in the browser means every later call tries it
        // and fails the same way.
        stage := newHandlerStage(t)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
        request.Header.Set("Origin", testOrigin)
        request.AddCookie(&http.Cookie{Name: auth.RefreshCookieName, Value: "a-token-from-somewhere-else"})

        response := stage.call(request)

        if response.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", response.Code)
        }

        for name, cookie := range cookiesOn(t, response) {
            if cookie.MaxAge != -1 {
                t.Fatalf("%s was not cleared after a refused refresh", name)
            }
        }
    })

    t.Run("edge: a refresh with no cookie at all is refused", func(t *testing.T) {
        stage := newHandlerStage(t)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
        request.Header.Set("Origin", testOrigin)

        if response := stage.call(request); response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }
    })
}

func TestTheLogoutRoute(t *testing.T) {
    t.Run("behaviour: signing out clears both cookies and withdraws the access token", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        request := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil), signedIn)
        request.Header.Set("Origin", testOrigin)

        response := stage.call(request)

        if response.Code != http.StatusNoContent {
            t.Fatalf("expected 204, got %d with body %s", response.Code, response.Body.String())
        }

        for name, cookie := range cookiesOn(t, response) {
            if cookie.MaxAge != -1 {
                t.Fatalf("%s was not cleared", name)
            }
        }

        // The same access token now gets nowhere, which is what makes the sign
        // out real rather than cosmetic.
        after := carry(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil), signedIn)

        if stage.call(after).Code != http.StatusUnauthorized {
            t.Fatal("the withdrawn access token still worked")
        }
    })

    t.Run("behaviour: signing out ends the refresh chain, so the stolen half is useless too", func(t *testing.T) {
        // This is why the refresh cookie is scoped to the auth group rather
        // than to the refresh route alone. Without it reaching this handler,
        // there is nothing here to revoke.
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        out := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil), signedIn)
        out.Header.Set("Origin", testOrigin)

        if response := stage.call(out); response.Code != http.StatusNoContent {
            t.Fatalf("expected 204, got %d", response.Code)
        }

        retry := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil), signedIn)
        retry.Header.Set("Origin", testOrigin)

        response := stage.call(retry)

        if response.Code != http.StatusUnauthorized {
            t.Fatalf("expected the ended chain to be refused, got %d", response.Code)
        }

        if codeOf(t, response) != auth.CodeTokenReused {
            t.Fatalf("expected %s, got %s", auth.CodeTokenReused, codeOf(t, response))
        }
    })

    t.Run("edge: signing out needs a token, or there is nothing to withdraw", func(t *testing.T) {
        stage := newHandlerStage(t)

        request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
        request.Header.Set("Origin", testOrigin)

        if response := stage.call(request); response.Code != http.StatusUnauthorized {
            t.Fatalf("expected 401, got %d", response.Code)
        }
    })

    t.Run("edge: a sign out from another site is refused", func(t *testing.T) {
        stage := newHandlerStage(t)

        signedIn := stage.signIn(t, contractParentEmail)

        request := carry(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil), signedIn)
        request.Header.Set("Origin", "http://somewhere-else.example.test")

        if response := stage.call(request); response.Code != http.StatusBadRequest {
            t.Fatalf("expected 400, got %d", response.Code)
        }
    })
}

func TestHandlerConstruction(t *testing.T) {
    t.Run("edge: a handler missing its service or its guard is refused rather than built", func(t *testing.T) {
        stage := newServiceStage(t)

        guard, err := auth.NewGuard(stage.signer, stage.denylist, auth.GuardSettings{AllowedOrigins: []string{testOrigin}})
        if err != nil {
            t.Fatalf("cannot build the guard: %v", err)
        }

        cookies := auth.NewCookieWriter(auth.CookieSettings{Secure: true})

        if _, err := auth.NewHandler(nil, cookies, guard); err == nil {
            t.Fatal("expected a handler with no service to be refused")
        }

        if _, err := auth.NewHandler(stage.service, cookies, nil); err == nil {
            t.Fatal("expected a handler with no guard to be refused")
        }
    })
}
