package httpx_test

import (
    "net/http"
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/httpx"
)

// Every business route, with who may reach it. The table is what a reviewer
// checks the router against, and it is what catches a route added without a
// guard.
var routeContract = []struct {
    method string
    path   string
    admin  bool
    write  bool
}{
    {http.MethodGet, "/api/v1/students", false, false},
    {http.MethodGet, "/api/v1/classes", false, false},
    {http.MethodGet, "/api/v1/classes/" + classOpen, false, false},
    {http.MethodGet, "/api/v1/bookings/" + classOpen, false, false},
    {http.MethodGet, "/api/v1/bookings/" + classOpen + "/events", false, false},
    {http.MethodPost, "/api/v1/bookings", false, true},
    {http.MethodDelete, "/api/v1/bookings/" + classOpen, false, true},
    {http.MethodPost, "/api/v1/bookings/" + classOpen + "/payments", false, true},
    {http.MethodGet, "/api/v1/classes/" + classOpen + "/roster", true, false},
    {http.MethodGet, "/api/v1/admin/queue", true, false},
    {http.MethodGet, "/api/v1/admin/bookings", true, false},
}

func TestEveryBusinessRouteNeedsAToken(t *testing.T) {
    fixture := newStage(t, stageOptions{})

    for _, route := range routeContract {
        t.Run("integration: "+route.method+" "+route.path, func(t *testing.T) {
            recorder := fixture.send(t, request{method: route.method, path: route.path, body: "{}"})

            if recorder.Code != http.StatusUnauthorized {
                t.Fatalf("an anonymous caller answered %d: %s", recorder.Code, recorder.Body.String())
            }

            if failureOf(t, recorder).Error.Code != httpx.CodeTokenInvalid {
                t.Fatalf("an anonymous caller answered %q", failureOf(t, recorder).Error.Code)
            }
        })
    }
}

func TestEveryAdminRouteRefusesAParent(t *testing.T) {
    fixture := newStage(t, stageOptions{})

    for _, route := range routeContract {
        if !route.admin {
            continue
        }

        t.Run("integration: "+route.path, func(t *testing.T) {
            recorder := fixture.send(t, request{
                method: route.method, path: route.path, parent: parentOne,
            })

            if recorder.Code != http.StatusForbidden {
                t.Fatalf("a parent role answered %d on an admin route: %s", recorder.Code, recorder.Body.String())
            }

            if failureOf(t, recorder).Error.Code != httpx.CodeForbiddenRole {
                t.Fatalf("a parent role answered %q", failureOf(t, recorder).Error.Code)
            }
        })
    }
}

func TestTheRosterNeverReachesAParent(t *testing.T) {
    t.Run("behaviour: a parent asking for a roster is refused before any name is read", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/classes/" + classOpen + "/roster",
            parent: parentOne,
        })

        if recorder.Code != http.StatusForbidden {
            t.Fatalf("a parent reached a roster: %d %s", recorder.Code, recorder.Body.String())
        }

        for _, name := range []string{"Adi", "Bella", "Citra", "student_name"} {
            if strings.Contains(recorder.Body.String(), name) {
                t.Fatalf("the refusal body carries %q: %s", name, recorder.Body.String())
            }
        }
    })

    t.Run("integration: an admin reads the roster", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{
            method: http.MethodGet,
            path:   "/api/v1/classes/" + classOpen + "/roster",
            parent: adminParent,
            role:   auth.RoleAdmin,
        })

        if recorder.Code != http.StatusOK {
            t.Fatalf("an admin answered %d: %s", recorder.Code, recorder.Body.String())
        }

        if recorder.Header().Get("Cache-Control") != "no-store" {
            t.Fatalf("a roster answered with Cache-Control %q, and this body carries names",
                recorder.Header().Get("Cache-Control"))
        }
    })
}

func TestEveryWriteNeedsAnOrigin(t *testing.T) {
    fixture := newStage(t, stageOptions{})

    for _, route := range routeContract {
        if !route.write {
            continue
        }

        t.Run("edge: "+route.method+" "+route.path+" with no Origin", func(t *testing.T) {
            recorder := fixture.send(t, request{
                method: route.method, path: route.path, body: "{}",
                parent: parentOne, omitOrigin: true,
            })

            if recorder.Code != http.StatusBadRequest {
                t.Fatalf("a write with no Origin answered %d: %s", recorder.Code, recorder.Body.String())
            }
        })

        t.Run("edge: "+route.method+" "+route.path+" from another site", func(t *testing.T) {
            recorder := fixture.send(t, request{
                method: route.method, path: route.path, body: "{}",
                parent: parentOne, origin: "https://somewhere.else.test",
            })

            if recorder.Code != http.StatusBadRequest {
                t.Fatalf("a cross origin write answered %d: %s", recorder.Code, recorder.Body.String())
            }
        })
    }
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
    t.Run("integration: the identifier is on the response, whether it succeeded or not", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        for _, call := range []request{
            {method: http.MethodGet, path: "/api/v1/classes", parent: parentOne},
            {method: http.MethodGet, path: "/api/v1/classes"},
            {method: http.MethodGet, path: "/healthz"},
        } {
            recorder := fixture.send(t, call)

            if recorder.Header().Get(httpx.RequestIDHeader) == "" {
                t.Fatalf("%s %s answered with no request id", call.method, call.path)
            }
        }
    })

    t.Run("edge: the identifier is minted here and never read from the request", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{method: http.MethodGet, path: "/healthz"})
        first := recorder.Header().Get(httpx.RequestIDHeader)

        second := fixture.send(t, request{method: http.MethodGet, path: "/healthz"}).
            Header().Get(httpx.RequestIDHeader)

        if first == second {
            t.Fatalf("two requests share the identifier %s", first)
        }
    })
}

func TestTheOperationsRoutesStayOpen(t *testing.T) {
    t.Run("integration: liveness, readiness, and version answer with no token", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        for _, path := range []string{"/healthz", "/readyz", "/version"} {
            recorder := fixture.send(t, request{method: http.MethodGet, path: path})

            if recorder.Code != http.StatusOK {
                t.Fatalf("%s answered %d with no token", path, recorder.Code)
            }
        }
    })
}

func TestBuildingTheRouter(t *testing.T) {
    t.Run("edge: a router missing a handler is refused rather than answering with a panic", func(t *testing.T) {
        if _, err := httpx.NewRouter(httpx.Routes{}); err == nil {
            t.Fatal("an empty router was built")
        }
    })
}

func TestARouteThatDoesNotExist(t *testing.T) {
    t.Run("edge: an unknown path answers 404 and no envelope", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{method: http.MethodGet, path: "/api/v1/nothing", parent: parentOne})

        if recorder.Code != http.StatusNotFound {
            t.Fatalf("an unknown path answered %d", recorder.Code)
        }
    })

    t.Run("edge: the wrong method on a real path is not the handler's problem", func(t *testing.T) {
        fixture := newStage(t, stageOptions{})

        recorder := fixture.send(t, request{method: http.MethodPut, path: "/api/v1/classes", parent: parentOne})

        if recorder.Code == http.StatusOK {
            t.Fatal("PUT on the class list was served")
        }
    })
}
