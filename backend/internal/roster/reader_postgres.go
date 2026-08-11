package roster

import (
    "context"
    "errors"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
)

// invalidTextRepresentation is what Postgres reports for text that is not a
// uuid. It is turned into a not-found here, because "that is not an id" and
// "there is no such class" are the same answer to whoever asked.
const invalidTextRepresentation = "22P02"

// PostgresReader is the real one.
type PostgresReader struct {
    pool *pgxpool.Pool
}

// NewPostgresReader wraps a pool.
//
// Note:
//   - the pool should be the replica. A teacher opens a roster minutes before a
//     class rather than at the instant of a write, so lag of a second is
//     invisible, and this is the read that would otherwise sit on the pool the
//     confirm transaction needs.
func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
    return &PostgresReader{pool: pool}
}

// For reads everyone who owns a seat in one class.
//
// The class is read first and separately, so a class nobody has booked answers
// with an empty roster rather than with a not-found. Those are different facts
// and a teacher acts differently on each.
func (reader *PostgresReader) For(ctx context.Context, classID string) (Roster, error) {
    var capacity int16

    err := reader.pool.QueryRow(ctx,
        `select capacity from trial_classes where id = $1`, classID).Scan(&capacity)
    if err != nil {
        return Roster{}, translate(err)
    }

    // Only confirmed bookings, and only rows that own a seat. Both conditions
    // are stated rather than one implying the other, because the seat is what a
    // teacher reads and the status is what makes it attendance.
    rows, err := reader.pool.Query(ctx, `
        select bookings.seat_no, students.id, students.full_name, bookings.confirmed_at
        from bookings
        join students on students.id = bookings.student_id
        where bookings.class_id = $1
          and bookings.status = 'confirmed'
          and bookings.seat_no is not null
        order by bookings.seat_no`, classID)
    if err != nil {
        return Roster{}, translate(err)
    }

    defer rows.Close()

    seated := make([]Entry, 0)

    for rows.Next() {
        var entry Entry

        if err := rows.Scan(&entry.SeatNo, &entry.StudentID, &entry.StudentName, &entry.ConfirmedAt); err != nil {
            return Roster{}, translate(err)
        }

        seated = append(seated, entry)
    }

    if err := rows.Err(); err != nil {
        return Roster{}, translate(err)
    }

    return Roster{ClassID: classID, Capacity: capacity, Entries: seated}, nil
}

// translate turns a driver failure into one of this package's own.
//
// Nothing from the driver is ever wrapped into what a caller sees. This package
// holds child names, and a driver message can echo the row it was reading.
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
