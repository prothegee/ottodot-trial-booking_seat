//go:build containers

// The real database half of the auth contracts, plus the proof no fake can
// give.
//
// It needs the backend stack running, which is why it sits behind the
// containers build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/auth/...
//
// Every case works inside its own throwaway schema, so it can run against the
// same database a reviewer is clicking through without touching a seeded row.
package auth_test

import (
    "context"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "ottodot-trial-booking/backend/internal/auth"
)

// scratchPoolSize has to cover the widest race in this package, which is the
// parallel rotation proof. A pool smaller than the fan-out would queue the
// goroutines and quietly weaken the proof, because two rotations that never
// overlap cannot demonstrate anything about row locking.
const scratchPoolSize = 12

// primaryAddress is where the proof tier connects. Every statement here either
// writes or decides whether a token has already been spent, so it goes to the
// primary and never the replica.
func primaryAddress() string {
    if address := os.Getenv("DATABASE_PRIMARY_URL"); address != "" {
        return address
    }

    return "postgres://ottodot:ottodot_development@127.0.0.1:5432/ottodot?sslmode=disable"
}

// newScratchPool creates an isolated schema, applies the migration into it, and
// returns a pool whose every connection is pointed there. The cleanup drops the
// whole thing.
func newScratchPool(t *testing.T) *pgxpool.Pool {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    t.Cleanup(cancel)

    scratchName := fmt.Sprintf("auth_proof_%d", time.Now().UnixNano())

    admin, err := pgx.Connect(ctx, primaryAddress())
    if err != nil {
        t.Fatalf("cannot reach the primary, run scripts/stack_up.sh backend first: %v", err)
    }

    if _, err := admin.Exec(ctx, "create schema "+scratchName); err != nil {
        t.Fatalf("cannot create the scratch schema: %v", err)
    }

    t.Cleanup(func() {
        cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cleanupCancel()

        if _, err := admin.Exec(cleanupCtx, "drop schema "+scratchName+" cascade"); err != nil {
            t.Errorf("the scratch schema %s was left behind: %v", scratchName, err)
        }

        admin.Close(cleanupCtx)
    })

    migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_schema.sql"))
    if err != nil {
        t.Fatalf("cannot read the migration: %v", err)
    }

    poolConfig, err := pgxpool.ParseConfig(primaryAddress())
    if err != nil {
        t.Fatalf("cannot parse the primary address: %v", err)
    }

    // Every connection in the pool resolves unqualified names inside the
    // scratch schema, which is what keeps the tables isolated.
    poolConfig.ConnConfig.RuntimeParams["search_path"] = scratchName
    poolConfig.MaxConns = scratchPoolSize

    pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
    if err != nil {
        t.Fatalf("cannot open the scratch pool: %v", err)
    }

    t.Cleanup(pool.Close)

    if _, err := pool.Exec(ctx, string(migration)); err != nil {
        t.Fatalf("the migration did not apply into the scratch schema: %v", err)
    }

    return pool
}

// seedAccounts writes the accounts both contracts name.
//
// The real refresh_tokens table hangs from parents by a foreign key, so a token
// cannot be written for a parent that does not exist. That constraint is the
// difference between the two fixtures, and seeding it here is what lets the one
// suite run against both.
func seedAccounts(t *testing.T, pool *pgxpool.Pool) {
    t.Helper()

    ctx := context.Background()

    if _, err := pool.Exec(ctx, `
        insert into parents (id, email, full_name, password_hash, role) values
            ($1, $2, $3, $10, 'parent'),
            ($4, $5, $6, $10, 'parent'),
            ($7, $8, $9, $10, 'admin')`,
        contractParentID, contractParentEmail, contractParentName,
        contractLonelyParentID, contractLonelyParentEmail, "Chandra Wijaya",
        adminParentID, "ops.admin@example.test", "Ops Admin",
        seededPasswordHash); err != nil {
        t.Fatalf("cannot seed the parents: %v", err)
    }

    if _, err := pool.Exec(ctx,
        `insert into students (id, parent_id, full_name, grade_level) values ($1, $2, $3, 5)`,
        contractChildID, contractParentID, contractChildName); err != nil {
        t.Fatalf("cannot seed the child: %v", err)
    }
}

// postgresStoreFixture points the shared store contract at the real table.
type postgresStoreFixture struct {
    store *auth.PostgresRefreshStore
}

func newPostgresStoreFixture(t *testing.T) storeFixture {
    pool := newScratchPool(t)
    seedAccounts(t, pool)

    return &postgresStoreFixture{store: auth.NewPostgresRefreshStore(pool)}
}

func (fixture *postgresStoreFixture) Store() auth.RefreshStore {
    return fixture.store
}

func (fixture *postgresStoreFixture) ParentID() string {
    return contractParentID
}

