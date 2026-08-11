package bootstrap

import (
    "context"
    "errors"

    "github.com/redis/go-redis/v9"

    "ottodot-trial-booking/backend/internal/config"
)

// OpenRedis connects to the shared store.
//
// It pings eagerly for the same reason the pools do: a wrong address should be a
// startup failure with a readable message, not a confusing one on the first
// parent's booking.
//
// Note:
//   - Redis is never a source of truth in this service. It holds the response
//     cache, the rate limit buckets, and the access token denylist, and only the
//     last of those is allowed to refuse a request when it is unreachable.
//
// Param:
// ctx - context.Context (the startup deadline)
// settings - config.RedisSettings (the address, the password, and the database number)
//
// Return:
//   - the client, which the caller closes
//   - an error saying only that Redis is not reachable. The driver's own error
//     can echo the address it was dialling, and an address carries a password,
//     so nothing from it is wrapped into what is printed
func OpenRedis(ctx context.Context, settings config.RedisSettings) (redis.UniversalClient, error) {
    client := redis.NewClient(&redis.Options{
        Addr:     settings.Address,
        Password: settings.Password.Reveal(),
        DB:       settings.Database,
    })

    if err := client.Ping(ctx).Err(); err != nil {
        _ = client.Close()

        return nil, errors.New("redis is not reachable")
    }

    return client, nil
}
