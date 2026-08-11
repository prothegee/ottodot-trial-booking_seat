package auth

import (
    "context"
    "errors"
    "fmt"
    "time"

    "ottodot-trial-booking/backend/internal/identifier"
)

// Settings is everything the service needs that is not a collaborator.
//
// The clock and the two minters are handed in rather than called directly, so a
// test pins every instant and every identifier it asserts on instead of reading
// them back out of the answer.
type Settings struct {
    // AccessTTL is how long an access token is believed. Short, because the
    // denylist is the only way to withdraw one early and its reach is bounded
    // by this number.
    AccessTTL time.Duration

    // RefreshTTL is how long one rotation link lives. Every use mints a new
    // one, so this is the gap a parent may leave between visits, not how long
    // a single token is worth stealing.
    RefreshTTL time.Duration

    // Clock is now. It defaults to time.Now.
    Clock func() time.Time

    // NewID mints an identifier for a token row and for a jti. It defaults to
    // identifier.NewUUIDv7.
    NewID func() (string, error)

    // NewToken mints one opaque refresh token. It defaults to
    // NewRefreshToken.
    NewToken func() (string, error)
}

// DefaultSettings is the lifetime pair this service runs with.
//
// Fifteen minutes and thirty days are the two numbers the design fixed. The
// short one bounds how long a withdrawn token keeps working when the denylist
// cannot be reached. The long one is how long a parent stays signed in across
// visits.
func DefaultSettings() Settings {
    return Settings{
        AccessTTL:  15 * time.Minute,
        RefreshTTL: 720 * time.Hour,
    }
}

// withDefaults fills in whatever the caller left out.
func (settings Settings) withDefaults() Settings {
    filled := settings

    if filled.AccessTTL <= 0 {
        filled.AccessTTL = DefaultSettings().AccessTTL
    }

    if filled.RefreshTTL <= 0 {
        filled.RefreshTTL = DefaultSettings().RefreshTTL
    }

    if filled.Clock == nil {
        filled.Clock = time.Now
    }

    if filled.NewID == nil {
        filled.NewID = identifier.NewUUIDv7
    }

    if filled.NewToken == nil {
        filled.NewToken = NewRefreshToken
    }

    return filled
}

// Issued is one signed in session, as the handler needs it to write cookies.
//
// Both token values are here and neither is stored by this service: the access
// token exists only in the response, and only the refresh token's hash reaches
// a row.
type Issued struct {
    AccessToken      string
    AccessExpiresAt  time.Time
    RefreshToken     string
    RefreshExpiresAt time.Time

    // Claims is what the access token carries, returned so a caller can log
    // the jti without decoding the token it just wrote.
    Claims Claims
}

// LogOutRequest is what ending a session needs.
//
// It carries the two facts about the access token and not the token itself.
// The middleware already verified it, and passing the whole thing back down
// would invite a second verification that could disagree with the first.
type LogOutRequest struct {
    // TokenID is the jti that goes on the denylist.
    TokenID string

    // TokenExpiry is how long the denylist entry has to stand. Past it the
    // signature no longer verifies, so the entry protects nothing.
    TokenExpiry time.Time

    // RefreshToken is the opaque token the request arrived with, when it
    // arrived with one. Empty is not a failure: see LogOut.
    RefreshToken string
}

// Validate refuses a sign out that could not withdraw anything.
func (request LogOutRequest) Validate() error {
    if request.TokenID == "" || request.TokenExpiry.IsZero() {
        return ErrInvalidRequest
    }

    return nil
}

// Service is the auth flow: sign in, rotate, sign out, and who am I.
//
// It owns the ordering and nothing else. Each collaborator owns its own rule,
// which is what keeps this file readable: the signer decides whether a token is
// real, the store decides whether a refresh token may be spent, and the
// directory decides who exists.
type Service struct {
    signer    *Signer
    store     RefreshStore
    directory Directory
    denylist  Denylist
    counters  *Counters
    settings  Settings
}

// NewService wires the flow.
//
// Param:
// signer - *Signer (issues and verifies access tokens)
// store - RefreshStore (where refresh tokens live)
// directory - Directory (who exists)
// denylist - Denylist (which access tokens have been withdrawn)
// settings - Settings (lifetimes, and the seams a test pins)
//
// Return:
//   - the service
//   - ErrInvalidRequest when a collaborator is missing, refused here rather
//     than at the first request that would have panicked
func NewService(
    signer *Signer,
    store RefreshStore,
    directory Directory,
    denylist Denylist,
    settings Settings,
) (*Service, error) {
    if signer == nil || store == nil || directory == nil || denylist == nil {
        return nil, ErrInvalidRequest
    }

    return &Service{
        signer:    signer,
        store:     store,
        directory: directory,
        denylist:  denylist,
        counters:  NewCounters(),
        settings:  settings.withDefaults(),
    }, nil
}

// Counters is what this service has counted, for the metrics endpoint and for
// the simulation that asserts reuse was noticed.
func (service *Service) Counters() *Counters {
    return service.counters
}

