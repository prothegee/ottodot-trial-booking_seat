package auth

import (
	"encoding/json"
	"io"
	"net/http"
)

// The four routes this package serves. They are constants because the frontend
// names the same four, and a path that exists in one and not the other is a
// sign in screen that does nothing.
const (
	LoginPath   = "POST /api/v1/auth/login"
	RefreshPath = "POST /api/v1/auth/refresh"
	LogoutPath  = "POST /api/v1/auth/logout"
	MePath      = "GET /api/v1/auth/me"
)

// maximumBodyBytes caps what a request body may be. The only body any of these
// routes reads is one email, so anything larger is either a mistake or someone
// seeing how much memory a request can be made to hold.
const maximumBodyBytes = 4 * 1024

// loginRequest is the sign in body. One field, because there is no password.
type loginRequest struct {
	Email string `json:"email"`
}

// sessionResponse is the answer to the session read.
//
// The field names are the wire names the frontend already reads. There is no
// email here and no token: the client is told who it is signed in as and what
// it may do, and nothing it could leak.
type sessionResponse struct {
	ParentID    string          `json:"parent_id"`
	DisplayName string          `json:"display_name"`
	Role        string          `json:"role"`
	Children    []childResponse `json:"children"`
}

// childResponse is one student on the account.
type childResponse struct {
	ID         string `json:"id"`
	FullName   string `json:"full_name"`
	GradeLevel int16  `json:"grade_level"`
}

// Handler serves the four auth routes.
//
// It owns the http shape and nothing else: reading a body, writing cookies, and
// choosing a status. Every decision behind those belongs to the service.
type Handler struct {
	service *Service
	cookies CookieWriter
	guard   *Guard
}

// NewHandler wires the routes.
//
// Param:
// service - *Service (the flow behind the routes)
// cookies - CookieWriter (how the session is put on a response)
// guard - *Guard (what protects the two routes that need an identity)
//
// Return:
//   - the handler
//   - ErrInvalidRequest when the service or the guard is missing
func NewHandler(service *Service, cookies CookieWriter, guard *Guard) (*Handler, error) {
	if service == nil || guard == nil {
		return nil, ErrInvalidRequest
	}

	return &Handler{service: service, cookies: cookies, guard: guard}, nil
}

// Register puts the four routes on a mux.
//
// Note:
//   - all three writes go through the origin check. A cookie session means the
//     browser attaches the token itself, so a write another site started would
//     otherwise carry a real identity.
//   - logout and the session read go through authentication, so neither can be
//     reached without a token. Logout needs one in particular: without the jti
//     there is nothing to withdraw.
//
// Param:
// mux - *http.ServeMux (where the routes are registered)
func (handler *Handler) Register(mux *http.ServeMux) {
	mux.Handle(LoginPath, handler.guard.CheckOrigin(http.HandlerFunc(handler.logIn)))
	mux.Handle(RefreshPath, handler.guard.CheckOrigin(http.HandlerFunc(handler.refresh)))
	mux.Handle(LogoutPath, handler.guard.CheckOrigin(
		handler.guard.Authenticate(http.HandlerFunc(handler.logOut))))
	mux.Handle(MePath, handler.guard.Authenticate(http.HandlerFunc(handler.me)))
}

// logIn signs a parent in by seeded email and writes both cookies.
func (handler *Handler) logIn(response http.ResponseWriter, request *http.Request) {
	var body loginRequest

	if err := decodeBody(request, &body); err != nil {
		Deny(response, err)

		return
	}

	issued, err := handler.service.LogIn(request.Context(), body.Email)
	if err != nil {
		Deny(response, err)

		return
	}

	handler.cookies.Write(response, issued, handler.service.settings.Clock())

	noContent(response)
}

// refresh rotates the refresh token and re-issues the access token.
//
// A refusal clears both cookies. The parent is going back to the sign in screen
// either way, and leaving a spent refresh token in the browser means every
// later call tries it again and fails the same way.
func (handler *Handler) refresh(response http.ResponseWriter, request *http.Request) {
	presented := CookieValue(request, RefreshCookieName)

	issued, err := handler.service.Refresh(request.Context(), presented)
	if err != nil {
		handler.cookies.Clear(response)
		Deny(response, err)

		return
	}

	handler.cookies.Write(response, issued, handler.service.settings.Clock())

	noContent(response)
}

// logOut withdraws the access token, ends the token family, and clears both
// cookies.
//
// The cookies are cleared even when the service reports a failure. A parent who
// pressed sign out must not be left holding a working token because the store
// was unreachable, and the withdrawal is the part that is retried by the
// service, not by the browser.
func (handler *Handler) logOut(response http.ResponseWriter, request *http.Request) {
	identity, carried := IdentityFrom(request.Context())
	if !carried {
		Deny(response, ErrNotAuthenticated)

		return
	}

	err := handler.service.LogOut(request.Context(), LogOutRequest{
		TokenID:      identity.TokenID,
		TokenExpiry:  identity.ExpiresAt,
		RefreshToken: CookieValue(request, RefreshCookieName),
	})

	handler.cookies.Clear(response)

	if err != nil {
		Deny(response, err)

		return
	}

	noContent(response)
}

// me answers who the request is signed in as.
func (handler *Handler) me(response http.ResponseWriter, request *http.Request) {
	identity, carried := IdentityFrom(request.Context())
	if !carried {
		Deny(response, ErrNotAuthenticated)

		return
	}

	account, err := handler.service.Account(request.Context(), identity.ParentID)
	if err != nil {
		Deny(response, err)

		return
	}

	writeJSON(response, http.StatusOK, sessionFrom(account))
}

// sessionFrom shapes an account for the wire.
//
// The children slice is built empty rather than left nil, so an account with no
// children answers with [] and the client renders an empty list instead of
// guarding against null.
func sessionFrom(account Account) sessionResponse {
	children := make([]childResponse, 0, len(account.Children))

	for _, child := range account.Children {
		children = append(children, childResponse{
			ID:         child.ID,
			FullName:   child.FullName,
			GradeLevel: child.GradeLevel,
		})
	}

	return sessionResponse{
		ParentID:    account.Parent.ID,
		DisplayName: account.Parent.DisplayName,
		Role:        account.Parent.Role,
		Children:    children,
	}
}

// decodeBody reads one json body, capped and strict.
//
// Unknown fields are refused rather than ignored. A client sending `password`
// to a service that has none should be told, not quietly signed in as if the
// field had been checked.
func decodeBody(request *http.Request, into any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(into); err != nil {
		return ErrInvalidRequest
	}

	return nil
}

// noContent answers a write that succeeded and has nothing to say.
//
// The cookies are the answer, so a body would be a second copy of something the
// client is told never to read.
func noContent(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

// writeJSON answers with a body.
//
// Every auth route is no-store. None of it is shared between parents, and a
// cached session read is one parent's account served to the next person on a
// shared machine.
func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)

	_ = json.NewEncoder(response).Encode(body)
}
