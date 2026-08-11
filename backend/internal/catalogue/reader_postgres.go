package catalogue

import (
    "context"
    "errors"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
)

// invalidTextRepresentation is what Postgres reports for text that is not a
// uuid. It is turned into a not-found here rather than reaching a caller,
// because "that is not an id" and "there is no such class" are the same answer
// to whoever asked.
const invalidTextRepresentation = "22P02"

// classProjection is the read this package exists for.
//
// The seat count is computed in the same statement as the class rather than
// looked up per row, so a catalogue of any size is one query. It counts rows
// that own a seat number, which is exactly what the uq_seat_taken index covers,
// so this number and the confirm transaction can never mean different things by
// "taken".
const classProjection = `
    select trial_classes.id,
           trial_classes.subject,
           trial_classes.title,
           trial_classes.starts_at,
           trial_classes.duration_minutes,
           trial_classes.capacity,
           greatest(trial_classes.capacity - count(bookings.id)
               filter (where bookings.seat_no is not null), 0)::smallint as seats_remaining
    from trial_classes
    left join bookings on bookings.class_id = trial_classes.id
`

// PostgresReader is the real one.
type PostgresReader struct {
    pool *pgxpool.Pool
}

// NewPostgresReader wraps a pool.
//
// Note:
//   - the pool should be the replica. Nothing here decides anything, and a seat
//     count that is a second behind is invisible on a screen, so this is the
//     read that keeps load off the pool the confirm transaction needs.
//   - it is not enforced here, because this package cannot tell one pool from
//     another. The wiring in cmd/api is where the choice is made and where the
//     proof tier checks it.
func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
    return &PostgresReader{pool: pool}
}

// Classes lists every trial class with the seats it has left, soonest first.
func (reader *PostgresReader) Classes(ctx context.Context) ([]Class, error) {
    rows, err := reader.pool.Query(ctx, classProjection+`
        group by trial_classes.id
        order by trial_classes.starts_at, trial_classes.id`)
    if err != nil {
        return nil, translate(err)
    }

    defer rows.Close()

    listed := make([]Class, 0)

    for rows.Next() {
        class, err := scanClass(rows)
        if err != nil {
            return nil, translate(err)
        }

        listed = append(listed, class)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err)
    }

    return listed, nil
}

// Class reads one class.
func (reader *PostgresReader) Class(ctx context.Context, classID string) (Class, error) {
    class, err := scanClass(reader.pool.QueryRow(ctx, classProjection+`
        where trial_classes.id = $1
        group by trial_classes.id`, classID))
    if err != nil {
        return Class{}, translate(err)
    }

    return class, nil
}

// scanClass reads one row into a Class.
func scanClass(row pgx.Row) (Class, error) {
    var class Class

    err := row.Scan(&class.ID, &class.Subject, &class.Title, &class.StartsAt,
        &class.DurationMinutes, &class.Capacity, &class.SeatsRemaining)
    if err != nil {
        return Class{}, err
    }

    return class, nil
}

// translate turns a driver failure into one of this package's own.
func translate(err error) error {
    if err == nil {
        return nil
    }

    if errors.Is(err, pgx.ErrNoRows) {
        return ErrClassNotFound
    }

    var databaseError *pgconn.PgError

    if errors.As(err, &databaseError) && databaseError.Code == invalidTextRepresentation {
        return ErrClassNotFound
    }

    return err
}
