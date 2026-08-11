// Package database owns the connection pools and nothing else. No query lives
// here, because a package that both connects and queries gives every caller a
// reason to reach past its own repository.
//
// There are two pools rather than one, and they are not interchangeable:
//
// The primary is the only place a decision may be made. The seat count inside a
// confirm transaction, a refresh token lookup, and a parent reading their own
// booking straight after acting all go here.
//
// The replica is advisory. Replication is asynchronous, so anything read from
// it may be a little behind, which is fine for a class list and wrong for a
// seat.
package database

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

// Settings describes both pools. The two addresses are separate values so a
// caller cannot accidentally point deciding reads at the replica.
type Settings struct {
    PrimaryURL     string
    ReplicaURL     string
    MaxConnections int32
    ConnectTimeout time.Duration
}

// Pools holds the two connection pools for the lifetime of the process.
type Pools struct {
    primary *pgxpool.Pool
    replica *pgxpool.Pool
}

// Primary returns the pool every deciding read and every write must use.
func (pools *Pools) Primary() *pgxpool.Pool {
    return pools.primary
}

// Replica returns the pool for advisory reads only. It may lag behind the
// primary.
func (pools *Pools) Replica() *pgxpool.Pool {
    return pools.replica
}

// Open builds both pools and confirms each one can reach its server.
//
// It connects eagerly rather than lazily, so a wrong address is a startup
// failure with a clear message instead of a confusing error on the first
// booking.
//
// Param:
// ctx - context.Context (cancelling it aborts the connection attempt)
// settings - Settings (both addresses, pool size, and the connect timeout)
//
// Return:
//   - the open pools, which the caller must Close
//   - an error naming which of the two failed, with no credential in the text
func Open(ctx context.Context, settings Settings) (*Pools, error) {
    primaryConfig, err := BuildPoolConfig(settings.PrimaryURL, settings)
    if err != nil {
        return nil, fmt.Errorf("primary: %w", err)
    }

    replicaConfig, err := BuildPoolConfig(settings.ReplicaURL, settings)
    if err != nil {
        return nil, fmt.Errorf("replica: %w", err)
    }

    primary, err := pgxpool.NewWithConfig(ctx, primaryConfig)
    if err != nil {
        return nil, fmt.Errorf("primary: cannot create the pool: %w", redactCredentials(err, settings))
    }

    replica, err := pgxpool.NewWithConfig(ctx, replicaConfig)
    if err != nil {
        primary.Close()

        return nil, fmt.Errorf("replica: cannot create the pool: %w", redactCredentials(err, settings))
    }

    pools := &Pools{primary: primary, replica: replica}

    if err := pools.Ping(ctx); err != nil {
        pools.Close()

        return nil, err
    }

    return pools, nil
}

// Ping checks both pools and reports every one that is unreachable, so a single
// startup attempt names both problems when both are broken.
func (pools *Pools) Ping(ctx context.Context) error {
    var problems []error

    if err := pools.primary.Ping(ctx); err != nil {
        problems = append(problems, fmt.Errorf("primary is unreachable: %w", err))
    }

    if err := pools.replica.Ping(ctx); err != nil {
        problems = append(problems, fmt.Errorf("replica is unreachable: %w", err))
    }

    return errors.Join(problems...)
}

// PingPrimary checks only the pool that decisions depend on. Readiness uses
// this one, because a service that cannot reach the primary cannot confirm a
// seat and must not be sent traffic.
func (pools *Pools) PingPrimary(ctx context.Context) error {
    return pools.primary.Ping(ctx)
}

// PingReplica checks the advisory pool. A failure here is a degraded state, not
// an unready one, because every read it serves can fall back to the primary.
func (pools *Pools) PingReplica(ctx context.Context) error {
    return pools.replica.Ping(ctx)
}

// Close releases both pools. It is safe to call on a partly built Pools.
func (pools *Pools) Close() {
    if pools == nil {
        return
    }

    if pools.primary != nil {
        pools.primary.Close()
    }

    if pools.replica != nil {
        pools.replica.Close()
    }
}

// BuildPoolConfig turns one address into a pool configuration. It performs no
// input or output, which is what lets the pool settings be tested without a
// database anywhere near the test.
//
// Param:
// address - string (a postgres connection url)
// settings - Settings (pool size and connect timeout applied to the result)
//
// Return:
//   - the pool configuration ready for pgxpool
//   - an error with the address left out, since it carries a password
func BuildPoolConfig(address string, settings Settings) (*pgxpool.Config, error) {
    if address == "" {
        return nil, errors.New("the connection url is empty")
    }

    poolConfig, err := pgxpool.ParseConfig(address)
    if err != nil {
        return nil, errors.New("the connection url cannot be parsed")
    }

    if settings.MaxConnections < 1 {
        return nil, fmt.Errorf("max connections is %d, it must be at least 1", settings.MaxConnections)
    }

    if settings.ConnectTimeout <= 0 {
        return nil, errors.New("connect timeout must be greater than zero")
    }

    poolConfig.MaxConns = settings.MaxConnections
    poolConfig.MinConns = 0
    poolConfig.MaxConnLifetime = time.Hour
    poolConfig.MaxConnIdleTime = 30 * time.Minute
    poolConfig.HealthCheckPeriod = time.Minute
    poolConfig.ConnConfig.ConnectTimeout = settings.ConnectTimeout

    return poolConfig, nil
}

// redactCredentials keeps a password out of an error string. A driver error can
// echo the address it was given, and that address carries the password.
func redactCredentials(err error, settings Settings) error {
    message := err.Error()

    for _, address := range []string{settings.PrimaryURL, settings.ReplicaURL} {
        if address == "" {
            continue
        }

        message = strings.ReplaceAll(message, address, "[connection url redacted]")
    }

    return errors.New(message)
}