// postgresDirectoryFixture points the shared directory contract at the real
// tables.
type postgresDirectoryFixture struct {
    directory *auth.PostgresDirectory
}

func newPostgresDirectoryFixture(t *testing.T) directoryFixture {
    pool := newScratchPool(t)
    seedAccounts(t, pool)

    return &postgresDirectoryFixture{directory: auth.NewPostgresDirectory(pool)}
}

func (fixture *postgresDirectoryFixture) Directory() auth.Directory {
    return fixture.directory
}

func TestThePostgresRefreshStoreHonoursTheContract(t *testing.T) {
    // The same suite the memory store runs. This is the run that catches a fake
    // which is right for reasons the sql is not.
    runRefreshStoreContract(t, newPostgresStoreFixture)
}

func TestThePostgresDirectoryHonoursTheContract(t *testing.T) {
    // The case that matters here is the address typed with capitals. The fake
    // lowercases in Go and the real one compares on lower(email), and this is
    // the only run where those two are checked against each other.
    runDirectoryContract(t, newPostgresDirectoryFixture)
}

func TestOneRefreshTokenIsSpentOnceUnderRealParallelism(t *testing.T) {
    t.Run("proof: parallel rotations of one token produce one successor and the rest are reuse", func(t *testing.T) {
        // This is the case the fast tiers cannot prove. A mutex makes the fake
        // pass it by serializing the callers, which is not what happens in
        // production: several requests, several connections, several
        // transactions, arriving at the same instant.
        //
        // What makes it correct is SELECT FOR UPDATE inside the rotation
        // transaction. The second caller waits on the locked row rather than
        // reading it, so by the time it looks, the row is revoked and it
        // reports reuse. Removing the lock makes this test hand out several
        // successors, which is one stolen token quietly becoming several
        // working sessions.
        pool := newScratchPool(t)
        seedAccounts(t, pool)

        store := auth.NewPostgresRefreshStore(pool)
        ctx := context.Background()

        if _, err := store.Issue(ctx, auth.IssueRequest{
            TokenID:   newTokenID(t),
            ParentID:  contractParentID,
            FamilyID:  newTokenID(t),
            TokenHash: auth.HashRefreshToken("R1"),
            ExpiresAt: storeMoment.Add(contractRefreshTTL),
            Now:       storeMoment,
        }); err != nil {
            t.Fatalf("cannot issue the first token: %v", err)
        }

        const racers = 8

        var (
            start    sync.WaitGroup
            finished sync.WaitGroup
            mutex    sync.Mutex
        )

        succeeded := 0
        reused := 0

        start.Add(1)
        finished.Add(racers)

        for index := 0; index < racers; index++ {
            go func(attempt int) {
                defer finished.Done()

                start.Wait()

                _, err := store.Rotate(ctx, auth.RotateRequest{
                    PresentedHash: auth.HashRefreshToken("R1"),
                    NextTokenID:   newTokenID(t),
                    NextTokenHash: auth.HashRefreshToken(fmt.Sprintf("successor-%d", attempt)),
                    NextExpiresAt: storeMoment.Add(contractRefreshTTL),
                    Now:           storeMoment.Add(time.Hour),
                })

                mutex.Lock()
                defer mutex.Unlock()

                switch {
                case err == nil:
                    succeeded++
                case errors.Is(err, auth.ErrTokenReused):
                    reused++
                default:
                    t.Errorf("unexpected failure: %v", err)
                }
            }(index)
        }

        start.Done()
        finished.Wait()

        if succeeded != 1 {
            t.Fatalf("expected exactly one rotation to succeed, got %d", succeeded)
        }

        if reused != racers-1 {
            t.Fatalf("expected the other %d to be reported as reuse, got %d", racers-1, reused)
        }
    })

    t.Run("proof: the unique index refuses a second row for one token hash", func(t *testing.T) {
        // uq_refresh_token_hash is what makes "look this token up" a question
        // with one answer. Without it, reuse detection would be reading
        // whichever row came back first.
        pool := newScratchPool(t)
        seedAccounts(t, pool)

        store := auth.NewPostgresRefreshStore(pool)
        ctx := context.Background()

        request := auth.IssueRequest{
            TokenID:   newTokenID(t),
            ParentID:  contractParentID,
            FamilyID:  newTokenID(t),
            TokenHash: auth.HashRefreshToken("R1"),
            ExpiresAt: storeMoment.Add(contractRefreshTTL),
            Now:       storeMoment,
        }

        if _, err := store.Issue(ctx, request); err != nil {
            t.Fatalf("cannot issue the first token: %v", err)
        }

        request.TokenID = newTokenID(t)

        if _, err := store.Issue(ctx, request); !errors.Is(err, auth.ErrDuplicateToken) {
            t.Fatalf("expected the index to refuse a second row, got %v", err)
        }
    })
}
