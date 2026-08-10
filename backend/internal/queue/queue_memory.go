package queue

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryQueue is the fake every fast test runs against.
//
// It holds the same rules the database holds, under one mutex instead of row
// locks. That is the point of it: the four fast tiers can prove the runner
// claims, completes, releases, and parks correctly, in a second, with nothing
// running.
//
// What it cannot prove is that FOR UPDATE SKIP LOCKED stops two real
// transactions taking the same row, because there is no transaction here. A
// mutex serializes the two claims, so of course they differ. That is why the
// same behaviour suite also runs against Postgres in the proof tier.
type MemoryQueue struct {
	mutex sync.Mutex
	jobs  map[string]Job
}

// NewMemoryQueue builds an empty queue.
func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{jobs: make(map[string]Job)}
}

// Enqueue writes one job.
func (memory *MemoryQueue) Enqueue(_ context.Context, request EnqueueRequest) (Job, error) {
	if err := request.Validate(); err != nil {
		return Job{}, err
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	if _, taken := memory.jobs[request.JobID]; taken {
		return Job{}, ErrDuplicateJob
	}

	written := Job{
		ID:        request.JobID,
		Kind:      request.Kind,
		Payload:   copyPayload(request.Payload),
		RunAfter:  scheduledAt(request),
		CreatedAt: request.Now,
	}

	memory.jobs[written.ID] = written

	return written, nil
}

// Claim takes up to Limit due jobs and leases them.
func (memory *MemoryQueue) Claim(_ context.Context, request ClaimRequest) ([]Job, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	claimable := memory.claimableLocked(request)

	sort.Slice(claimable, func(first int, second int) bool {
		if !claimable[first].RunAfter.Equal(claimable[second].RunAfter) {
			return claimable[first].RunAfter.Before(claimable[second].RunAfter)
		}

		return claimable[first].ID < claimable[second].ID
	})

	if len(claimable) > request.Limit {
		claimable = claimable[:request.Limit]
	}

	leased := make([]Job, 0, len(claimable))

	for _, job := range claimable {
		job.Attempts++
		job.LockedUntil = request.Now.Add(request.Lease)

		memory.jobs[job.ID] = job
		leased = append(leased, job)
	}

	return leased, nil
}

// Complete removes a finished job.
func (memory *MemoryQueue) Complete(_ context.Context, jobID string) error {
	if jobID == "" {
		return ErrInvalidRequest
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	if _, found := memory.jobs[jobID]; !found {
		return ErrJobNotFound
	}

	delete(memory.jobs, jobID)

	return nil
}

// Release hands a job back unfinished, keeping the attempt it spent.
func (memory *MemoryQueue) Release(_ context.Context, request ReleaseRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	held, found := memory.jobs[request.JobID]
	if !found {
		return ErrJobNotFound
	}

	held.LockedUntil = time.Time{}
	held.RunAfter = request.RunAfter

	memory.jobs[held.ID] = held

	return nil
}

// Job reads one job.
func (memory *MemoryQueue) Job(_ context.Context, jobID string) (Job, error) {
	if jobID == "" {
		return Job{}, ErrInvalidRequest
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	held, found := memory.jobs[jobID]
	if !found {
		return Job{}, ErrJobNotFound
	}

	return held, nil
}

// Depth counts what is waiting, held, and parked.
func (memory *MemoryQueue) Depth(_ context.Context, request DepthRequest) (Depth, error) {
	if err := request.Validate(); err != nil {
		return Depth{}, err
	}

	memory.mutex.Lock()
	defer memory.mutex.Unlock()

	var counted Depth

	for _, job := range memory.jobs {
		switch {
		case job.IsParked(request.MaxAttempts):
			counted.Parked++
		case job.IsClaimed(request.Now):
			counted.Claimed++
		case job.IsDue(request.Now):
			counted.Ready++
		}
	}

	return counted, nil
}

// claimableLocked lists the jobs a poll may take, in no particular order.
//
// The three conditions are the same three the sql has, written in the same
// order, so a reader can hold one against the other.
func (memory *MemoryQueue) claimableLocked(request ClaimRequest) []Job {
	var claimable []Job

	for _, job := range memory.jobs {
		if !job.IsDue(request.Now) {
			continue
		}

		if job.IsClaimed(request.Now) {
			continue
		}

		if job.IsParked(request.MaxAttempts) {
			continue
		}

		claimable = append(claimable, job)
	}

	return claimable
}

// scheduledAt resolves the instant a job becomes claimable. A request that left
// RunAfter alone means now, which is what an immediate job wants and what the
// column default does on the sql side.
func scheduledAt(request EnqueueRequest) time.Time {
	if request.RunAfter.IsZero() {
		return request.Now
	}

	return request.RunAfter
}

// copyPayload takes the caller's bytes out of their hands. Without this a caller
// reusing its buffer would rewrite a job already in the queue, which the sql
// version cannot do and the fake therefore must not either.
func copyPayload(payload []byte) []byte {
	held := make([]byte, len(payload))
	copy(held, payload)

	return held
}
