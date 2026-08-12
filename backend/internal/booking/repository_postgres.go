package booking

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"

    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/identifier"
)

// Postgres error codes this repository turns into its own failures. A driver
// code reaching a caller would make the http layer depend on the database
// vendor, which is a coupling nobody notices until it is expensive.
const (
    uniqueViolation           = "23505"
    foreignKeyViolation       = "23503"
    invalidTextRepresentation = "22P02"
    uniqueBookingActiveIndex  = "uq_booking_active"
    uniqueSeatTakenIndex      = "uq_seat_taken"
)

// bookingColumns is the read shape for a booking. The enum is cast to text so
// the driver never has to know about a database side type.
const bookingColumns = `id, student_id, class_id, status::text, seat_no, hold_expires_at, confirmed_at, created_at, updated_at`

// classColumns is the read shape for a class.
const classColumns = `id, subject, title, starts_at, duration_minutes, capacity, hold_allowance`

// PostgresRepository is the real one. It is the authority on who owns a seat.
type PostgresRepository struct {
    pool         *pgxpool.Pool
    fault        Fault
    transactions TransactionCounter
}

// NewPostgresRepository wraps a pool.
//
// Note:
//   - the pool must be the primary. Every method here either writes or decides,
//     and the replica is asynchronous, so a seat count read from it would be a
//     guess dressed up as an answer.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
    return &PostgresRepository{pool: pool}
}

// InjectFaults points this repository at a fault source.
//
// It is a setter rather than a constructor argument on purpose. Every ordinary
// caller builds a repository without one and never learns this method exists,
// and the one place that does call it is the api's wiring, under a configuration
// flag that cannot be true outside development.
func (repository *PostgresRepository) InjectFaults(fault Fault) {
    repository.fault = fault
}

// Class reads one class.
func (repository *PostgresRepository) Class(ctx context.Context, classID string) (Class, error) {
    class, err := scanClass(repository.pool.QueryRow(ctx,
        `select `+classColumns+` from trial_classes where id = $1`, classID))
    if err != nil {
        return Class{}, translate(err, ErrClassNotFound)
    }

    return class, nil
}

