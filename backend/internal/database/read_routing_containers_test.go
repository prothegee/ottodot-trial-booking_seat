//go:build containers

// The proof that read routing is real.
//
// Every other test in this repository can be satisfied by two pools pointed at
// the same address, because a fake and a single database behave identically for
// everything except the one thing that matters here: whether a deciding read can
// land on a server that is a moment behind.
//
// This file needs the backend stack running, which is why it sits behind the
// containers build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/database/...
package database_test

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/database"
)

// routingTimeout caps every statement here. A hang would look like a failure
// nobody can read, and every query in this file is one row.
const routingTimeout = 15 * time.Second

// addressOr returns the environment value or the local default.
func addressOr(key string, fallback string) string {
    if address := os.Getenv(key); address != "" {
        return address
    }

    return fallback
}

// openBothPools opens the pair exactly as cmd/api opens them.
func openBothPools(t *testing.T) *database.Pools {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
    t.Cleanup(cancel)

    pools, err := database.Open(ctx, database.Settings{
        PrimaryURL: addressOr("DATABASE_PRIMARY_URL",
            "postgres://ottodot:ottodot_development@127.0.0.1:5432/ottodot?sslmode=disable"),
        ReplicaURL: addressOr("DATABASE_REPLICA_URL",
            "postgres://ottodot:ottodot_development@127.0.0.1:5433/ottodot?sslmode=disable"),
        MaxConnections: 4,
        ConnectTimeout: 5 * time.Second,
    })
    if err != nil {
        t.Fatalf("cannot reach both servers, run scripts/stack_up.sh backend first: %v", err)
    }

    t.Cleanup(pools.Close)

    return pools
}

func TestTheTwoPoolsAreTwoServers(t *testing.T) {
    t.Run("proof: the primary is not in recovery and the replica is", func(t *testing.T) {
        pools := openBothPools(t)

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        var primaryInRecovery bool

        if err := pools.Primary().QueryRow(ctx, "select pg_is_in_recovery()").Scan(&primaryInRecovery); err != nil {
            t.Fatalf("cannot ask the primary what it is: %v", err)
        }

        var replicaInRecovery bool

        if err := pools.Replica().QueryRow(ctx, "select pg_is_in_recovery()").Scan(&replicaInRecovery); err != nil {
            t.Fatalf("cannot ask the replica what it is: %v", err)
        }

        // This is the case the whole file exists for. Two pools pointed at one
        // address pass every other test in this repository, and every claim
        // about read routing in the documents would be false.
        if primaryInRecovery {
            t.Fatal("the primary pool is connected to a standby, so no write can land")
        }

        if !replicaInRecovery {
            t.Fatal("the replica pool is connected to the primary, so nothing about read routing is being tested")
        }
    })
}

func TestTheReplicaRefusesAWrite(t *testing.T) {
    t.Run("proof: a deciding write on the advisory pool fails loudly rather than quietly", func(t *testing.T) {
        pools := openBothPools(t)

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        _, err := pools.Replica().Exec(ctx,
            `insert into trial_classes (id, subject, title, starts_at)
             values ('99999999-9999-7999-8999-999999999999', 'science', 'never', now())`)

        if err == nil {
            t.Fatal("the replica accepted a write, which means it is not a replica")
        }

        // The message is what a developer sees when they wire a repository to
        // the wrong pool. It is asserted so the failure stays recognisable
        // rather than becoming a generic driver error somebody dismisses.
        if !strings.Contains(strings.ToLower(err.Error()), "read-only") {
            t.Fatalf("the replica refused a write with an unexpected reason: %v", err)
        }
    })
}

func TestADecidingReadSeesItsOwnWrite(t *testing.T) {
    t.Run("proof: what was just written on the primary is readable on the primary", func(t *testing.T) {
        pools := openBothPools(t)

        ctx, cancel := context.WithTimeout(context.Background(), routingTimeout)
        defer cancel()

        // A throwaway table, so this proof touches nothing a reviewer is
        // clicking through and needs no seeded row.
        scratch := "read_routing_proof"

        if _, err := pools.Primary().Exec(ctx,
            "create table if not exists "+scratch+" (id uuid primary key)"); err != nil {
            t.Fatalf("cannot create the scratch table: %v", err)
        }

        t.Cleanup(func() {
            cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), routingTimeout)
            defer cleanupCancel()

            if _, err := pools.Primary().Exec(cleanupCtx, "drop table if exists "+scratch); err != nil {
                t.Errorf("the scratch table %s was left behind: %v", scratch, err)
            }
        })

        written := "88888888-8888-7888-8888-888888888888"

        if _, err := pools.Primary().Exec(ctx,
            "insert into "+scratch+" (id) values ($1) on conflict do nothing", written); err != nil {
            t.Fatalf("cannot write: %v", err)
        }

        var found string

        // No wait, no retry. Read-your-own-writes is what the primary is for,
        // and a parent reading their booking straight after paying depends on
        // exactly this being immediate.
        if err := pools.Primary().QueryRow(ctx,
            "select id::text from "+scratch+" where id = $1", written).Scan(&found); err != nil {
            t.Fatalf("the primary cannot see what it just wrote: %v", err)
        }

        if found != written {
            t.Fatalf("read back %s, wrote %s", found, written)
        }
    })
}
