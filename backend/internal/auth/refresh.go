package auth

import (
    "context"
    "errors"
    "fmt"
)

// Refresh spends the presented refresh token and issues its successor.
//
// Rotation on every use is what makes a stolen refresh token detectable. A
// token that is never rotated is either valid or not, and a thief holding a
// copy is indistinguishable from the parent. Rotating means there is only ever
// one live link in the chain, so the moment two parties present the same one,
// this service knows.
//
// The consequence is stated rather than hidden: reuse revokes the whole family,
// which signs the real parent out as well. That is the correct trade. One of
// the two holders stole the token, this service cannot tell which, and the
// alternative is leaving the thief signed in.
//
// The access token this returns is signed with the role read fresh from the
// directory rather than carried over. A parent whose role changed gets the new
// one at the next refresh, which bounds how long a stale role survives to one
// access token lifetime.
//
// Param:
// ctx - context.Context
// presented - string (the opaque token the request arrived with)
//
// Return:
//   - a new pair, the old refresh token now spent
//   - ErrInvalidRequest when nothing was presented
//   - ErrTokenNotFound when no stored token carries that hash
//   - ErrTokenExpired when the presented token is past its life
//   - ErrTokenReused when it had already been spent, the family now revoked
func (service *Service) Refresh(ctx context.Context, presented string) (Issued, error) {
    if presented == "" {
        return Issued{}, ErrInvalidRequest
    }

    now := service.settings.Clock()

    nextID, err := service.settings.NewID()
    if err != nil {
        return Issued{}, fmt.Errorf("a refresh token could not be identified: %w", err)
    }

    nextToken, err := service.settings.NewToken()
    if err != nil {
        return Issued{}, fmt.Errorf("a refresh token could not be minted: %w", err)
    }

    successor, err := service.store.Rotate(ctx, RotateRequest{
        PresentedHash: HashRefreshToken(presented),
        NextTokenID:   nextID,
        NextTokenHash: HashRefreshToken(nextToken),
        NextExpiresAt: now.Add(service.settings.RefreshTTL),
        Now:           now,
    })
    if err != nil {
        // The store has already revoked the family by the time it reports
        // reuse. Counting it here is what puts the event on a dashboard, and it
        // is the one auth number worth alerting on.
        if errors.Is(err, ErrTokenReused) {
            service.counters.ReuseDetected()
        }

        return Issued{}, err
    }

    account, err := service.directory.Account(ctx, successor.ParentID)
    if err != nil {
        return Issued{}, err
    }

    issued, err := service.completeSession(
        account.Parent,
        issuedRefresh{Token: nextToken, Record: successor},
        now)
    if err != nil {
        return Issued{}, err
    }

    service.counters.Rotated()

    return issued, nil
}