// Booking reads one booking.
func (repository *PostgresRepository) Booking(ctx context.Context, bookingID string) (Booking, error) {
    stored, err := scanBooking(repository.pool.QueryRow(ctx,
        `select `+bookingColumns+` from bookings where id = $1`, bookingID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    return stored, nil
}

// LiveBooking finds the booking that stands between this child and a second one
// for this class.
//
// The two statuses here are exactly the ones in uq_booking_active, so this query
// and the index agree by construction: whatever the index would refuse a second
// row for is what this finds.
func (repository *PostgresRepository) LiveBooking(ctx context.Context, studentID string, classID string) (Booking, error) {
    stored, err := scanBooking(repository.pool.QueryRow(ctx, `
        select `+bookingColumns+`
        from bookings
        where student_id = $1
          and class_id = $2
          and status in ('pending_payment', 'confirmed')
        order by created_at desc
        limit 1`, studentID, classID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    return stored, nil
}

// SeatsTaken lists the seats currently held in a class, ascending.
func (repository *PostgresRepository) SeatsTaken(ctx context.Context, classID string) ([]int16, error) {
    rows, err := repository.pool.Query(ctx,
        `select seat_no from bookings where class_id = $1 and seat_no is not null order by seat_no`, classID)
    if err != nil {
        return nil, translate(err, nil)
    }

    defer rows.Close()

    var seats []int16

    for rows.Next() {
        var seat int16

        if err := rows.Scan(&seat); err != nil {
            return nil, translate(err, nil)
        }

        seats = append(seats, seat)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return seats, nil
}

// Events reads the audit trail for one booking, oldest first.
func (repository *PostgresRepository) Events(ctx context.Context, bookingID string) ([]Event, error) {
    rows, err := repository.pool.Query(ctx, `
        select id, booking_id, from_status::text, to_status::text, actor, reason, created_at
        from booking_events
        where booking_id = $1
        order by created_at, id`, bookingID)
    if err != nil {
        return nil, translate(err, nil)
    }

    defer rows.Close()

    var trail []Event

    for rows.Next() {
        var (
            entry      Event
            fromStatus *string
            reason     *string
        )

        if err := rows.Scan(&entry.ID, &entry.BookingID, &fromStatus, &entry.ToStatus,
            &entry.Actor, &reason, &entry.CreatedAt); err != nil {
            return nil, translate(err, nil)
        }

        if fromStatus != nil {
            entry.FromStatus = Status(*fromStatus)
        }

        if reason != nil {
            entry.Reason = *reason
        }

        trail = append(trail, entry)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return trail, nil
}

// Hold grants a place on the payment screen, in one transaction.
//
// The class row is locked first, so two parents asking for the last allowance
// slot at the same instant are counted one after the other rather than both
// against the same stale number.
func (repository *PostgresRepository) Hold(ctx context.Context, request HoldRequest) (result Booking, err error) {
    transaction, err := repository.pool.Begin(ctx)
    if err != nil {
        return Booking{}, fmt.Errorf("cannot open the hold transaction: %w", translate(err, nil))
    }

    // Registered before the rollback below, so it runs after it and reads the
    // error this method is actually returning.
    defer func() { repository.countTransaction(transactionHold, err) }()

    defer transaction.Rollback(ctx)

    class, err := scanClass(transaction.QueryRow(ctx,
        `select `+classColumns+` from trial_classes where id = $1 for update`, request.ClassID))
    if err != nil {
        return Booking{}, translate(err, ErrClassNotFound)
    }

    var parentID string

    err = transaction.QueryRow(ctx,
        `select parent_id from students where id = $1`, request.StudentID).Scan(&parentID)
    if err != nil {
        return Booking{}, translate(err, ErrStudentNotFound)
    }

    // Mirrors uq_booking_active: one live booking per child per class, whether
    // or not its hold has lapsed. The index would refuse a second row anyway,
    // so answering here is only about giving the parent a clear reason.
    var duplicates int

    err = transaction.QueryRow(ctx, `
        select count(*)
        from bookings
        where student_id = $1
          and class_id = $2
          and status in ('pending_payment', 'confirmed')`, request.StudentID, request.ClassID).Scan(&duplicates)
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if duplicates > 0 {
        return Booking{}, ErrAlreadyBooked
    }

    var standing int

    err = transaction.QueryRow(ctx, `
        select count(*)
        from bookings
        join students on students.id = bookings.student_id
        where students.parent_id = $1
          and bookings.status = 'pending_payment'
          and bookings.hold_expires_at > $2`, parentID, request.Now).Scan(&standing)
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if standing >= request.MaxHoldsPerParent {
        return Booking{}, ErrTooManyHolds
    }

    // A lapsed hold is not counted, so a parent who walked away does not keep
    // a class looking full until the worker gets to it.
    var holders int

    err = transaction.QueryRow(ctx, `
        select count(*)
        from bookings
        where class_id = $1
          and (status = 'confirmed' or (status = 'pending_payment' and hold_expires_at > $2))`,
        request.ClassID, request.Now).Scan(&holders)
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if holders >= MaxHolders(class) {
        return Booking{}, ErrClassFull
    }

    granted, err := scanBooking(transaction.QueryRow(ctx, `
        insert into bookings (id, student_id, class_id, status, hold_expires_at, created_at, updated_at)
        values ($1, $2, $3, 'pending_payment', $4, $5, $5)
        returning `+bookingColumns,
        request.BookingID, request.StudentID, request.ClassID, request.ExpiresAt, request.Now))
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if err := recordEvent(ctx, transaction, granted.ID, "", StatusPendingPayment, ActorParent, "hold granted", request.Now); err != nil {
        return Booking{}, err
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the hold could not be committed: %w", translate(err, nil))
    }

    return granted, nil
}

// Confirm runs the last-seat transaction. This is the only authority in the
// system on who owns a seat.
//
// Note:
//   - the hold deadline is deliberately not consulted. By the time a booking
//     reaches here the money has already moved, and handing over a seat that is
//     still free beats refunding a parent because a countdown ran out a moment
//     earlier. A hold the worker already expired fails the status check
//     instead, which is the honest signal.
//   - the seat-lost path commits. The refund_required row is the record that
//     tells an operator money has to move back, so rolling it back would lose
//     the only trace of a charge that has already happened.
func (repository *PostgresRepository) Confirm(ctx context.Context, request ConfirmRequest) (result Booking, err error) {
    transaction, err := repository.pool.Begin(ctx)
    if err != nil {
        return Booking{}, fmt.Errorf("cannot open the confirm transaction: %w", translate(err, nil))
    }

    // Registered before the rollback below, so it runs after it and reads the
    // error this method is actually returning.
    defer func() { repository.countTransaction(transactionConfirm, err) }()

    defer transaction.Rollback(ctx)

    // The class is read first and locked first, always in that order, so two
    // confirms can never take the two locks in opposite order and deadlock.
    var classID string

    err = transaction.QueryRow(ctx,
        `select class_id from bookings where id = $1`, request.BookingID).Scan(&classID)
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    // Injection point: the class lock. A real database under contention gives up
    // on a lock rather than waiting forever, and this is where that happens.
    if repository.fault.triggered(faults.PointConfirmLockWait) {
        return Booking{}, ErrLockWaitTimeout
    }

    class, err := scanClass(transaction.QueryRow(ctx,
        `select `+classColumns+` from trial_classes where id = $1 for update`, classID))
    if err != nil {
        return Booking{}, translate(err, ErrClassNotFound)
    }

    stored, err := scanBooking(transaction.QueryRow(ctx,
        `select `+bookingColumns+` from bookings where id = $1 for update`, request.BookingID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    if stored.Status != StatusPendingPayment {
        return stored, ErrNotHolding
    }

    // The lowest free seat under the lock. This is the sql the whole exercise
    // turns on, and it is the memory repository's LowestFreeSeat in database
    // form.
    var chosen int32

    err = transaction.QueryRow(ctx, `
        select seat_no
        from generate_series(1, $1::int) as seat_no
        where seat_no not in (
            select seat_no from bookings
                where class_id = $2 and seat_no is not null
        )
        order by seat_no
        limit 1`, int32(class.Capacity), classID).Scan(&chosen)

    if errors.Is(err, pgx.ErrNoRows) {
        return repository.settleSeatLost(ctx, transaction, stored, request.Now)
    }

    if err != nil {
        return Booking{}, translate(err, nil)
    }

    won, err := scanBooking(transaction.QueryRow(ctx, `
        update bookings
        set status = 'confirmed', seat_no = $2, confirmed_at = $3, hold_expires_at = null, updated_at = $3
        where id = $1 and status = 'pending_payment'
        returning `+bookingColumns, request.BookingID, int16(chosen), request.Now))
    if err != nil {
        return Booking{}, translate(err, ErrNotHolding)
    }

    if err := recordEvent(ctx, transaction, won.ID, StatusPendingPayment, StatusConfirmed, ActorSystem, "seat assigned under the class lock", request.Now); err != nil {
        return Booking{}, err
    }

    // Injection point: the database dying with the seat written and the commit
    // not yet sent. Returning here leaves the deferred rollback to undo the seat,
    // the event row, and the status change together, which is the property the
    // whole design rests on and the one worth being able to demonstrate.
    if repository.fault.triggered(faults.PointConfirmBeforeCommit) {
        return Booking{}, ErrTransactionBroken
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the confirmation could not be committed: %w", translate(err, nil))
    }

    return won, nil
}

// Cancel withdraws a booking and releases any seat it held, which is what makes
// that seat available to the next confirm.
func (repository *PostgresRepository) Cancel(ctx context.Context, request CancelRequest) (result Booking, err error) {
    transaction, err := repository.pool.Begin(ctx)
    if err != nil {
        return Booking{}, fmt.Errorf("cannot open the cancel transaction: %w", translate(err, nil))
    }

    // Registered before the rollback below, so it runs after it and reads the
    // error this method is actually returning.
    defer func() { repository.countTransaction(transactionCancel, err) }()

    defer transaction.Rollback(ctx)

    stored, err := scanBooking(transaction.QueryRow(ctx,
        `select `+bookingColumns+` from bookings where id = $1 for update`, request.BookingID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    if !CanTransition(stored.Status, StatusCancelled) {
        return stored, ErrInvalidTransition
    }

    withdrawn, err := scanBooking(transaction.QueryRow(ctx, `
        update bookings
        set status = 'cancelled', seat_no = null, hold_expires_at = null, updated_at = $2
        where id = $1
        returning `+bookingColumns, request.BookingID, request.Now))
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if err := recordEvent(ctx, transaction, withdrawn.ID, stored.Status, StatusCancelled, request.Actor, request.Reason, request.Now); err != nil {
        return Booking{}, err
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the cancellation could not be committed: %w", translate(err, nil))
    }

    return withdrawn, nil
}

// Fail ends a booking whose payment was declined, in one transaction.
//
// The row is locked before the status is read, so a decline arriving at the same
// instant as a confirm cannot both be written. The second one finds the booking
// is no longer pending_payment and is told so.
func (repository *PostgresRepository) Fail(ctx context.Context, request FailRequest) (result Booking, err error) {
    transaction, err := repository.pool.Begin(ctx)
    if err != nil {
        return Booking{}, fmt.Errorf("cannot open the decline transaction: %w", translate(err, nil))
    }

    // Registered before the rollback below, so it runs after it and reads the
    // error this method is actually returning.
    defer func() { repository.countTransaction(transactionFail, err) }()

    defer transaction.Rollback(ctx)

    stored, err := scanBooking(transaction.QueryRow(ctx,
        `select `+bookingColumns+` from bookings where id = $1 for update`, request.BookingID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    if stored.Status != StatusPendingPayment {
        return stored, ErrNotHolding
    }

    declined, err := scanBooking(transaction.QueryRow(ctx, `
        update bookings
        set status = 'payment_failed', hold_expires_at = null, updated_at = $2
        where id = $1 and status = 'pending_payment'
        returning `+bookingColumns, request.BookingID, request.Now))
    if err != nil {
        return Booking{}, translate(err, ErrNotHolding)
    }

    if err := recordEvent(ctx, transaction, declined.ID, stored.Status, StatusPaymentFailed, ActorPayment, request.Reason, request.Now); err != nil {
        return Booking{}, err
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the decline could not be committed: %w", translate(err, nil))
    }

    return declined, nil
}

// Worklist lists bookings for an operator, newest first.
//
// It reads the pool it was built with, which is the primary. That is deliberate
// even though nothing here decides anything: an operator opens this screen to
// check what just happened, and a list that is a second behind is a list that
// does not show the booking they are being asked about.
func (repository *PostgresRepository) Worklist(ctx context.Context, request WorklistRequest) ([]Booking, error) {
    if err := request.Validate(); err != nil {
        return nil, err
    }

    // One statement for both shapes. A null status matches every row, so the
    // unfiltered view and the filtered one cannot drift apart.
    var status *string

    if request.Status != "" {
        value := string(request.Status)
        status = &value
    }

    rows, err := repository.pool.Query(ctx, `
        select `+bookingColumns+`
        from bookings
        where $1::text is null or status = $1::text::booking_status
        order by created_at desc, id desc
        limit $2`, status, request.Limit)
    if err != nil {
        return nil, translate(err, nil)
    }

    defer rows.Close()

    matched := make([]Booking, 0, request.Limit)

    for rows.Next() {
        stored, err := scanBooking(rows)
        if err != nil {
            return nil, translate(err, nil)
        }

        matched = append(matched, stored)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return matched, nil
}

// ParentBookings lists one parent's own bookings, newest first.
//
// It reads the primary like the worklist above, and for a sharper reason: this
// is the screen a parent opens straight after paying, and a replica a second
// behind would show the booking they just paid for as still waiting for money.
//
// The parent is applied inside the statement rather than to the rows that come
// back. A subquery on students is what keeps the column list unqualified and
// identical to every other read here, and it means there is no shape of this
// call that returns somebody else's booking to be filtered out afterwards.
func (repository *PostgresRepository) ParentBookings(ctx context.Context, request ParentBookingsRequest) ([]Booking, error) {
    if err := request.Validate(); err != nil {
        return nil, err
    }

    rows, err := repository.pool.Query(ctx, `
        select `+bookingColumns+`
        from bookings
        where student_id in (select id from students where parent_id = $1)
        order by created_at desc, id desc
        limit $2`, request.ParentID, request.Limit)
    if err != nil {
        return nil, translate(err, nil)
    }

    defer rows.Close()

    matched := make([]Booking, 0, request.Limit)

    for rows.Next() {
        stored, err := scanBooking(rows)
        if err != nil {
            return nil, translate(err, nil)
        }

        matched = append(matched, stored)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return matched, nil
}

// Expire releases a hold whose deadline has passed, which is what puts the seat
// behind a parent who walked away back in front of everyone else.
//
// The row is locked before the deadline is read, so two workers that both got
// the job cannot both write the transition. The second one finds the booking is
// no longer pending_payment and is told so.
func (repository *PostgresRepository) Expire(ctx context.Context, request ExpireRequest) (result Booking, err error) {
    transaction, err := repository.pool.Begin(ctx)
    if err != nil {
        return Booking{}, fmt.Errorf("cannot open the expiry transaction: %w", translate(err, nil))
    }

    // Registered before the rollback below, so it runs after it and reads the
    // error this method is actually returning.
    defer func() { repository.countTransaction(transactionExpire, err) }()

    defer transaction.Rollback(ctx)

    stored, err := scanBooking(transaction.QueryRow(ctx,
        `select `+bookingColumns+` from bookings where id = $1 for update`, request.BookingID))
    if err != nil {
        return Booking{}, translate(err, ErrBookingNotFound)
    }

    if stored.Status != StatusPendingPayment {
        return stored, ErrNotHolding
    }

    if HoldIsLive(stored.HoldExpiresAt, request.Now) {
        return stored, ErrHoldStillLive
    }

    lapsed, err := scanBooking(transaction.QueryRow(ctx, `
        update bookings
        set status = 'expired', hold_expires_at = null, updated_at = $2
        where id = $1
        returning `+bookingColumns, request.BookingID, request.Now))
    if err != nil {
        return Booking{}, translate(err, nil)
    }

    if err := recordEvent(ctx, transaction, lapsed.ID, stored.Status, StatusExpired, ActorSystem, "hold deadline passed", request.Now); err != nil {
        return Booking{}, err
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the expiry could not be committed: %w", translate(err, nil))
    }

    return lapsed, nil
}

// settleSeatLost writes the outcome for a parent whose payment settled after
// the last seat was gone, and commits it before reporting the failure.
func (repository *PostgresRepository) settleSeatLost(ctx context.Context, transaction pgx.Tx, stored Booking, now time.Time) (Booking, error) {
    lost, err := scanBooking(transaction.QueryRow(ctx, `
        update bookings
        set status = 'refund_required', hold_expires_at = null, updated_at = $2
        where id = $1 and status = 'pending_payment'
        returning `+bookingColumns, stored.ID, now))
    if err != nil {
        return Booking{}, translate(err, ErrNotHolding)
    }

    if err := recordEvent(ctx, transaction, lost.ID, StatusPendingPayment, StatusRefundRequired, ActorSystem, "no free seat under the class lock", now); err != nil {
        return Booking{}, err
    }

    if err := transaction.Commit(ctx); err != nil {
        return Booking{}, fmt.Errorf("the lost seat could not be recorded: %w", translate(err, nil))
    }

    return lost, ErrSeatLost
}

// recordEvent appends one line to the audit trail. The id is minted here
// because an event is the repository's own record of what it just did.
func recordEvent(ctx context.Context, transaction pgx.Tx, bookingID string, from Status, to Status, actor Actor, reason string, now time.Time) error {
    eventID, err := identifier.NewUUIDv7()
    if err != nil {
        return err
    }

    var fromStatus *string

    if from != "" {
        value := string(from)
        fromStatus = &value
    }

    var reasonText *string

    if reason != "" {
        reasonText = &reason
    }

    _, err = transaction.Exec(ctx, `
        insert into booking_events (id, booking_id, from_status, to_status, actor, reason, created_at)
        values ($1, $2, $3::text::booking_status, $4::text::booking_status, $5, $6, $7)`,
        eventID, bookingID, fromStatus, string(to), string(actor), reasonText, now)
    if err != nil {
        return fmt.Errorf("the audit trail could not be written: %w", translate(err, nil))
    }

    return nil
}

// scanBooking reads one row into a Booking, turning the three nullable columns
// into zero values so no caller carries a nil check.
func scanBooking(row pgx.Row) (Booking, error) {
    var (
        stored        Booking
        seatNo        *int16
        holdExpiresAt *time.Time
        confirmedAt   *time.Time
    )

    err := row.Scan(&stored.ID, &stored.StudentID, &stored.ClassID, &stored.Status,
        &seatNo, &holdExpiresAt, &confirmedAt, &stored.CreatedAt, &stored.UpdatedAt)
    if err != nil {
        return Booking{}, err
    }

    if seatNo != nil {
        stored.SeatNo = *seatNo
    }

    if holdExpiresAt != nil {
        stored.HoldExpiresAt = *holdExpiresAt
    }

    if confirmedAt != nil {
        stored.ConfirmedAt = *confirmedAt
    }

    return stored, nil
}

// scanClass reads one row into a Class.
func scanClass(row pgx.Row) (Class, error) {
    var class Class

    err := row.Scan(&class.ID, &class.Subject, &class.Title, &class.StartsAt,
        &class.DurationMinutes, &class.Capacity, &class.HoldAllowance)
    if err != nil {
        return Class{}, err
    }

    return class, nil
}

// translate turns a driver failure into one of this package's own failures.
//
// Param:
// err - error (whatever the driver reported)
// missing - error (what an empty result means here, nil when it cannot happen)
//
// Return:
//   - the package failure when the cause is one this package names
//   - the original error otherwise, so nothing unexpected is silently reshaped
func translate(err error, missing error) error {
    if err == nil {
        return nil
    }

    if errors.Is(err, pgx.ErrNoRows) && missing != nil {
        return missing
    }

    var databaseError *pgconn.PgError

    if !errors.As(err, &databaseError) {
        return err
    }

    switch databaseError.Code {
    case uniqueViolation:
        switch databaseError.ConstraintName {
        case uniqueBookingActiveIndex:
            return ErrAlreadyBooked
        case uniqueSeatTakenIndex:
            return ErrSeatLost
        }
    case foreignKeyViolation:
        if strings.Contains(databaseError.ConstraintName, "class") {
            return ErrClassNotFound
        }

        return ErrStudentNotFound
    case invalidTextRepresentation:
        return ErrInvalidRequest
    }

    return err
}
