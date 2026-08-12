package main

import (
    "context"
    "fmt"

    "ottodot-trial-booking/backend/internal/operations"
)

// buildOperations wires liveness, readiness, and build identity.
//
// Param:
// deps - *dependencies (the two pools and the Redis client, all three probed)
// identity - operations.Identity (already resolved, so this route and the
// startup log line cannot disagree about which build is running)
//
// Return:
//   - the handler for the three unauthenticated routes
//   - an error when there is nothing to probe, refused here rather than as a
//     route that answers ready no matter what is broken
func buildOperations(deps *dependencies, identity operations.Identity) (*operations.Handler, error) {
    readiness, err := operations.NewReadiness(readinessChecks(deps))
    if err != nil {
        return nil, fmt.Errorf("readiness: %w", err)
    }

    handler, err := operations.NewHandler(readiness, identity)
    if err != nil {
        return nil, fmt.Errorf("the operations routes: %w", err)
    }

    return handler, nil
}

// readinessChecks is what /readyz probes, and which failures take this service
// out of rotation.
//
// The replica is advisory. Every deciding read already goes to the primary, so a
// replica that is down costs nothing that matters, and reporting unready would
// take a working service out for no reason.
//
// The primary and Redis are required, for different reasons. Without the primary
// no seat can be decided. Without Redis the access token denylist cannot say no,
// which means a token somebody has already signed out of would still be
// believed, and that is worse than being out of rotation.
func readinessChecks(deps *dependencies) []operations.Dependency {
    return []operations.Dependency{
        {
            Name:     "postgres_primary",
            Required: true,
            Probe:    deps.pools.PingPrimary,
        },
        {
            Name:     "postgres_replica",
            Required: false,
            Probe:    deps.pools.PingReplica,
        },
        {
            Name:     "redis",
            Required: true,
            Probe: func(ctx context.Context) error {
                return deps.redis.Ping(ctx).Err()
            },
        },
    }
}
