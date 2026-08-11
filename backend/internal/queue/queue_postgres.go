package queue

import (
    "context"
    "errors"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
)

// Postgres error codes this queue turns into its own failures. A driver code
// reaching a caller would make the worker depend on the database vendor, which
// is a coupling nobody notices until it is expensive.
const (
    uniqueViolation           = "23505"
    checkViolation            = "23514"
    invalidTextRepresentation = "22P02"

    kindAllowedConstraint = "job_queue_kind_allowed"
)

// jobColumns is the read shape for a job.
const jobColumns = `id, kind, payload, run_after, attempts, locked_until, created_at`

// PostgresQueue is the real one. It is where FOR UPDATE SKIP LOCKED does the
// work the fake does with a mutex.
type PostgresQueue struct {
    pool *pgxpool.Pool
}

// NewPostgresQueue wraps a pool.
//
// Note:
//   - the pool must be the primary. Every method here either writes or decides
//     whether a job is already being run, and the replica is asynchronous, so a
//     claim judged against it could hand one job to two workers.
func NewPostgresQueue(pool *pgxpool.Pool) *PostgresQueue {
    return &PostgresQueue{pool: pool}
}

// Enqueue writes one job.
func (queue *PostgresQueue) Enqueue(ctx context.Context, request EnqueueRequest) (Job, error) {
    if err := request.Validate(); err != nil {
        return Job{}, err
    }

    written, err := scanJob(queue.pool.QueryRow(ctx, `
        insert into job_queue (id, kind, payload, run_after, created_at)
        values ($1, $2, $3, $4, $5)
        returning `+jobColumns,
        request.JobID, string(request.Kind), request.Payload, scheduledAt(request), request.Now))
    if err != nil {
        return Job{}, fmt.Errorf("the job could not be written: %w", translate(err, nil))
    }

    return written, nil
}

// Claim takes up to Limit due jobs and leases them.
//
// This is the one statement the whole package exists for, so it is worth
// reading slowly:
//
//	the inner select finds due, unclaimed, unparked rows, locks them, and
//	SKIP LOCKED steps over any row another worker locked a moment ago
//	the outer update leases what it locked and spends one attempt
//
// Both halves are one statement, so there is no instant between finding a job
// and owning it. A second worker arriving mid-flight sees locked rows, skips
// them, and takes different ones, which is the behaviour the proof tier asserts
// with real parallel connections.
func (queue *PostgresQueue) Claim(ctx context.Context, request ClaimRequest) ([]Job, error) {
    if err := request.Validate(); err != nil {
        return nil, err
    }

    rows, err := queue.pool.Query(ctx, `
        with claimable as (
            select id
            from job_queue
            where run_after <= $1
              and (locked_until is null or locked_until <= $1)
              and attempts < $2
            order by run_after, id
            limit $3
            for update skip locked
        )
        update job_queue
        set locked_until = $4, attempts = attempts + 1
        where id in (select id from claimable)
        returning `+jobColumns,
        request.Now, request.MaxAttempts, request.Limit, request.Now.Add(request.Lease))
    if err != nil {
        return nil, fmt.Errorf("the queue could not be polled: %w", translate(err, nil))
    }

    defer rows.Close()

    leased := make([]Job, 0, request.Limit)

    for rows.Next() {
        job, scanErr := scanJob(rows)
        if scanErr != nil {
            return nil, scanErr
        }

        leased = append(leased, job)
    }

    if err := rows.Err(); err != nil {
        return nil, translate(err, nil)
    }

    return leased, nil
}

// Complete removes a finished job.
func (queue *PostgresQueue) Complete(ctx context.Context, jobID string) error {
    if jobID == "" {
        return ErrInvalidRequest
    }

    tag, err := queue.pool.Exec(ctx, `delete from job_queue where id = $1`, jobID)
    if err != nil {
        return fmt.Errorf("the job could not be completed: %w", translate(err, nil))
    }

    if tag.RowsAffected() == 0 {
        return ErrJobNotFound
    }

    return nil
}

// Release hands a job back unfinished, keeping the attempt it spent.
func (queue *PostgresQueue) Release(ctx context.Context, request ReleaseRequest) error {
    if err := request.Validate(); err != nil {
        return err
    }

    tag, err := queue.pool.Exec(ctx,
        `update job_queue set locked_until = null, run_after = $2 where id = $1`,
        request.JobID, request.RunAfter)
    if err != nil {
        return fmt.Errorf("the job could not be released: %w", translate(err, nil))
    }

    if tag.RowsAffected() == 0 {
        return ErrJobNotFound
    }

    return nil
}

// Job reads one job.
func (queue *PostgresQueue) Job(ctx context.Context, jobID string) (Job, error) {
    if jobID == "" {
        return Job{}, ErrInvalidRequest
    }

    stored, err := scanJob(queue.pool.QueryRow(ctx,
        `select `+jobColumns+` from job_queue where id = $1`, jobID))
    if err != nil {
        return Job{}, translate(err, ErrJobNotFound)
    }

    return stored, nil
}

// Depth counts what is waiting, held, and parked.
//
// The three counts come from one scan rather than three queries, so they
// describe the same instant. Three separate counts could add up to a table that
// never existed.
func (queue *PostgresQueue) Depth(ctx context.Context, request DepthRequest) (Depth, error) {
    if err := request.Validate(); err != nil {
        return Depth{}, err
    }

    var counted Depth

    err := queue.pool.QueryRow(ctx, `
        select
            count(*) filter (
                where attempts < $2
                  and run_after <= $1
                  and (locked_until is null or locked_until <= $1)
            ),
            count(*) filter (where attempts < $2 and locked_until > $1),
            count(*) filter (where attempts >= $2)
        from job_queue`,
        request.Now, request.MaxAttempts).Scan(&counted.Ready, &counted.Claimed, &counted.Parked)
    if err != nil {
        return Depth{}, fmt.Errorf("the queue depth could not be read: %w", translate(err, nil))
    }

    return counted, nil
}

// scanJob reads one row into a Job, turning the nullable lease into a zero
// value so no caller carries a nil check.
func scanJob(row pgx.Row) (Job, error) {
    var (
        stored      Job
        lockedUntil *time.Time
    )

    err := row.Scan(&stored.ID, &stored.Kind, &stored.Payload,
        &stored.RunAfter, &stored.Attempts, &lockedUntil, &stored.CreatedAt)
    if err != nil {
        return Job{}, err
    }

    if lockedUntil != nil {
        stored.LockedUntil = *lockedUntil
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
        return ErrDuplicateJob
    case checkViolation:
        if databaseError.ConstraintName == kindAllowedConstraint {
            return ErrUnknownKind
        }
    case invalidTextRepresentation:
        // A malformed uuid never matches a row, so the honest answer is the
        // same one an absent row gets rather than a driver message about
        // syntax.
        if missing != nil {
            return missing
        }

        return ErrInvalidRequest
    }

    return err
}
