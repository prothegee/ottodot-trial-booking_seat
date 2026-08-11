package queue_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/identifier"
    "ottodot-trial-booking/backend/internal/queue"
)

// The contract every queue has to satisfy.
//
// The risk this file exists to remove: the fake implements leasing and parking
// correctly while the sql is wrong, and every fast test stays green. One suite
// pointed at both implementations is the only thing that catches that, so this
// file is run twice, once against the memory queue in the fast tiers and once
// against real Postgres behind the containers tag.
//
// One rule it cannot carry is the one that matters most under load: that two
// workers polling at the same instant never get the same job. A mutex makes the
// fake pass it for the wrong reason. That proof lives beside the Postgres
// fixture, on real parallel connections.

// contractMoment is the instant every case starts from. Whole seconds, because
// the column stores microseconds and a rounder value reads the same on both
// sides.
var contractMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// The lease and cap every case claims with, unless the case is about one of
// them.
const (
    contractLease       = time.Minute
    contractMaxAttempts = 3
    contractLimit       = 10
)

// queueFixture is one queue, however it was built. The Postgres version brings
// a throwaway schema with it, the memory version brings a map.
type queueFixture interface {
    Queue() queue.Queue
}

// newJobID mints an identifier in the same format the worker uses, so the
// Postgres uuid column accepts it.
func newJobID(t *testing.T) string {
    t.Helper()

    minted, err := identifier.NewUUIDv7()
    if err != nil {
        t.Fatalf("cannot mint an identifier: %v", err)
    }

    return minted
}

// enqueueRequestFor builds a job carrying a fresh booking payload.
func enqueueRequestFor(t *testing.T, kind queue.Kind, runAfter time.Time) queue.EnqueueRequest {
    t.Helper()

    payload, err := queue.EncodeBookingPayload(newJobID(t))
    if err != nil {
        t.Fatalf("cannot encode a payload: %v", err)
    }

    return queue.EnqueueRequest{
        JobID:    newJobID(t),
        Kind:     kind,
        Payload:  payload,
        RunAfter: runAfter,
        Now:      contractMoment,
    }
}

// mustEnqueue writes one job and fails the test if it was refused.
func mustEnqueue(t *testing.T, target queue.Queue, kind queue.Kind, runAfter time.Time) queue.Job {
    t.Helper()

    written, err := target.Enqueue(context.Background(), enqueueRequestFor(t, kind, runAfter))
    if err != nil {
        t.Fatalf("expected the job to be written, got: %v", err)
    }

    return written
}

// claimAt polls the queue at one instant with the suite's lease and cap.
func claimAt(t *testing.T, target queue.Queue, at time.Time) []queue.Job {
    t.Helper()

    leased, err := target.Claim(context.Background(), queue.ClaimRequest{
        Now: at, Lease: contractLease, Limit: contractLimit, MaxAttempts: contractMaxAttempts,
    })
    if err != nil {
        t.Fatalf("expected the poll to answer, got: %v", err)
    }

    return leased
}

