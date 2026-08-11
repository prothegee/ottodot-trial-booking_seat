package payment

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
)

// Postgres error codes this repository turns into its own failures. A driver
// code reaching a caller would make the http layer depend on the database
// vendor, which is a coupling nobody notices until it is expensive.
const (
    uniqueViolation           = "23505"
    foreignKeyViolation       = "23503"
    checkViolation            = "23514"
    invalidTextRepresentation = "22P02"

    uniquePaymentIdempotencyIndex = "uq_payment_idempotency"
    amountPositiveConstraint      = "payment_attempts_amount_positive"
)

// attemptColumns is the read shape for an attempt. The enum is cast to text so
// the driver never has to know about a database side type.
const attemptColumns = `id, booking_id, idempotency_key, amount_cents, currency, status::text, provider_ref, failure_reason, created_at, settled_at`

// PostgresRepository is the real one. It is where the uq_payment_idempotency
// index does the work the fake does with a mutex.
type PostgresRepository struct {
    pool *pgxpool.Pool
}

// NewPostgresRepository wraps a pool.
//
// Note:
//   - the pool must be the primary. Every method here either writes or decides
//     whether a charge already happened, and the replica is asynchronous, so a
//     replayed key read from it could miss the row that stops a second charge.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
    return &PostgresRepository{pool: pool}
}

// Attempt reads one attempt.
func (repository *PostgresRepository) Attempt(ctx context.Context, attemptID string) (Attempt, error) {
    stored, err := scanAttempt(repository.pool.QueryRow(ctx,
        `select `+attemptColumns+` from payment_attempts where id = $1`, attemptID))
    if err != nil {
        return Attempt{}, translate(err, ErrAttemptNotFound)
    }

    return stored, nil
}

// AttemptByKey reads the attempt an idempotency key already produced.
func (repository *PostgresRepository) AttemptByKey(ctx context.Context, bookingID string, idempotencyKey string) (Attempt, error) {
    stored, err := scanAttempt(repository.pool.QueryRow(ctx,
        `select `+attemptColumns+` from payment_attempts where booking_id = $1 and idempotency_key = $2`,
        bookingID, idempotencyKey))
    if err != nil {
        return Attempt{}, translate(err, ErrAttemptNotFound)
    }

    return stored, nil
}

