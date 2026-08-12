//go:build containers

// The real database half of the repository contract.
//
// It needs the backend stack running, which is why it sits behind the
// containers build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/booking/...
//
// Every case works inside its own throwaway schema, so it can run against the
// same database a reviewer is clicking through without touching a seeded row.
package booking_test

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "ottodot-trial-booking/backend/internal/booking"
)

// scratchPoolSize has to cover the widest race in this package, which is the
// twenty parallel confirms of simulation 5. A pool smaller than the fan-out
// would queue the goroutines and quietly weaken the proof.
const scratchPoolSize = 25

// fixturePasswordHash is what the parents column requires: not null, and a
// string the argon2id check accepts. Nothing in this package signs anybody in,
// so the value only has to be well formed.
const fixturePasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$" +
    "8HvgNB40ArlxEEpvrs6x2g$6BJSMpsmkP7ai0ihs7HAYUm6bO2rwxAfMvY9i0C6mZs"

// primaryAddress is where the proof tier connects. Deciding reads and every
// write go to the primary, never the replica.
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

    scratchName := fmt.Sprintf("booking_proof_%d", time.Now().UnixNano())

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
    // scratch schema, which is what keeps the enum and the tables isolated.
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

// postgresFixture points the shared contract at the real repository.
type postgresFixture struct {
    pool       *pgxpool.Pool
    repository *booking.PostgresRepository
}

func newPostgresFixture(t *testing.T) repositoryFixture {
    pool := newScratchPool(t)

    return &postgresFixture{pool: pool, repository: booking.NewPostgresRepository(pool)}
}

func (fixture *postgresFixture) Repository() booking.Repository {
    return fixture.repository
}

func (fixture *postgresFixture) AddClass(t *testing.T, class booking.Class) {
    t.Helper()

    startsAt := class.StartsAt
    if startsAt.IsZero() {
        startsAt = time.Now().Add(72 * time.Hour)
    }

    _, err := fixture.pool.Exec(context.Background(), `
        insert into trial_classes (id, subject, title, starts_at, duration_minutes, capacity, hold_allowance)
        values ($1, $2, $3, $4, $5, $6, $7)`,
        class.ID, class.Subject, class.Title, startsAt, class.DurationMinutes, class.Capacity, class.HoldAllowance)
    if err != nil {
        t.Fatalf("cannot seed the class: %v", err)
    }
}

func (fixture *postgresFixture) AddStudent(t *testing.T, studentID string, parentID string) {
    t.Helper()

    // The address is obviously fake and the domain is reserved for testing, so
    // nothing here can ever reach a real inbox.
    _, err := fixture.pool.Exec(context.Background(), `
        insert into parents (id, email, full_name, password_hash, role)
        values ($1, $2, $3, $4, 'parent')
        on conflict (id) do nothing`,
        parentID, "parent-"+parentID+"@example.test", "Test Parent",
        fixturePasswordHash)
    if err != nil {
        t.Fatalf("cannot seed the parent: %v", err)
    }

    _, err = fixture.pool.Exec(context.Background(), `
        insert into students (id, parent_id, full_name, grade_level)
        values ($1, $2, $3, 5)`,
        studentID, parentID, "Test Child")
    if err != nil {
        t.Fatalf("cannot seed the student: %v", err)
    }
}

func TestThePostgresRepositoryHonoursTheContract(t *testing.T) {
    // The same suite the memory repository runs. This is the run that catches a
    // fake which enforces the invariant correctly while the sql does not.
    runRepositoryContract(t, newPostgresFixture)
}

func TestThePostgresRepositoryHonoursTheDeclineAndWorklistContract(t *testing.T) {
    // The run that matters for these two: payment_failed has to be a value the
    // enum accepts and the filtered worklist has to cast the same way the
    // unfiltered one does. Neither can be shown against a map.
    runDeclineAndWorklistContract(t, newPostgresFixture)
}

func TestThePostgresRepositoryHonoursTheParentBookingsContract(t *testing.T) {
    // The run that matters here: the fake reads a map of children and the sql
    // reads the students table. A join written the wrong way round would list
    // every parent's bookings, and a map cannot show that.
    runParentBookingsContract(t, newPostgresFixture)
}

func TestAMalformedIdentifierIsRefusedRatherThanSearchedFor(t *testing.T) {
    t.Run("edge: text that is not a uuid never reaches a query as a not-found", func(t *testing.T) {
        // Postgres refuses the cast before it looks at a single row. Letting
        // that surface as a driver error would leak the vendor into the http
        // layer, so it is translated like everything else.
        fixture := newPostgresFixture(t)
        repository := fixture.Repository()

        if _, err := repository.Class(context.Background(), "not-a-uuid"); err == nil {
            t.Fatal("expected malformed text to be refused")
        }
    })
}