// runQueueContract is the suite itself. Every case gets its own fixture, so
// nothing leaks from one to the next.
func runQueueContract(t *testing.T, newFixture func(t *testing.T) queueFixture) {
    t.Helper()

    ctx := context.Background()

    t.Run("integration: an enqueued job comes back with what was put in", func(t *testing.T) {
        target := newFixture(t).Queue()

        request := enqueueRequestFor(t, queue.KindExpireHold, contractMoment)

        written, err := target.Enqueue(ctx, request)
        if err != nil {
            t.Fatalf("expected the job to be written, got: %v", err)
        }

        if written.ID != request.JobID || written.Kind != queue.KindExpireHold {
            t.Fatalf("expected the job that was asked for, got %+v", written)
        }

        if written.Attempts != 0 || !written.LockedUntil.IsZero() {
            t.Fatalf("a new job has run nothing and is held by nobody, got %+v", written)
        }

        // The payload is compared decoded rather than byte for byte, because
        // jsonb normalises what it stores and the fake does not.
        decoded, err := queue.DecodeBookingPayload(written.Payload)
        if err != nil {
            t.Fatalf("expected the payload to survive, got: %v", err)
        }

        if decoded.BookingID == "" {
            t.Fatal("expected the payload to carry the booking it was given")
        }
    })

    t.Run("integration: a claim leases the job and spends one attempt", func(t *testing.T) {
        target := newFixture(t).Queue()
        written := mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        leased := claimAt(t, target, contractMoment)

        if len(leased) != 1 || leased[0].ID != written.ID {
            t.Fatalf("expected the one job back, got %d", len(leased))
        }

        if leased[0].Attempts != 1 {
            t.Fatalf("expected one attempt spent, got %d", leased[0].Attempts)
        }

        if !leased[0].IsClaimed(contractMoment) {
            t.Fatalf("expected the job to be held, got a lease of %v", leased[0].LockedUntil)
        }
    })

    t.Run("integration: a claimed job is invisible to the next poll", func(t *testing.T) {
        // This is the rule the whole package is built for, checked here in its
        // simplest form. The parallel version lives with the Postgres fixture.
        target := newFixture(t).Queue()
        mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        if len(claimAt(t, target, contractMoment)) != 1 {
            t.Fatal("expected the first poll to take the job")
        }

        if taken := claimAt(t, target, contractMoment); len(taken) != 0 {
            t.Fatalf("expected the second poll to find nothing, got %d", len(taken))
        }
    })

    t.Run("integration: a lapsed lease makes the job claimable again", func(t *testing.T) {
        // A worker that dies holding a job clears nothing. Recovery is the
        // lease simply stopping being believed.
        target := newFixture(t).Queue()
        mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        claimAt(t, target, contractMoment)

        recovered := claimAt(t, target, contractMoment.Add(contractLease))

        if len(recovered) != 1 {
            t.Fatalf("expected the abandoned job back, got %d", len(recovered))
        }

        if recovered[0].Attempts != 2 {
            t.Fatalf("expected the dead worker's attempt to still be spent, got %d", recovered[0].Attempts)
        }
    })

    t.Run("integration: a job scheduled ahead is left alone until its instant", func(t *testing.T) {
        target := newFixture(t).Queue()
        mustEnqueue(t, target, queue.KindExpireHold, contractMoment.Add(10*time.Minute))

        if early := claimAt(t, target, contractMoment); len(early) != 0 {
            t.Fatalf("expected nothing before the instant, got %d", len(early))
        }

        if due := claimAt(t, target, contractMoment.Add(10*time.Minute)); len(due) != 1 {
            t.Fatalf("expected the job once its instant arrived, got %d", len(due))
        }
    })

    t.Run("integration: completing a job removes it", func(t *testing.T) {
        target := newFixture(t).Queue()
        written := mustEnqueue(t, target, queue.KindReconcileRefund, contractMoment)

        claimAt(t, target, contractMoment)

        if err := target.Complete(ctx, written.ID); err != nil {
            t.Fatalf("expected the job to be completed, got: %v", err)
        }

        if _, err := target.Job(ctx, written.ID); !errors.Is(err, queue.ErrJobNotFound) {
            t.Fatalf("expected the job to be gone, got: %v", err)
        }
    })

    t.Run("integration: releasing a job hands it back and keeps the attempt", func(t *testing.T) {
        target := newFixture(t).Queue()
        written := mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        claimAt(t, target, contractMoment)

        backoff := contractMoment.Add(30 * time.Second)

        if err := target.Release(ctx, queue.ReleaseRequest{JobID: written.ID, RunAfter: backoff}); err != nil {
            t.Fatalf("expected the job to be released, got: %v", err)
        }

        if early := claimAt(t, target, contractMoment); len(early) != 0 {
            t.Fatalf("expected the backoff to hold, got %d", len(early))
        }

        retried := claimAt(t, target, backoff)

        if len(retried) != 1 {
            t.Fatalf("expected the job back after the backoff, got %d", len(retried))
        }

        if retried[0].Attempts != 2 {
            t.Fatalf("expected the failed attempt to still count, got %d", retried[0].Attempts)
        }
    })

    t.Run("integration: a job that spends its attempts is parked rather than looped", func(t *testing.T) {
        target := newFixture(t).Queue()
        written := mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        at := contractMoment

        for spent := 0; spent < contractMaxAttempts; spent++ {
            if len(claimAt(t, target, at)) != 1 {
                t.Fatalf("expected attempt %d to be handed out", spent+1)
            }

            if err := target.Release(ctx, queue.ReleaseRequest{JobID: written.ID, RunAfter: at}); err != nil {
                t.Fatalf("expected the job to be released, got: %v", err)
            }
        }

        if parked := claimAt(t, target, at); len(parked) != 0 {
            t.Fatalf("expected the job to stop being handed out, got %d", len(parked))
        }

        // It is parked, not deleted. An operator has to be able to see it.
        stored, err := target.Job(ctx, written.ID)
        if err != nil {
            t.Fatalf("expected the parked job to still be there, got: %v", err)
        }

        if !stored.IsParked(contractMaxAttempts) {
            t.Fatalf("expected a parked job, got %d attempts", stored.Attempts)
        }
    })

    t.Run("integration: one poll takes no more than the limit", func(t *testing.T) {
        target := newFixture(t).Queue()

        for written := 0; written < 5; written++ {
            mustEnqueue(t, target, queue.KindExpireHold, contractMoment)
        }

        leased, err := target.Claim(ctx, queue.ClaimRequest{
            Now: contractMoment, Lease: contractLease, Limit: 2, MaxAttempts: contractMaxAttempts,
        })
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if len(leased) != 2 {
            t.Fatalf("expected the limit to hold at 2, got %d", len(leased))
        }
    })

    t.Run("integration: the oldest scheduled job is handed out first", func(t *testing.T) {
        target := newFixture(t).Queue()

        later := mustEnqueue(t, target, queue.KindExpireHold, contractMoment.Add(time.Minute))
        sooner := mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        leased, err := target.Claim(ctx, queue.ClaimRequest{
            Now: contractMoment.Add(2 * time.Minute), Lease: contractLease, Limit: 1, MaxAttempts: contractMaxAttempts,
        })
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if len(leased) != 1 || leased[0].ID != sooner.ID {
            t.Fatalf("expected the job scheduled first, got %+v and not %s", leased, later.ID)
        }
    })

    t.Run("integration: depth counts waiting, held, and parked apart", func(t *testing.T) {
        target := newFixture(t).Queue()

        mustEnqueue(t, target, queue.KindExpireHold, contractMoment)
        mustEnqueue(t, target, queue.KindExpireHold, contractMoment)
        mustEnqueue(t, target, queue.KindExpireHold, contractMoment.Add(time.Hour))

        // Both due jobs are claimed and left held, so the counts have something
        // in more than one column.
        if leased := claimAt(t, target, contractMoment); len(leased) != 2 {
            t.Fatalf("expected the poll to take both due jobs, got %d", len(leased))
        }

        counted, err := target.Depth(ctx, queue.DepthRequest{
            Now: contractMoment, MaxAttempts: contractMaxAttempts,
        })
        if err != nil {
            t.Fatalf("expected the depth, got: %v", err)
        }

        // The job scheduled an hour out is in none of the three counts, which
        // is the point of measuring depth at an instant rather than counting
        // rows.
        if counted.Ready != 0 || counted.Claimed != 2 || counted.Parked != 0 {
            t.Fatalf("expected two held and nothing ready, got %+v", counted)
        }
    })

    t.Run("edge: a kind this service does not run is refused", func(t *testing.T) {
        target := newFixture(t).Queue()

        request := enqueueRequestFor(t, "send_newsletter", contractMoment)

        if _, err := target.Enqueue(ctx, request); !errors.Is(err, queue.ErrUnknownKind) {
            t.Fatalf("expected ErrUnknownKind, got: %v", err)
        }
    })

    t.Run("edge: a payload that is not a json object is refused", func(t *testing.T) {
        target := newFixture(t).Queue()

        request := enqueueRequestFor(t, queue.KindExpireHold, contractMoment)
        request.Payload = []byte("expire it please")

        if _, err := target.Enqueue(ctx, request); !errors.Is(err, queue.ErrInvalidPayload) {
            t.Fatalf("expected ErrInvalidPayload, got: %v", err)
        }
    })

    t.Run("edge: the same job id twice is refused", func(t *testing.T) {
        target := newFixture(t).Queue()

        request := enqueueRequestFor(t, queue.KindExpireHold, contractMoment)

        if _, err := target.Enqueue(ctx, request); err != nil {
            t.Fatalf("expected the first write to land, got: %v", err)
        }

        if _, err := target.Enqueue(ctx, request); !errors.Is(err, queue.ErrDuplicateJob) {
            t.Fatalf("expected ErrDuplicateJob, got: %v", err)
        }
    })

    t.Run("edge: completing a job twice says so rather than passing quietly", func(t *testing.T) {
        target := newFixture(t).Queue()
        written := mustEnqueue(t, target, queue.KindExpireHold, contractMoment)

        if err := target.Complete(ctx, written.ID); err != nil {
            t.Fatalf("expected the first completion to land, got: %v", err)
        }

        if err := target.Complete(ctx, written.ID); !errors.Is(err, queue.ErrJobNotFound) {
            t.Fatalf("expected ErrJobNotFound, got: %v", err)
        }
    })

    t.Run("edge: releasing a job that is not there says so", func(t *testing.T) {
        target := newFixture(t).Queue()

        err := target.Release(ctx, queue.ReleaseRequest{JobID: newJobID(t), RunAfter: contractMoment})
        if !errors.Is(err, queue.ErrJobNotFound) {
            t.Fatalf("expected ErrJobNotFound, got: %v", err)
        }
    })

    t.Run("edge: a poll with no lease is refused before any row is locked", func(t *testing.T) {
        target := newFixture(t).Queue()

        _, err := target.Claim(ctx, queue.ClaimRequest{Now: contractMoment, Limit: 1, MaxAttempts: 1})
        if !errors.Is(err, queue.ErrInvalidRequest) {
            t.Fatalf("expected ErrInvalidRequest, got: %v", err)
        }
    })
}
