package queue

import (
	"context"
	"time"
)

// Queue is the only way background work is scheduled and consumed.
//
// It exists as an interface for one reason: the four fast test tiers run
// against a fake, and the same behaviour suite runs against real Postgres in
// the proof tier. A fake that quietly disagrees with the sql is the risk the
// shared suite exists to catch, and here the disagreement that matters is
// whether two workers can claim one job.
//
// Every method is one statement. The caller never reads a job and then writes
// it back, because that is precisely how the same job gets run twice.
type Queue interface {
	// Enqueue writes one job. The caller mints the id, so it knows what it
	// scheduled even if the write then fails.
	Enqueue(ctx context.Context, request EnqueueRequest) (Job, error)

	// Claim takes up to Limit jobs and leases them.
	//
	// This is the method the whole package is built around. It skips rows
	// another worker holds, skips rows whose instant has not arrived, skips
	// rows that have spent their attempts, and spends one attempt on every row
	// it hands back.
	Claim(ctx context.Context, request ClaimRequest) ([]Job, error)

	// Complete removes a finished job. It reports ErrJobNotFound when the row
	// is already gone.
	Complete(ctx context.Context, jobID string) error

	// Release hands a job back unfinished, so another claim can pick it up
	// after RunAfter. The attempt it spent is not returned.
	Release(ctx context.Context, request ReleaseRequest) error

	// Job reads one job. It reports ErrJobNotFound when there is none, and it
	// exists for tests and the operator queue view rather than for the runner.
	Job(ctx context.Context, jobID string) (Job, error)

	// Depth counts what is waiting, what is held, and what has been parked.
	Depth(ctx context.Context, request DepthRequest) (Depth, error)
}

// EnqueueRequest is everything the write needs.
//
// The instant and the identifier are handed in rather than read inside the
// implementation, so both answer identically and a test can pin the clock
// instead of sleeping.
type EnqueueRequest struct {
	// JobID is minted by the caller.
	JobID string

	Kind Kind

	// Payload is a json object. This package never reads inside it.
	Payload []byte

	// RunAfter is the earliest instant the job may be claimed. A zero value
	// means now, which is what an immediate job wants.
	RunAfter time.Time

	// Now stamps created_at, and fills RunAfter when it was left zero.
	Now time.Time
}

// Validate refuses a request that would write a row nobody can act on, before
// either implementation touches storage.
//
// Return:
//   - nil when the request describes a job this service runs
//   - ErrInvalidRequest, ErrUnknownKind, or ErrInvalidPayload, each naming what
//     is wrong
func (request EnqueueRequest) Validate() error {
	if request.JobID == "" || request.Now.IsZero() {
		return ErrInvalidRequest
	}

	if !request.Kind.IsKnown() {
		return ErrUnknownKind
	}

	if !validPayload(request.Payload) {
		return ErrInvalidPayload
	}

	return nil
}

// ClaimRequest is everything one poll needs.
type ClaimRequest struct {
	// Now is the instant the claim is judged at. It decides which jobs are due
	// and which leases have lapsed.
	Now time.Time

	// Lease is how long the claim holds. It has to be longer than the slowest
	// handler, because a lease that lapses mid-job lets a second worker start
	// the same work.
	Lease time.Duration

	// Limit caps how many jobs one poll takes. It exists so a worker cannot
	// lease the whole table and then die holding it.
	Limit int

	// MaxAttempts is where a job stops being handed out. A job at or past this
	// is parked, left in the table, and never claimed again.
	MaxAttempts int
}

// Validate refuses a poll that could not behave, before any row is locked.
//
// Return:
//   - nil when the request describes a claim that can be honoured
//   - ErrInvalidRequest otherwise
func (request ClaimRequest) Validate() error {
	if request.Now.IsZero() || request.Lease <= 0 {
		return ErrInvalidRequest
	}

	if request.Limit < 1 || request.MaxAttempts < 1 {
		return ErrInvalidRequest
	}

	return nil
}

// ReleaseRequest hands one job back.
type ReleaseRequest struct {
	JobID string

	// RunAfter is when the job may be claimed again. A caller backing off puts
	// it in the future, and a caller that simply cannot run right now puts it
	// at the present instant.
	RunAfter time.Time
}

// Validate refuses a release that names nothing or schedules nothing.
func (request ReleaseRequest) Validate() error {
	if request.JobID == "" || request.RunAfter.IsZero() {
		return ErrInvalidRequest
	}

	return nil
}

// DepthRequest is what the counts are measured against.
type DepthRequest struct {
	Now time.Time

	// MaxAttempts is the same value the runner claims with. Counting parked
	// jobs against a different number would report a depth the runner does not
	// agree with.
	MaxAttempts int
}

// Validate refuses a count that would be measured against nothing.
func (request DepthRequest) Validate() error {
	if request.Now.IsZero() || request.MaxAttempts < 1 {
		return ErrInvalidRequest
	}

	return nil
}

// Depth is the queue in three numbers.
//
// They are separate rather than one total because they mean different things to
// whoever is looking. Ready rising means the worker is behind. Claimed rising
// means jobs are slow. Parked rising means something is broken and no amount of
// waiting will fix it.
type Depth struct {
	// Ready is due, unclaimed, and still has attempts left.
	Ready int

	// Claimed is currently leased by a worker.
	Claimed int

	// Parked has spent its attempts and will not be handed out again.
	Parked int
}
