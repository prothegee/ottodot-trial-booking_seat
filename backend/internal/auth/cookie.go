package auth

import (
    "net/http"
    "time"
)

// The two cookie names. They are constants because the handler writes them and
// the middleware reads them, and a typo across those two files would be a
// service that signs everybody in and then cannot find them.
const (
    AccessCookieName  = "access_token"
    RefreshCookieName = "refresh_token"
)

// AccessCookiePath is every route, because the access token is what every
// business call is judged on.
const AccessCookiePath = "/"

// RefreshCookiePath is the auth group, and the scope is deliberate.
//
// The reason for scoping it at all is that a cookie is sent on every request
// matching its path, and a refresh token sent on every class list read is a
// refresh token exposed hundreds of times a session for no purpose. Scoping it
// here means it never travels on a business call.
//
// It is the group rather than the refresh route alone because logout has to
// revoke the family the presented token belongs to, and a cookie scoped to
// /api/v1/auth/refresh is not sent to /api/v1/auth/logout. Scoped to the route,
// logout could withdraw the access token and never end the chain behind it,
// which leaves a stolen refresh token working after the parent signed out.
// ADR-030 records the trade.
const RefreshCookiePath = "/api/v1/auth"

// CookieSettings is how the cookies are written for this environment.
type CookieSettings struct {
    // Domain is left empty for a host-only cookie, which is what a local run
    // and a single-host deployment both want.
    Domain string

    // Secure keeps the cookie off plain http. It is false only in development,
    // and configuration refuses to start with it false anywhere else.
    Secure bool
}

// CookieWriter puts the session on the response, and takes it off again.
//
// Every cookie it writes is HttpOnly, so no script in the page can read a token
// value. That is the whole reason tokens live in cookies here rather than in
// localStorage: an injected script can read storage, and it cannot read this.
type CookieWriter struct {
    settings CookieSettings
}

// NewCookieWriter builds the writer.
func NewCookieWriter(settings CookieSettings) CookieWriter {
    return CookieWriter{settings: settings}
}

// Write puts both cookies on the response.
//
// Note:
//   - SameSite=Strict means the browser does not send either cookie on a
//     request another site started. That plus the Origin check on mutations is
//     what a cookie session costs, and it is a smaller cost than a token any
//     script on the page could read.
//   - MaxAge comes from the token's own expiry rather than a separate number,
//     so a cookie cannot outlive what it carries.
//
// Param:
// response - http.ResponseWriter (the response being written)
// issued - Issued (both tokens and their expiries)
// now - time.Time (the instant the remaining life is measured from)
func (writer CookieWriter) Write(response http.ResponseWriter, issued Issued, now time.Time) {
    http.SetCookie(response, writer.cookie(
        AccessCookieName, issued.AccessToken, AccessCookiePath, secondsUntil(issued.AccessExpiresAt, now)))

    http.SetCookie(response, writer.cookie(
        RefreshCookieName, issued.RefreshToken, RefreshCookiePath, secondsUntil(issued.RefreshExpiresAt, now)))
}

// Clear takes both cookies off, which is what a sign out and a refused refresh
// both do.
//
// The value is emptied as well as the age negated. A browser that ignores one
// is left holding an empty string rather than a working token.
func (writer CookieWriter) Clear(response http.ResponseWriter) {
    http.SetCookie(response, writer.cookie(AccessCookieName, "", AccessCookiePath, -1))
    http.SetCookie(response, writer.cookie(RefreshCookieName, "", RefreshCookiePath, -1))
}

// cookie builds one cookie with the flags every cookie here carries.
func (writer CookieWriter) cookie(name string, value string, path string, maxAge int) *http.Cookie {
    return &http.Cookie{
        Name:     name,
        Value:    value,
        Path:     path,
        Domain:   writer.settings.Domain,
        MaxAge:   maxAge,
        HttpOnly: true,
        Secure:   writer.settings.Secure,
        SameSite: http.SameSiteStrictMode,
    }
}

// secondsUntil is the cookie's remaining life in whole seconds.
//
// A deadline already reached returns -1, which is the browser's instruction to
// drop the cookie. Returning 0 would mean a session cookie that survives until
// the tab closes, which is the opposite of what an expired token wants.
func secondsUntil(deadline time.Time, now time.Time) int {
    remaining := deadline.Sub(now)
    if remaining <= 0 {
        return -1
    }

    return int(remaining.Seconds())
}

// CookieValue reads one cookie off a request, returning an empty string when it
// is absent.
//
// Absence is not an error here. A first visit has no cookies, and a caller that
// needs one says so itself rather than unpicking a not-found from a read
// failure.
func CookieValue(request *http.Request, name string) string {
    found, err := request.Cookie(name)
    if err != nil {
        return ""
    }

    return found.Value
}
