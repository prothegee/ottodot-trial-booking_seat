//go:build containers

// The real database proof for the schema. It needs the stack running, which is
// why it sits behind the containers build tag and never runs during the fast
// go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh
//	cd backend && go test -tags=containers ./internal/database/...
//
// It works inside a throwaway schema, so it can run against the same database a
// reviewer is clicking through without touching a single seeded row.
package database_test

import (
    "context"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
)

const uniqueViolation = "23505"

func primaryAddressFromEnvironment() string {
    if address := os.Getenv("DATABASE_PRIMARY_URL"); address != "" {
        return address
    }

    return "postgres://ottodot:ottodot_development@127.0.0.1:5432/ottodot?sslmode=disable"
}

// applySchemaInAScratchSchema creates an isolated schema, applies the migration
// into it, and returns a connection whose search path points there. The cleanup
// drops the whole thing.
func applySchemaInAScratchSchema(t *testing.T) *pgx.Conn {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    t.Cleanup(cancel)

    scratchName := fmt.Sprintf("schema_proof_%d", time.Now().UnixNano())

    admin, err := pgx.Connect(ctx, primaryAddressFromEnvironment())
    if err != nil {
        t.Fatalf("cannot reach the primary, run scripts/stack_up.sh first: %v", err)
    }

    if _, err := admin.Exec(ctx, "create schema "+scratchName); err != nil {
        t.Fatalf("cannot create the scratch schema: %v", err)
    }

    t.Cleanup(func() {
        cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
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

    scoped, err := pgx.Connect(ctx, primaryAddressFromEnvironment())
    if err != nil {
        t.Fatalf("cannot open the scoped connection: %v", err)
    }

    t.Cleanup(func() {
        closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer closeCancel()

        scoped.Close(closeCtx)
    })

    if _, err := scoped.Exec(ctx, "set search_path to "+scratchName); err != nil {
        t.Fatalf("cannot point the connection at the scratch schema: %v", err)
    }

    if _, err := scoped.Exec(ctx, string(migration)); err != nil {
        t.Fatalf("the migration did not apply cleanly: %v", err)
    }

    return scoped
}

func TestMigrationCreatesEveryTable(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    expected := []string{
        "booking_events",
        "bookings",
        "job_queue",
        "parents",
        "payment_attempts",
        "refresh_tokens",
        "students",
        "trial_classes",
    }

    for _, table := range expected {
        var found bool

        err := connection.QueryRow(ctx, `
            select exists (
                select 1 from pg_tables
                where schemaname = current_schema() and tablename = $1
            )`, table).Scan(&found)
        if err != nil {
            t.Fatalf("cannot look up %s: %v", table, err)
        }

        if !found {
            t.Errorf("table %s is missing", table)
        }
    }
}

func TestMigrationCreatesBothEnums(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    cases := []struct {
        name   string
        labels []string
    }{
        {
            name: "booking_status",
            labels: []string{
                "pending_payment", "confirmed", "payment_failed",
                "refund_required", "expired", "cancelled",
            },
        },
        {
            name:   "payment_status",
            labels: []string{"initiated", "succeeded", "failed"},
        },
    }

    for _, testCase := range cases {
        var labels []string

        err := connection.QueryRow(ctx, `
            select array_agg(enumlabel order by enumsortorder)
            from pg_enum
            join pg_type on pg_type.oid = pg_enum.enumtypid
            join pg_namespace on pg_namespace.oid = pg_type.typnamespace
            where pg_type.typname = $1 and pg_namespace.nspname = current_schema()`,
            testCase.name).Scan(&labels)
        if err != nil {
            t.Fatalf("cannot read the %s enum: %v", testCase.name, err)
        }

        if strings.Join(labels, ",") != strings.Join(testCase.labels, ",") {
            t.Errorf("enum %s holds %v, expected %v", testCase.name, labels, testCase.labels)
        }
    }
}

func TestMigrationCreatesEveryUniqueIndex(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    // Each of these is an invariant the database owns. A missing one means the
    // application is the only thing standing between a race and a double
    // booking, which is exactly the failure this project is about.
    expected := map[string]string{
        "uq_booking_active":      "student_id, class_id",
        "uq_seat_taken":          "class_id, seat_no",
        "uq_payment_idempotency": "booking_id, idempotency_key",
        "uq_refresh_token_hash":  "token_hash",
    }

    for name, columns := range expected {
        var definition string

        err := connection.QueryRow(ctx, `
            select indexdef from pg_indexes
            where schemaname = current_schema() and indexname = $1`, name).Scan(&definition)
        if err != nil {
            t.Errorf("index %s is missing: %v", name, err)

            continue
        }

        if !strings.Contains(definition, "CREATE UNIQUE INDEX") {
            t.Errorf("index %s is not unique: %s", name, definition)
        }

        for _, column := range strings.Split(columns, ", ") {
            if !strings.Contains(definition, column) {
                t.Errorf("index %s does not cover %s: %s", name, column, definition)
            }
        }
    }
}

func TestTheLiveBookingIndexIsPartial(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)

    seedOneParentAndClass(t, connection)

    // Two live bookings for the same child in the same class is the duplicate
    // case from the brief, and the database must be the one that says no.
    insertBooking(t, connection, "0192a000-0000-7000-8000-0000000000b1", "pending_payment", nil)

    err := insertBookingReturningError(t, connection,
        "0192a000-0000-7000-8000-0000000000b2", "pending_payment", nil)
    if !isUniqueViolation(err) {
        t.Fatalf("expected a unique violation on the second live booking, got: %v", err)
    }

    // The same child may book again once the first booking is no longer live,
    // which is what makes the index partial rather than plain.
    mustExec(t, connection, `
        update bookings set status = 'cancelled'
        where id = '0192a000-0000-7000-8000-0000000000b1'`)

    insertBooking(t, connection, "0192a000-0000-7000-8000-0000000000b3", "pending_payment", nil)
}

func TestTwoBookingsCannotShareASeat(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    seedOneParentAndClass(t, connection)
    mustExec(t, connection, `
        insert into students (id, parent_id, full_name, grade_level)
        values ('0192a000-0000-7000-8000-0000000000a3',
                '0192a000-0000-7000-8000-0000000000a1', 'Second Child', 4)`)

    seat := 1
    insertBooking(t, connection, "0192a000-0000-7000-8000-0000000000c1", "confirmed", &seat)

    _, err := connection.Exec(ctx, `
        insert into bookings (id, student_id, class_id, status, seat_no, confirmed_at)
        values ('0192a000-0000-7000-8000-0000000000c2',
                '0192a000-0000-7000-8000-0000000000a3',
                '0192a000-0000-7000-8000-0000000000a2', 'confirmed', 1, now())`)
    if !isUniqueViolation(err) {
        t.Fatalf("expected a unique violation on the shared seat, got: %v", err)
    }
}

func TestAReplayedPaymentKeyCannotCreateASecondCharge(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    seedOneParentAndClass(t, connection)
    insertBooking(t, connection, "0192a000-0000-7000-8000-0000000000d1", "pending_payment", nil)

    mustExec(t, connection, `
        insert into payment_attempts (id, booking_id, idempotency_key, amount_cents, status)
        values ('0192a000-0000-7000-8000-0000000000d2',
                '0192a000-0000-7000-8000-0000000000d1', 'replayed-key', 4500, 'initiated')`)

    _, err := connection.Exec(ctx, `
        insert into payment_attempts (id, booking_id, idempotency_key, amount_cents, status)
        values ('0192a000-0000-7000-8000-0000000000d3',
                '0192a000-0000-7000-8000-0000000000d1', 'replayed-key', 4500, 'initiated')`)
    if !isUniqueViolation(err) {
        t.Fatalf("expected a unique violation on the replayed key, got: %v", err)
    }
}

func TestAConfirmedBookingCannotExistWithoutASeat(t *testing.T) {
    connection := applySchemaInAScratchSchema(t)
    ctx := context.Background()

    seedOneParentAndClass(t, connection)

    _, err := connection.Exec(ctx, `
        insert into bookings (id, student_id, class_id, status, confirmed_at)
        values ('0192a000-0000-7000-8000-0000000000e1',
                '0192a000-0000-7000-8000-0000000000a4',
                '0192a000-0000-7000-8000-0000000000a2', 'confirmed', now())`)
    if err == nil {
        t.Fatal("a confirmed booking with no seat number was accepted")
    }

    if !strings.Contains(err.Error(), "bookings_confirmed_holds_a_seat") {
        t.Fatalf("expected the seat check to fire, got: %v", err)
    }
}

func seedOneParentAndClass(t *testing.T, connection *pgx.Conn) {
    t.Helper()

    mustExec(t, connection, `
        insert into parents (id, email, full_name)
        values ('0192a000-0000-7000-8000-0000000000a1', 'proof@example.test', 'Proof Parent')`)

    mustExec(t, connection, `
        insert into students (id, parent_id, full_name, grade_level)
        values ('0192a000-0000-7000-8000-0000000000a4',
                '0192a000-0000-7000-8000-0000000000a1', 'Proof Child', 5)`)

    mustExec(t, connection, `
        insert into trial_classes (id, subject, title, starts_at)
        values ('0192a000-0000-7000-8000-0000000000a2', 'science', 'Proof Class',
                now() + interval '1 day')`)
}

func insertBooking(t *testing.T, connection *pgx.Conn, id string, status string, seat *int) {
    t.Helper()

    if err := insertBookingReturningError(t, connection, id, status, seat); err != nil {
        t.Fatalf("cannot insert booking %s: %v", id, err)
    }
}

func insertBookingReturningError(t *testing.T, connection *pgx.Conn, id string, status string, seat *int) error {
    t.Helper()

    var confirmedAt any

    if status == "confirmed" {
        confirmedAt = time.Now()
    }

    _, err := connection.Exec(context.Background(), `
        insert into bookings (id, student_id, class_id, status, seat_no, confirmed_at)
        values ($1, '0192a000-0000-7000-8000-0000000000a4',
                '0192a000-0000-7000-8000-0000000000a2', $2, $3, $4)`,
        id, status, seat, confirmedAt)

    return err
}

func mustExec(t *testing.T, connection *pgx.Conn, statement string) {
    t.Helper()

    if _, err := connection.Exec(context.Background(), statement); err != nil {
        t.Fatalf("statement failed: %v\n%s", err, statement)
    }
}

func isUniqueViolation(err error) bool {
    var pgError *pgconn.PgError

    if !errors.As(err, &pgError) {
        return false
    }

    return pgError.Code == uniqueViolation
}
