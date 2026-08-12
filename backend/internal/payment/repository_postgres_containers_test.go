//go:build containers

// The real database half of the payment repository contract.
//
// It needs the backend stack running, which is why it sits behind the
// containers build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/payment/...
//
// Every case works inside its own throwaway schema, so it can run against the
// same database a reviewer is clicking through without touching a seeded row.
package payment_test

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

    "ottodot-trial-booking/backend/internal/payment"
)

// scratchPoolSize has to cover the widest race in this package, which is the
// parallel replay proof. A pool smaller than the fan-out would queue the
// goroutines and quietly weaken the proof.
const scratchPoolSize = 12

// The rows every seeded booking hangs from. A booking needs a student, a
// student needs a parent, and a booking needs a class, so the fixture creates
// all three once and points every booking at them.
const (
    fixtureParent  = "0192d000-0000-7000-8000-000000000101"
    fixtureStudent = "0192d000-0000-7000-8000-000000000102"
    fixtureClass   = "0192d000-0000-7000-8000-000000000103"
)

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

    scratchName := fmt.Sprintf("payment_proof_%d", time.Now().UnixNano())

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
    repository *payment.PostgresRepository
    seeded     bool
}

func newPostgresFixture(t *testing.T) repositoryFixture {
    pool := newScratchPool(t)

    return &postgresFixture{pool: pool, repository: payment.NewPostgresRepository(pool)}
}

func (fixture *postgresFixture) Repository() payment.Repository {
    return fixture.repository
}

// AddBooking inserts the booking the foreign key points at, along with the
// parent, student, and class it hangs from the first time it is called.
func (fixture *postgresFixture) AddBooking(t *testing.T, bookingID string) {
    t.Helper()

    ctx := context.Background()

    if !fixture.seeded {
        // The address is obviously fake and the domain is reserved for testing,
        // so nothing here can ever reach a real inbox.
        if _, err := fixture.pool.Exec(ctx, `
            insert into parents (id, email, full_name, password_hash, role)
            values ($1, $2, $3, $4, 'parent')`,
            fixtureParent, "parent-"+fixtureParent+"@example.test", "Test Parent",
            fixturePasswordHash); err != nil {
            t.Fatalf("cannot seed the parent: %v", err)
        }

        if _, err := fixture.pool.Exec(ctx, `
            insert into students (id, parent_id, full_name, grade_level)
            values ($1, $2, $3, 5)`,
            fixtureStudent, fixtureParent, "Test Child"); err != nil {
            t.Fatalf("cannot seed the student: %v", err)
        }

        if _, err := fixture.pool.Exec(ctx, `
            insert into trial_classes (id, subject, title, starts_at, capacity)
            values ($1, 'science', 'Science Discovery', $2, 4)`,
            fixtureClass, time.Now().Add(72*time.Hour)); err != nil {
            t.Fatalf("cannot seed the class: %v", err)
        }

        fixture.seeded = true
    }

    // The booking is cancelled rather than live, because uq_booking_active
    // admits one live booking per child per class and this fixture needs
    // several bookings for one child. Nothing in this package reads the status.
    if _, err := fixture.pool.Exec(ctx, `
        insert into bookings (id, student_id, class_id, status)
        values ($1, $2, $3, 'cancelled')`,
        bookingID, fixtureStudent, fixtureClass); err != nil {
        t.Fatalf("cannot seed the booking: %v", err)
    }
}

func TestThePostgresRepositoryHonoursTheContract(t *testing.T) {
    // The same suite the memory repository runs. This is the run that catches a
    // fake which enforces idempotency correctly while the sql does not.
    runRepositoryContract(t, newPostgresFixture)
}

func TestOneKeyChargesOnceUnderRealConcurrency(t *testing.T) {
    t.Run("proof: parallel calls with one key produce exactly one row", func(t *testing.T) {
        // This is what the fake cannot prove. Ten real connections race on
        // uq_payment_idempotency, and the index is what makes one of them the
        // row and the other nine replays of it.
        fixture := newPostgresFixture(t)
        seedContractFixture(t, fixture)
        repository := fixture.Repository()

        const callers = 10

        var (
            waitGroup sync.WaitGroup
            mutex     sync.Mutex
            opened    int
            replays   int
        )

        for i := 0; i < callers; i++ {
            waitGroup.Add(1)

            go func() {
                defer waitGroup.Done()

                _, replayed, err := repository.Begin(context.Background(), beginRequestFor(t, bookingOne, "key-race"))

                mutex.Lock()
                defer mutex.Unlock()

                switch {
                case err != nil:
                    t.Errorf("unexpected failure from a parallel call: %v", err)
                case replayed:
                    replays++
                default:
                    opened++
                }
            }()
        }

        waitGroup.Wait()

        if opened != 1 || replays != callers-1 {
            t.Fatalf("expected one opened attempt and %d replays, got %d and %d", callers-1, opened, replays)
        }

        stored, err := repository.AttemptsFor(context.Background(), bookingOne)
        if err != nil {
            t.Fatalf("expected the attempts for the booking, got: %v", err)
        }

        if len(stored) != 1 {
            t.Fatalf("expected exactly one payment_attempts row, got %d", len(stored))
        }
    })
}

func TestTheAmountCheckHoldsInTheDatabaseToo(t *testing.T) {
    t.Run("edge: a charge of nothing is refused by the column as well as by the code", func(t *testing.T) {
        // The repository refuses this before the driver sees it. The check
        // constraint is the backstop, and this case proves the backstop is
        // really there rather than assumed.
        fixture := newPostgresFixture(t)
        seedContractFixture(t, fixture)

        postgres, ok := fixture.(*postgresFixture)
        if !ok {
            t.Fatal("this case needs the postgres fixture")
        }

        _, err := postgres.pool.Exec(context.Background(), `
            insert into payment_attempts (id, booking_id, idempotency_key, amount_cents, status)
            values ($1, $2, 'key-direct', 0, 'initiated')`,
            newAttemptID(t), bookingOne)
        if err == nil {
            t.Fatal("expected the check constraint to refuse an amount of zero")
        }
    })
}

func TestAMalformedIdentifierIsRefusedRatherThanSearchedFor(t *testing.T) {
    t.Run("edge: text that is not a uuid never reaches a query as a not-found", func(t *testing.T) {
        // Postgres refuses the cast before it looks at a single row. Letting
        // that surface as a driver error would leak the vendor into the http
        // layer, so it is translated like everything else.
        fixture := newPostgresFixture(t)
        repository := fixture.Repository()

        _, err := repository.Attempt(context.Background(), "not-a-uuid")
        if !errors.Is(err, payment.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest for text that is not a uuid, got: %v", err)
        }
    })
}
