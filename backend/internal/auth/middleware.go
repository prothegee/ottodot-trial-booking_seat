package auth

import (
    "context"
    "net/http"
    "time"
)

// identityKey is the context key the established identity is carried under.
//
// It is an unexported type rather than a string, so nothing outside this
// package can write a value under it. A middleware is only worth having if the
// identity behind it cannot be forged by the handler it protects.
type identityKey struct{}

// Identity is who a request is, once its token has been believed.
//
// It is the claims and nothing more. A handler needing a display name or a
// child list asks the directory, because a middleware that loaded an account on
// every request would put a database read back on the path this design took it
// off.
type Identity struct {
    ParentID string
    Role     string
    TokenID  string

    // ExpiresAt is when this token stops verifying, which is exactly how long
    // a logout has to keep its id on the denylist. It is carried here so the
    // logout handler does not decode a token the middleware already read.
    ExpiresAt time.Time
}

// IsAdmin reports whether this identity may reach the admin routes.
func (identity Identity) IsAdmin() bool {
    return identity.Role == RoleAdmin
}

// IdentityFrom reads the identity a request was authenticated with.
//
// Return:
//   - the identity and true when the request came through Authenticate
//   - a zero identity and false otherwise, which a handler treats as a bug in
//     its own wiring rather than as an anonymous caller
func IdentityFrom(ctx context.Context) (Identity, bool) {
    held, carried := ctx.Value(identityKey{}).(Identity)

    return held, carried
}

// GuardSettings is what the middleware needs to decide.
type GuardSettings struct {
    // FrontendOrigin is the one origin this service serves. A mutation from
    // anywhere else is refused.
    FrontendOrigin string

    // Clock is now. It defaults to time.Now.
    Clock func() time.Time
}

// Guard is the request side of authentication: read the cookie, believe it or
// not, and decide whether this identity may go on.
//
// It holds no store and no directory. Verifying an access token is a signature
// check, a clock comparison, and a denylist lookup, and that is the whole
// reason the hot path costs no database read.
type Guard struct {
    signer   *Signer
    denylist Denylist
    settings GuardSettings
}

// NewGuard wires the middleware.
//
// Param:
// signer - *Signer (verifies the access token)
// denylist - Denylist (which token ids have been withdrawn)
// settings - GuardSettings (the origin to accept, and the clock)
//
// Return:
//   - the guard
//   - ErrInvalidRequest when a collaborator or the origin is missing, refused
//     here rather than at the first request that would have let everything
//     through
func NewGuard(signer *Signer, denylist Denylist, settings GuardSettings) (*Guard, error) {
    if signer == nil || denylist == nil || settings.FrontendOrigin == "" {
        return nil, ErrInvalidRequest
    }

    if settings.Clock == nil {
        settings.Clock = time.Now
    }

    return &Guard{signer: signer, denylist: denylist, settings: settings}, nil
}

// Authenticate establishes the identity behind a request, or refuses it.
//
// The order is the point:
//
//	the cookie is read, and its absence is the same refusal as a bad token
//	the signature and expiry are checked, which costs no database read
//	the jti is checked against the denylist, which is what makes logout real
//
// The denylist is last because it is the only step that can reach out of the
// process, and there is no reason to reach anywhere for a token that did not
// verify in the first place.
//
// Param:
// next - http.Handler (what runs once the identity is established)
//
// Return:
//   - a handler that either calls next with an identity in its context, or
//     answers the envelope itself
func (guard *Guard) Authenticate(next http.Handler) http.Handler {
    return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        identity, err := guard.identify(request)
        if err != nil {
            Deny(response, err)

            return
        }

        next.ServeHTTP(response, request.WithContext(
            context.WithValue(request.Context(), identityKey{}, identity)))
    })
}

// RequireRole refuses an established identity that is not the role a route is
// for.
//
// It wraps Authenticate rather than replacing it, so an admin route cannot be
// wired in a way that checks the role without checking the token.
//
// Param:
// role - string (the role this route is for)
// next - http.Handler (what runs when the identity carries it)
//
// Return:
//   - a handler that authenticates first and then checks the role
func (guard *Guard) RequireRole(role string, next http.Handler) http.Handler {
    return guard.Authenticate(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        identity, carried := IdentityFrom(request.Context())

        if !carried || identity.Role != role {
            // The refusal names no role and no route. A caller learning which
            // role would have worked is a caller learning what to go looking
            // for.
            Deny(response, ErrForbiddenRole)

            return
        }

        next.ServeHTTP(response, request)
    }))
}

// CheckOrigin refuses a mutation that did not come from the origin this service
// serves.
//
// This is the cost of session cookies. A browser sends them automatically, so
// another site can make a parent's browser issue a write, and SameSite=Strict
// is the first defence rather than the only one: it is a browser behaviour, and
// this check does not depend on the browser getting it right.
//
// A safe method is left alone. A GET changes nothing, and requiring the header
// on reads would refuse the very first page load.
//
// A mutation with no Origin at all is refused. Every browser sends one on a
// write, so an absent header means a caller that is not the browser this
// service serves.
//
// Param:
// next - http.Handler (what runs when the origin is this service's own)
//
// Return:
//   - a handler that checks writes and passes reads straight through
func (guard *Guard) CheckOrigin(next http.Handler) http.Handler {
    return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        if isSafeMethod(request.Method) {
            next.ServeHTTP(response, request)

            return
        }

        if request.Header.Get("Origin") != guard.settings.FrontendOrigin {
            Deny(response, ErrOriginRefused)

            return
        }

        next.ServeHTTP(response, request)
    })
}

// identify reads the access cookie and decides whether to believe it.
//
// Return:
//   - the identity when the token verified and has not been withdrawn
//   - ErrTokenInvalid when there was no cookie, or it did not verify
//   - ErrTokenExpired when it verified and its life is over, which is the one
//     failure the client acts on by refreshing
func (guard *Guard) identify(request *http.Request) (Identity, error) {
    token := CookieValue(request, AccessCookieName)
    if token == "" {
        return Identity{}, ErrTokenInvalid
    }

    now := guard.settings.Clock()

    claims, err := guard.signer.Verify(token, now)
    if err != nil {
        return Identity{}, err
    }

    withdrawn, err := guard.denylist.IsDenied(request.Context(), claims.TokenID, now)
    if err != nil {
        // A denylist that cannot be read is a denylist that cannot say no. The
        // service refuses rather than guessing, because guessing here means
        // honouring a token somebody has already signed out of.
        return Identity{}, err
    }

    if withdrawn {
        return Identity{}, ErrTokenInvalid
    }

    return Identity{
        ParentID:  claims.Subject,
        Role:      claims.Role,
        TokenID:   claims.TokenID,
        ExpiresAt: claims.Expiry(),
    }, nil
}

// isSafeMethod reports whether a method changes nothing.
func isSafeMethod(method string) bool {
    return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
