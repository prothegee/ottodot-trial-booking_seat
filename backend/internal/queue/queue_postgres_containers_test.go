//go:build containers

// The real database half of the queue contract, plus the proof no fake can
// give.
//
// It needs the backend stack running, which is why it sits behind the
// containers build tag and never runs during the fast go test ./... pass.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/queue/...
//
// Every case works inside its own throwaway schema, so it can run against the
// same database a reviewer is clicking through without touching a seeded row.
package queue_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ottodot-trial-booking/backend/internal/queue"
)

// scratchPoolSize has to cover the widest race in this package, which is the
// parallel claim proof. A pool smaller than the fan-out would queue the
// goroutines and quietly weaken the proof, because two claims that never
// overlap cannot demonstrate anything about SKIP LOCKED.
const scratchPoolSize = 12

// primaryAddress is where the proof tier connects. A claim decides whether a
// job is already running, so it goes to the primary and never the replica.
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

	scratchName := fmt.Sprintf("queue_proof_%d", time.Now().UnixNano())

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
	// scratch schema, which is what keeps the tables isolated.
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

// postgresFixture points the shared contract at the real queue. There is
// nothing to seed: job_queue hangs from no foreign key, which is deliberate,
// because a job outliving the row it refers to has to be the handler's problem
// rather than a write failure at three in the morning.
type postgresFixture struct {
	queue *queue.PostgresQueue
}

func newPostgresFixture(t *testing.T) queueFixture {
	return &postgresFixture{queue: queue.NewPostgresQueue(newScratchPool(t))}
}

func (fixture *postgresFixture) Queue() queue.Queue {
	return fixture.queue
}

func TestThePostgresQueueHonoursTheContract(t *testing.T) {
	// The same suite the memory queue runs. This is the run that catches a fake
	// which is right for reasons the sql is not.
	runQueueContract(t, newPostgresFixture)
}

func TestTwoWorkersNeverClaimTheSameJob(t *testing.T) {
	t.Run("proof: parallel workers on one queue split the work and never share a job", func(t *testing.T) {
		// This is the case the fast tiers cannot prove. A mutex makes the fake
		// pass it by serializing the two polls, which is not what happens in
		// production: two worker processes, two connections, two transactions,
		// arriving at the same instant.
		//
		// What makes it correct is FOR UPDATE SKIP LOCKED. The second poll does
		// not wait for the first one's rows, it steps over them and takes
		// others. Removing SKIP LOCKED makes this test hang rather than fail,
		// which is itself the signal.
		pool := newScratchPool(t)
		target := queue.NewPostgresQueue(pool)

		const (
			jobs    = 24
			workers = 8
		)

		ctx := context.Background()
		now := time.Now().UTC()

		for written := 0; written < jobs; written++ {
			payload, err := queue.EncodeBookingPayload(newJobID(t))
			if err != nil {
				t.Fatalf("cannot encode a payload: %v", err)
			}

			if _, err := target.Enqueue(ctx, queue.EnqueueRequest{
				JobID: newJobID(t), Kind: queue.KindExpireHold, Payload: payload, Now: now,
			}); err != nil {
				t.Fatalf("cannot enqueue job %d: %v", written, err)
			}
		}

		var (
			ready     sync.WaitGroup
			start     = make(chan struct{})
			finished  sync.WaitGroup
			mutex     sync.Mutex
			handedOut []string
		)

		ready.Add(workers)
		finished.Add(workers)

		for worker := 0; worker < workers; worker++ {
			go func() {
				defer finished.Done()

				ready.Done()
				<-start

				leased, err := target.Claim(ctx, queue.ClaimRequest{
					Now: now, Lease: time.Minute, Limit: jobs, MaxAttempts: 3,
				})

				mutex.Lock()
				defer mutex.Unlock()

				if err != nil {
					t.Errorf("unexpected failure from a parallel worker: %v", err)

					return
				}

				for _, job := range leased {
					handedOut = append(handedOut, job.ID)
				}
			}()
		}

		// Every worker is waiting on the same channel, so the polls overlap
		// rather than following one another.
		ready.Wait()
		close(start)
		finished.Wait()

		if len(handedOut) != jobs {
			t.Fatalf("expected every job handed out exactly once, got %d for %d jobs", len(handedOut), jobs)
		}

		seen := make(map[string]struct{}, len(handedOut))

		for _, jobID := range handedOut {
			if _, twice := seen[jobID]; twice {
				t.Fatalf("job %s was claimed by two workers at once", jobID)
			}

			seen[jobID] = struct{}{}
		}

		counted, err := target.Depth(ctx, queue.DepthRequest{Now: now, MaxAttempts: 3})
		if err != nil {
			t.Fatalf("expected the depth, got: %v", err)
		}

		if counted.Ready != 0 || counted.Claimed != jobs {
			t.Fatalf("expected every job held and none left ready, got %+v", counted)
		}
	})

	t.Run("proof: a job each worker fails on is parked rather than looped forever", func(t *testing.T) {
		// The other half of the same story. A job that always fails must stop,
		// or two workers spend the night handing it back and forth.
		pool := newScratchPool(t)
		target := queue.NewPostgresQueue(pool)

		const maxAttempts = 3

		ctx := context.Background()
		now := time.Now().UTC()

		payload, err := queue.EncodeBookingPayload(newJobID(t))
		if err != nil {
			t.Fatalf("cannot encode a payload: %v", err)
		}

		written, err := target.Enqueue(ctx, queue.EnqueueRequest{
			JobID: newJobID(t), Kind: queue.KindReconcileRefund, Payload: payload, Now: now,
		})
		if err != nil {
			t.Fatalf("cannot enqueue the job: %v", err)
		}

		for spent := 0; spent < maxAttempts; spent++ {
			leased, claimErr := target.Claim(ctx, queue.ClaimRequest{
				Now: now, Lease: time.Minute, Limit: 1, MaxAttempts: maxAttempts,
			})
			if claimErr != nil {
				t.Fatalf("cannot claim on attempt %d: %v", spent+1, claimErr)
			}

			if len(leased) != 1 {
				t.Fatalf("expected attempt %d to be handed out, got %d jobs", spent+1, len(leased))
			}

			if releaseErr := target.Release(ctx, queue.ReleaseRequest{
				JobID: written.ID, RunAfter: now,
			}); releaseErr != nil {
				t.Fatalf("cannot release on attempt %d: %v", spent+1, releaseErr)
			}
		}

		leased, err := target.Claim(ctx, queue.ClaimRequest{
			Now: now, Lease: time.Minute, Limit: 1, MaxAttempts: maxAttempts,
		})
		if err != nil {
			t.Fatalf("cannot poll after the attempts were spent: %v", err)
		}

		if len(leased) != 0 {
			t.Fatalf("expected the job to stop being handed out, got %d", len(leased))
		}

		counted, err := target.Depth(ctx, queue.DepthRequest{Now: now, MaxAttempts: maxAttempts})
		if err != nil {
			t.Fatalf("expected the depth, got: %v", err)
		}

		if counted.Parked != 1 {
			t.Fatalf("expected one parked job an operator can still see, got %+v", counted)
		}
	})
}
