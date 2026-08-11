package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/cache"
    "ottodot-trial-booking/backend/internal/captcha"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/ratelimit"
)

// guards are the things every route passes through on its way to a handler: the
// response cache, the two token buckets, the ownership check, and the
// cooperative bot checks.
//
// They are together in one file because they share the counters. Every one of
// them either refuses a request or avoids work, and both of those are numbers an
// operator reads off the same dashboard row.
type guards struct {
    counters    *httpx.Counters
    conditional *httpx.Conditional
    limits      *httpx.Limits
    owner       *httpx.Owner
    botCheck    *httpx.BotCheck
    store       *cache.RedisStore
}

// buildGuards wires everything that runs before a handler does.
//
// Param:
// deps - *dependencies (the Redis client)
// watch - bootstrap.Observability (where every refusal is counted)
// signedIn - session (the directory the ownership check reads accounts from)
// decided - deciding (the booking service the ownership check reads through)
//
// Return:
//   - the guards, including the cache store the fault surface reaches into
//   - an error naming the piece that could not be built
func buildGuards(deps *dependencies, watch bootstrap.Observability, signedIn session, decided deciding) (guards, error) {
    counters := httpx.NewCounters(watch.Metrics)

    store, err := cache.NewRedisStore(deps.redis)
    if err != nil {
        return guards{}, fmt.Errorf("the response cache: %w", err)
    }

    conditional, err := httpx.NewConditional(store, cache.DefaultLifetime, counters)
    if err != nil {
        return guards{}, fmt.Errorf("the conditional reads: %w", err)
    }

    limiter, err := ratelimit.NewRedisLimiter(deps.redis)
    if err != nil {
        return guards{}, fmt.Errorf("the rate limiter: %w", err)
    }

    limits, err := httpx.NewLimits(limiter, nil, counters)
    if err != nil {
        return guards{}, fmt.Errorf("the rate limit middleware: %w", err)
    }

    // The directory is the session's, and it reads the primary. An ownership
    // check that answered from a replica could refuse a child who was added a
    // second ago, and could accept one who was removed a second ago, and the
    // second of those is the one that matters.
    owner, err := httpx.NewOwner(signedIn.directory, decided.bookings, counters)
    if err != nil {
        return guards{}, fmt.Errorf("the ownership check: %w", err)
    }

    botCheck, err := httpx.NewBotCheck(httpx.BotCheckSettings{
        Verifier: captcha.NewMockVerifier(),
    }, counters)
    if err != nil {
        return guards{}, fmt.Errorf("the bot check: %w", err)
    }

    return guards{
        counters:    counters,
        conditional: conditional,
        limits:      limits,
        owner:       owner,
        botCheck:    botCheck,
        store:       store,
    }, nil
}
