package auth

import "errors"

// MetricSink is where this package's counts are published.
//
// It is an interface declared here rather than the metrics type itself, so this
// package can be tested without a Prometheus registry existing and so nothing
// about the session flow depends on a monitoring library.
//
// Nil is the ordinary state in a test and never the state in a running service.
type MetricSink interface {
    AccessDenied(reason string)
    TokenIssued(kind string)
    RefreshReuseDetected()
    RefreshRotated()
    LoginRefused()
}

// The reasons this package denies a request.
//
// They are declared here rather than imported so the session flow does not have
// to know where its numbers end up. The values match the ones the metric layer
// publishes, and the test that reads both is what keeps them the same.
const (
    reasonTokenExpired  = "token_expired"
    reasonTokenInvalid  = "token_invalid"
    reasonTokenReused   = "token_reused"
    reasonForbiddenRole = "forbidden_role"
    reasonOriginRefused = "origin_refused"

    tokenKindAccess  = "access"
    tokenKindRefresh = "refresh"
)

// denialReason maps a refusal to the label it is counted under.
//
// Anything unrecognised counts as an invalid token, which is the truthful
// default: the request did not carry an identity this service would believe, and
// exactly why is a detail the metric does not need to be precise about.
func denialReason(err error) string {
    switch {
    case errors.Is(err, ErrTokenExpired):
        return reasonTokenExpired
    case errors.Is(err, ErrTokenReused):
        return reasonTokenReused
    case errors.Is(err, ErrForbiddenRole):
        return reasonForbiddenRole
    case errors.Is(err, ErrOriginRefused):
        return reasonOriginRefused
    default:
        return reasonTokenInvalid
    }
}