// LogIn signs a parent in by seeded email and starts a new token family.
//
// Note:
//   - there is no password. That is the brief's cut, not an oversight, and it
//     is the one method a real credential check would replace.
//   - an unknown address is counted and refused. It never says whether the
//     address exists, so this endpoint cannot be used to find out who has an
//     account here.
//
// Param:
// ctx - context.Context
// email - string (the seeded address, matched case insensitively)
//
// Return:
//   - both tokens and their expiries
//   - ErrInvalidRequest when the address is empty
//   - ErrNoSuchParent when it matches no account
func (service *Service) LogIn(ctx context.Context, email string) (Issued, error) {
    if normaliseEmail(email) == "" {
        return Issued{}, ErrInvalidRequest
    }

    parent, err := service.directory.ParentByEmail(ctx, email)
    if err != nil {
        if errors.Is(err, ErrNoSuchParent) {
            service.counters.LoginRefused()
        }

        return Issued{}, err
    }

    now := service.settings.Clock()

    familyID, err := service.settings.NewID()
    if err != nil {
        return Issued{}, fmt.Errorf("a token family could not be identified: %w", err)
    }

    refresh, err := service.issueRefresh(ctx, parent.ID, familyID, now)
    if err != nil {
        return Issued{}, err
    }

    return service.completeSession(parent, refresh, now)
}

// LogOut ends the session the request arrived with.
//
// Note:
//   - the jti goes on the denylist for exactly the token's remaining life. Not
//     longer, because after that instant the signature no longer verifies and
//     the entry protects nothing.
//   - a missing or unknown refresh token is not a failure. The parent asked to
//     be signed out, the handler clears both cookies either way, and refusing
//     here would leave them signed in because their refresh cookie had already
//     lapsed.
//
// Param:
// ctx - context.Context
// request - LogOutRequest (the claims to withdraw and the refresh token to end)
//
// Return:
//   - nil when the session is over
//   - ErrInvalidRequest when the request names no token to withdraw
//   - whatever the denylist reported, because a logout that could not withdraw
//     the access token has not done its job and must not answer as if it had
func (service *Service) LogOut(ctx context.Context, request LogOutRequest) error {
    if err := request.Validate(); err != nil {
        return err
    }

    if err := service.denylist.Deny(ctx, request.TokenID, request.TokenExpiry); err != nil {
        return fmt.Errorf("the access token could not be withdrawn: %w", err)
    }

    if request.RefreshToken == "" {
        return nil
    }

    held, err := service.store.Record(ctx, HashRefreshToken(request.RefreshToken))
    if err != nil {
        if errors.Is(err, ErrTokenNotFound) {
            return nil
        }

        return err
    }

    _, err = service.store.RevokeFamily(ctx, RevokeFamilyRequest{
        FamilyID: held.FamilyID,
        Now:      service.settings.Clock(),
    })

    return err
}

// Account is the session read: who this parent is and which children are on
// their account.
func (service *Service) Account(ctx context.Context, parentID string) (Account, error) {
    if parentID == "" {
        return Account{}, ErrInvalidRequest
    }

    return service.directory.Account(ctx, parentID)
}

// issuedRefresh is one freshly minted refresh token and what was stored for it.
type issuedRefresh struct {
    Token  string
    Record RefreshRecord
}

// issueRefresh mints one refresh token and writes its hash as the first of a
// family.
func (service *Service) issueRefresh(
    ctx context.Context,
    parentID string,
    familyID string,
    now time.Time,
) (issuedRefresh, error) {
    tokenID, err := service.settings.NewID()
    if err != nil {
        return issuedRefresh{}, fmt.Errorf("a refresh token could not be identified: %w", err)
    }

    token, err := service.settings.NewToken()
    if err != nil {
        return issuedRefresh{}, fmt.Errorf("a refresh token could not be minted: %w", err)
    }

    written, err := service.store.Issue(ctx, IssueRequest{
        TokenID:   tokenID,
        ParentID:  parentID,
        FamilyID:  familyID,
        TokenHash: HashRefreshToken(token),
        ExpiresAt: now.Add(service.settings.RefreshTTL),
        Now:       now,
    })
    if err != nil {
        return issuedRefresh{}, err
    }

    return issuedRefresh{Token: token, Record: written}, nil
}

// completeSession signs the access token that goes with a stored refresh token.
func (service *Service) completeSession(parent Parent, refresh issuedRefresh, now time.Time) (Issued, error) {
    accessID, err := service.settings.NewID()
    if err != nil {
        return Issued{}, fmt.Errorf("an access token could not be identified: %w", err)
    }

    claims := Claims{
        Subject:   parent.ID,
        Role:      parent.Role,
        Type:      TypeAccess,
        TokenID:   accessID,
        IssuedAt:  now.Unix(),
        ExpiresAt: now.Add(service.settings.AccessTTL).Unix(),
    }

    token, err := service.signer.Sign(claims)
    if err != nil {
        return Issued{}, err
    }

    return Issued{
        AccessToken:      token,
        AccessExpiresAt:  claims.Expiry(),
        RefreshToken:     refresh.Token,
        RefreshExpiresAt: refresh.Record.ExpiresAt,
        Claims:           claims,
    }, nil
}
