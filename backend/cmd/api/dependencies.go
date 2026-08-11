package main

import (
    "context"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/database"
)

// dependencies are the two stores this process holds open for its whole life.
//
// They are one struct rather than two return values so closing them is one call
// that cannot half happen. A process that closed its pools and leaked its Redis
// connection would look like a clean shutdown from the outside.
type dependencies struct {
    pools *database.Pools
    redis redis.UniversalClient
}

// openDependencies connects to both stores or fails to start.
//
// The order is the cheap one first. If the database is unreachable there is no
// point dialling Redis, and reporting the database is more useful than reporting
// whichever one happened to be tried first.
//
// Param:
// ctx - context.Context (the startup deadline)
// settings - config.Config (both sets of addresses)
//
// Return:
//   - the open dependencies, which the caller closes
//   - the first failure, named for the store that could not be reached
func openDependencies(ctx context.Context, settings config.Config) (*dependencies, error) {
    pools, err := bootstrap.OpenDatabase(ctx, settings)
    if err != nil {
        return nil, err
    }

    client, err := bootstrap.OpenRedis(ctx, settings.Redis)
    if err != nil {
        pools.Close()

        return nil, err
    }

    return &dependencies{pools: pools, redis: client}, nil
}

// close gives both stores back.
//
// Neither failure is reported. This runs after the listener has already stopped
// accepting, so there is nobody left to tell and nothing left to do about it.
func (deps *dependencies) close() {
    deps.pools.Close()

    _ = deps.redis.Close()
}
