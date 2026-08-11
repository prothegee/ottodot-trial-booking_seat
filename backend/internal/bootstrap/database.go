package bootstrap

import (
    "context"
    "fmt"

    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/database"
)

// OpenDatabase opens both pools from the loaded configuration.
//
// It pings eagerly, which is what turns a wrong address into a startup failure
// with a readable message rather than a confusing one on the first parent's
// booking.
//
// Note:
//   - the two pools are separate values rather than one address with a flag,
//     because which one a read goes to is a decision each call site makes and
//     not something to be configured centrally. Everything that decides reads
//     the primary, and only the advisory reads may use the replica.
//
// Param:
// ctx - context.Context (the startup deadline, so a wrong address fails rather than hangs)
// settings - config.Config (both addresses, the pool size, and the connect timeout)
//
// Return:
//   - the pools, which the caller closes
//   - an error naming the database as unreachable, with nothing from the driver
//     wrapped into it, because a connection string carries a password
func OpenDatabase(ctx context.Context, settings config.Config) (*database.Pools, error) {
    pools, err := database.Open(ctx, database.Settings{
        PrimaryURL:     settings.Database.PrimaryURL.Reveal(),
        ReplicaURL:     settings.Database.ReplicaURL.Reveal(),
        MaxConnections: settings.Database.MaxConnections,
        ConnectTimeout: settings.Database.ConnectTimeout,
    })
    if err != nil {
        return nil, fmt.Errorf("the database is not reachable: %w", err)
    }

    return pools, nil
}
