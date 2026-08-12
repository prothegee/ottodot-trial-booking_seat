package worker_test

import (
    "context"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// A worker that claims ten jobs and then works through them one at a time holds
// ten leases for the length of all ten. Concurrency is what makes the batch and
// the pace two separate decisions.

// overlapWatcher records how many handlers were inside at once.
//
// It is the only assertion that can tell concurrent work from fast serial work,
// because a batch of quick jobs finishes in the same order either way.
type overlapWatcher struct {
    mutex   sync.Mutex
    inside  int
    highest int

    finished atomic.Int64
}

func (watcher *overlapWatcher) enter() {
    watcher.mutex.Lock()
    defer watcher.mutex.Unlock()

    watcher.inside++

    if watcher.inside > watcher.highest {
        watcher.highest = watcher.inside
    }
}

func (watcher *overlapWatcher) leave() {
    watcher.mutex.Lock()
    defer watcher.mutex.Unlock()

    watcher.inside--
    watcher.finished.Add(1)
}

func (watcher *overlapWatcher) highWaterMark() int {
    watcher.mutex.Lock()
    defer watcher.mutex.Unlock()

    return watcher.highest
}

// watchingHandler holds each job open long enough for another to arrive beside
// it, so the overlap is a fact rather than a race the test happened to win.
func watchingHandler(watcher *overlapWatcher) worker.HandlerFunc {
    return func(_ context.Context, _ queue.Job) error {
        watcher.enter()
        defer watcher.leave()

        time.Sleep(20 * time.Millisecond)

        return nil
    }
}

// runBatchWith enqueues a batch, runs it once at the given concurrency, and
// hands back what overlapped.
func runBatchWith(t *testing.T, concurrency int, jobCount int) *overlapWatcher {
    t.Helper()

    watcher := &overlapWatcher{}
    jobs := queue.NewMemoryQueue()

    for index := 0; index < jobCount; index++ {
        enqueue(t, jobs, queue.KindExpireHold)
    }

    settings := runnerSettingsAt(runnerMoment, nil)
    settings.Concurrency = concurrency
    settings.BatchSize = jobCount

    runner, err := worker.NewRunner(jobs, registryRunning(t, watchingHandler(watcher)), settings)
    if err != nil {
        t.Fatalf("cannot build the runner: %v", err)
    }

    claimed, err := runner.RunOnce(context.Background())
    if err != nil {
        t.Fatalf("the poll failed: %v", err)
    }

    if claimed != jobCount {
        t.Fatalf("expected %d jobs claimed, got %d", jobCount, claimed)
    }

    return watcher
}

func TestTheRunnerWorksAtTheConfiguredConcurrency(t *testing.T) {
    t.Run("integration: jobs in one batch overlap when concurrency allows it", func(t *testing.T) {
        watcher := runBatchWith(t, 4, 8)

        if watcher.highWaterMark() < 2 {
            t.Fatalf("nothing ever overlapped, so the batch ran one at a time")
        }

        if watcher.finished.Load() != 8 {
            t.Fatalf("expected every job finished before the poll returned, got %d", watcher.finished.Load())
        }
    })

    t.Run("integration: one means one, in order, exactly as before this setting existed", func(t *testing.T) {
        watcher := runBatchWith(t, 1, 4)

        if watcher.highWaterMark() != 1 {
            t.Fatalf("a concurrency of one ran %d jobs at once", watcher.highWaterMark())
        }
    })

    t.Run("edge: the configured limit is never exceeded", func(t *testing.T) {
        // The limit is what keeps a large batch from opening a connection per
        // job, so exceeding it is worse than ignoring it.
        watcher := runBatchWith(t, 2, 10)

        if watcher.highWaterMark() > 2 {
            t.Fatalf("expected at most two at once, saw %d", watcher.highWaterMark())
        }
    })

    t.Run("edge: the poll waits for every job before it returns", func(t *testing.T) {
        // The claim, the lease, and the count of claimed jobs all describe one
        // batch. Handing back early would leave jobs running against a lease
        // the next poll has already moved past.
        watcher := runBatchWith(t, 4, 8)

        if watcher.highWaterMark() < 1 {
            t.Fatal("no job ran at all")
        }

        if watcher.finished.Load() != 8 {
            t.Fatalf("the poll returned with %d of 8 jobs finished", watcher.finished.Load())
        }
    })

    t.Run("edge: a zero from a caller that never heard of this setting is filled in", func(t *testing.T) {
        settings := runnerSettingsAt(runnerMoment, nil)
        settings.Concurrency = 0

        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindExpireHold)

        runner, err := worker.NewRunner(jobs, registryRunning(t, nil), settings)
        if err != nil {
            t.Fatalf("a zero concurrency was refused instead of filled in: %v", err)
        }

        if _, err := runner.RunOnce(context.Background()); err != nil {
            t.Fatalf("the poll failed: %v", err)
        }
    })
}
