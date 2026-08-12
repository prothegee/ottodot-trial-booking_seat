package auth

import "errors"

// The failures this package can report.
//
// They are sentinel values rather than strings so a caller decides on identity
// with errors.Is and never on wording.
//
// No message carries an email, a name, or an identifier. These strings reach a
// log, and a log gets pasted into a chat window.
var (
    // ErrInvalidRequest means the request was refused before anything was read
    // or written.
    ErrInvalidRequest = errors.New("auth: the request is missing something it needs")

    // ErrTokenInvalid means the token did not verify: the shape was wrong, the
    // algorithm was not the one this service signs with, the signature did not
    // match, or a claim this service requires was absent.
    //
    // The four are one failure on purpose. Telling a caller which part failed
    // tells an attacker which part to fix next.
    ErrTokenInvalid = errors.New("auth: the token did not verify")

    // ErrTokenExpired means the token verified but its life is over. It is
    // separate from ErrTokenInvalid because it is the one failure the client
    // acts on: refresh, then retry the original call.
    ErrTokenExpired = errors.New("auth: the token is past its expiry")

    // ErrTokenReused means a refresh token that had already been spent was
    // presented again. The whole family is revoked by the time this is
    // reported, which signs the real parent out as well. That is the correct
    // trade: one of the two holders is a thief and this service cannot tell
    // which.
    ErrTokenReused = errors.New("auth: that refresh token was already spent, the family is now revoked")

    // ErrTokenNotFound means no stored refresh token carries that hash. A
    // token this service never issued, or one whose row is long gone.
    ErrTokenNotFound = errors.New("auth: no refresh token carries that hash")

    // ErrDuplicateToken means a refresh token already carries that hash, which
    // can only happen if the randomness behind it repeated.
    ErrDuplicateToken = errors.New("auth: a refresh token already carries that hash")

    // ErrNoSuchParent means the email and password together match no account.
    //
    // The two ways to earn it, an address nobody holds and a password that does
    // not match, are deliberately the same error. Telling them apart is telling
    // a caller which addresses have accounts here.
    //
    // It never reaches a client as its own code either. See FailureFor: the
    // answer on the wire is the generic refusal.
    ErrNoSuchParent = errors.New("auth: no parent carries that email")

    // ErrForbiddenRole means the identity is real and the route is not for it.
    ErrForbiddenRole = errors.New("auth: that role cannot reach this route")

    // ErrOriginRefused means a mutation arrived from somewhere other than the
    // origin this service serves. Cookies travel automatically, so this check
    // plus SameSite=Strict is what a cookie session costs.
    ErrOriginRefused = errors.New("auth: the request did not come from the origin this service serves")

    // ErrNotAuthenticated means no identity was established for a request that
    // needs one.
    ErrNotAuthenticated = errors.New("auth: this route needs a signed in parent")
)