// AttemptsFor lists every attempt against one booking, oldest first.
func (repository *PostgresRepository) AttemptsFor(ctx context.Context, bookingID string) ([]Attempt, error) {
    rows, err := repository.pool.Query(ctx,
        `select `+attemptColumns+` from payment_attempts where booking_id = $1 order by created_at, id`, bookingID)
    if err != nil {
        return nil, translate(err, nil)
    }

    defer rows.Close()

    var found []Attempt

    for rows.Next() {
        stored, err := scanAttempt(rows)
        if err != nil {
            return nil, translate(err, nil)
        }

        found = append(found, stored)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return found, nil
}

// Begin writes a new attempt, or reports the one this key already produced.
//
// Note:
//   - the key is looked up first and the insert is still allowed to fail on
//     uq_payment_idempotency. The lookup is the ordinary replay, the index is
//     what holds when two calls with one key arrive at the same instant, and
//     only the index can be trusted for that.
func (repository *PostgresRepository) Begin(ctx context.Context, request BeginRequest) (Attempt, bool, error) {
    if request.AttemptID == "" || request.BookingID == "" {
        return Attempt{}, false, ErrInvalidRequest
    }

    if err := ValidateIdempotencyKey(request.IdempotencyKey); err != nil {
        return Attempt{}, false, err
    }

    if err := request.Amount.Validate(); err != nil {
        return Attempt{}, false, err
    }

    amount := request.Amount.Normalised()

    replayed, err := repository.AttemptByKey(ctx, request.BookingID, request.IdempotencyKey)
    if err == nil {
        return matchReplay(replayed, amount)
    }

    if !errors.Is(err, ErrAttemptNotFound) {
        return Attempt{}, false, err
    }

    opened, err := scanAttempt(repository.pool.QueryRow(ctx, `
        insert into payment_attempts (id, booking_id, idempotency_key, amount_cents, currency, status, created_at)
        values ($1, $2, $3, $4, $5, 'initiated', $6)
        returning `+attemptColumns,
        request.AttemptID, request.BookingID, request.IdempotencyKey, amount.Cents, amount.Currency, request.Now))

    if errors.Is(translate(err, nil), ErrIdempotencyConflict) {
        // Another call with the same key committed between the lookup and this
        // insert. The row it wrote is the one that counts, so this call replays
        // it rather than reporting a failure nobody caused.
        lost, readErr := repository.AttemptByKey(ctx, request.BookingID, request.IdempotencyKey)
        if readErr != nil {
            return Attempt{}, false, readErr
        }

        return matchReplay(lost, amount)
    }

    if err != nil {
        return Attempt{}, false, translate(err, nil)
    }

    return opened, false, nil
}

// Settle records the provider's answer. The status guard in the update is what
// keeps the row append only once it has settled.
func (repository *PostgresRepository) Settle(ctx context.Context, request SettleRequest) (Attempt, error) {
    if err := request.Validate(); err != nil {
        return Attempt{}, err
    }

    settled, err := scanAttempt(repository.pool.QueryRow(ctx, `
        update payment_attempts
        set status = $2::text::payment_status, provider_ref = $3, failure_reason = $4, settled_at = $5
        where id = $1 and status = 'initiated'
        returning `+attemptColumns,
        request.AttemptID, string(request.Status),
        emptyToNull(request.ProviderRef), emptyToNull(request.FailureReason), request.Now))

    if errors.Is(err, pgx.ErrNoRows) {
        // Either the attempt is not there at all, or it already settled. The
        // two are different answers to the caller, so the row is read once to
        // tell them apart.
        stored, readErr := repository.Attempt(ctx, request.AttemptID)
        if readErr != nil {
            return Attempt{}, readErr
        }

        return stored, ErrAlreadySettled
    }

    if err != nil {
        return Attempt{}, fmt.Errorf("the attempt could not be settled: %w", translate(err, nil))
    }

    return settled, nil
}

// matchReplay decides whether a row found under an idempotency key is the same
// charge being retried, or a different one wearing the first one's key.
func matchReplay(stored Attempt, amount Amount) (Attempt, bool, error) {
    if !stored.Amount.SameAs(amount) {
        return Attempt{}, false, ErrIdempotencyConflict
    }

    return stored, true, nil
}

// emptyToNull writes an absent value as null rather than as an empty string, so
// the column reads the way the schema describes it.
func emptyToNull(value string) *string {
    if value == "" {
        return nil
    }

    return &value
}

// scanAttempt reads one row into an Attempt, turning the two nullable columns
// into zero values so no caller carries a nil check.
func scanAttempt(row pgx.Row) (Attempt, error) {
    var (
        stored        Attempt
        providerRef   *string
        failureReason *string
        settledAt     *time.Time
    )

    err := row.Scan(&stored.ID, &stored.BookingID, &stored.IdempotencyKey,
        &stored.Amount.Cents, &stored.Amount.Currency, &stored.Status,
        &providerRef, &failureReason, &stored.CreatedAt, &settledAt)
    if err != nil {
        return Attempt{}, err
    }

    if providerRef != nil {
        stored.ProviderRef = *providerRef
    }

    if failureReason != nil {
        stored.FailureReason = *failureReason
    }

    if settledAt != nil {
        stored.SettledAt = *settledAt
    }

    return stored, nil
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
        if databaseError.ConstraintName == uniquePaymentIdempotencyIndex {
            return ErrIdempotencyConflict
        }
    case foreignKeyViolation:
        return ErrBookingNotFound
    case checkViolation:
        if databaseError.ConstraintName == amountPositiveConstraint {
            return ErrInvalidAmount
        }
    case invalidTextRepresentation:
        return ErrInvalidRequest
    }

    return err
}
