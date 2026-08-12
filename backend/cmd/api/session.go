package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/auth"
    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
)

// session is the authentication half, assembled.
type session struct {
    guard   *auth.Guard
    handler *auth.Handler

    // directory is who exists and which children are on which account.
    //
    // It is built once here and shared, rather than built again wherever it is
    // needed. Three things read it: sign in, the ownership check, and the route
    // that lists a parent's children, and all three have to agree about what an
    // account holds.
    directory auth.Directory
}

// buildSession wires sign in, refresh, sign out, and who am I.
//
// Every store here is the primary pool, and that is the one decision in this
// file worth stopping on. A refresh token lookup decides whether a token has
// already been spent, and a revoked token that still works because a replica is
// a second behind is a security hole rather than a display quirk.
//
// Param:
// deps - *dependencies (the primary pool and the Redis client)
// watch - bootstrap.Observability (where denials and rotations are counted)
// settings - config.Config (the signing secret, the lifetimes, and the cookie policy)
//
// Return:
//   - the guard and the routes
//   - an error naming the piece that could not be built
func buildSession(deps *dependencies, watch bootstrap.Observability, settings config.Config) (session, error) {
    signer, err := auth.NewSigner(settings.Auth.JWTSecret.Reveal())
    if err != nil {
        return session{}, fmt.Errorf("the token signer: %w", err)
    }

    denylist, err := auth.NewRedisDenylist(deps.redis)
    if err != nil {
        return session{}, fmt.Errorf("the token denylist: %w", err)
    }

    directory := auth.NewPostgresDirectory(deps.pools.Primary())

    authSettings := auth.DefaultSettings()
    authSettings.AccessTTL = settings.Auth.AccessTokenTTL
    authSettings.RefreshTTL = settings.Auth.RefreshTokenTTL
    authSettings.Metrics = watch.Metrics

    service, err := auth.NewService(
        signer,
        auth.NewPostgresRefreshStore(deps.pools.Primary()),
        directory,
        denylist,
        authSettings)
    if err != nil {
        return session{}, fmt.Errorf("the auth service: %w", err)
    }

    guard, err := auth.NewGuard(signer, denylist, auth.GuardSettings{
        AllowedOrigins: settings.AllowedOrigins,
        Metrics:        watch.Metrics,
    })
    if err != nil {
        return session{}, fmt.Errorf("the request guard: %w", err)
    }

    cookies := auth.NewCookieWriter(auth.CookieSettings{
        Domain: settings.Auth.CookieDomain,
        Secure: settings.Auth.CookieSecure,
    })

    handler, err := auth.NewHandler(service, cookies, guard)
    if err != nil {
        return session{}, fmt.Errorf("the auth routes: %w", err)
    }

    return session{guard: guard, handler: handler, directory: directory}, nil
}
