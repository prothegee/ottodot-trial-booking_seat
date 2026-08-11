package worker_test

import (
    "context"
    "errors"
    "strings"
    "sync"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/identifier"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// runnerMoment is the instant every runner case is judged at.
var runnerMoment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

// errHandlerRefused is what a deliberately failing handler reports, so a case
// can assert the failure reached the report rather than something else did.
var errHandlerRefused = errors.New("the handler refused this job")

// newIdentifier mints an id in the format the queue accepts.
func newIdentifier(t *testing.T) string {
    t.Helper()

    minted, err := identifier.NewUUIDv7()
    if err != nil {
        t.Fatalf("cannot mint an identifier: %v", err)
    }

    return minted
}

// enqueue puts one job on a queue and fails the test if it was refused.
func enqueue(t *testing.T, jobs queue.Queue, kind queue.Kind) queue.Job {
    t.Helper()

    payload, err := queue.EncodeBookingPayload(newIdentifier(t))
    if err != nil {
        t.Fatalf("cannot encode a payload: %v", err)
    }

    written, err := jobs.Enqueue(context.Background(), queue.EnqueueRequest{
        JobID: newIdentifier(t), Kind: kind, Payload: payload, Now: runnerMoment,
    })
    if err != nil {
        t.Fatalf("cannot enqueue a job: %v", err)
    }

    return written
}

// reportCollector gathers what the runner said went wrong, so a case can assert
// on it instead of on a log nobody reads.
type reportCollector struct {
    mutex   sync.Mutex
    reports []error
}

func (collector *reportCollector) Record(err error) {
    collector.mutex.Lock()
    defer collector.mutex.Unlock()

    collector.reports = append(collector.reports, err)
}

func (collector *reportCollector) All() []error {
    collector.mutex.Lock()
    defer collector.mutex.Unlock()

    held := make([]error, len(collector.reports))
    copy(held, collector.reports)

    return held
}

// runnerSettingsAt builds settings with the clock pinned, so a backoff
// assertion is exact rather than approximately right.
func runnerSettingsAt(moment time.Time, collector *reportCollector) worker.Settings {
    settings := worker.DefaultSettings()
    settings.Clock = func() time.Time { return moment }

    if collector != nil {
        settings.OnError = collector.Record
    }

    return settings
}

// registryRunning puts the same handler behind both kinds, which is what a case
// about the runner rather than about a domain wants. Nil means a handler that
// succeeds and does nothing, for the cases that never dispatch a job.
func registryRunning(t *testing.T, handle worker.HandlerFunc) worker.Registry {
    t.Helper()

    if handle == nil {
        handle = func(_ context.Context, _ queue.Job) error { return nil }
    }

    registry := worker.Registry{}

    for _, kind := range queue.AllKinds() {
        if err := registry.Register(kind, handle); err != nil {
            t.Fatalf("cannot register %s: %v", kind, err)
        }
    }

    return registry
}

func TestTheRunnerRefusesSettingsItCannotWorkWith(t *testing.T) {
    t.Run("unit: the defaults build a runner", func(t *testing.T) {
        _, err := worker.NewRunner(queue.NewMemoryQueue(), registryRunning(t, nil), worker.DefaultSettings())
        if err != nil {
            t.Fatalf("the defaults must build a runner, got: %v", err)
        }
    })

    t.Run("unit: the defaults are the documented numbers", func(t *testing.T) {
        settings := worker.DefaultSettings()

        if settings.Lease <= settings.PollInterval {
            t.Fatalf("a lease shorter than a poll would let two workers share a job, got %s and %s", settings.Lease, settings.PollInterval)
        }

        if settings.MaxAttempts < 1 || settings.BatchSize < 1 {
            t.Fatalf("expected usable defaults, got %+v", settings)
        }
    })

    t.Run("edge: a registry missing a kind is refused at construction", func(t *testing.T) {
        // A job with no handler can only fail. Finding that out at startup is
        // far cheaper than finding it out from a queue that stopped draining.
        registry := worker.Registry{}

        if err := registry.Register(queue.KindExpireHold, noopHandler()); err != nil {
            t.Fatalf("cannot register a handler: %v", err)
        }

        _, err := worker.NewRunner(queue.NewMemoryQueue(), registry, worker.DefaultSettings())
        if !errors.Is(err, worker.ErrHandlerMissing) {
            t.Fatalf("expected ErrHandlerMissing, got: %v", err)
        }
    })

    t.Run("edge: a missing queue is refused at construction", func(t *testing.T) {
        _, err := worker.NewRunner(nil, registryRunning(t, nil), worker.DefaultSettings())
        if !errors.Is(err, worker.ErrInvalidSettings) {
            t.Fatalf("expected ErrInvalidSettings, got: %v", err)
        }
    })

    t.Run("edge: a lease of zero is refused at construction", func(t *testing.T) {
        // A zero lease means every job is claimable by everybody the instant it
        // is handed out.
        settings := worker.DefaultSettings()
        settings.Lease = 0

        _, err := worker.NewRunner(queue.NewMemoryQueue(), registryRunning(t, nil), settings)
        if !errors.Is(err, worker.ErrInvalidSettings) {
            t.Fatalf("expected ErrInvalidSettings, got: %v", err)
        }
    })

    t.Run("edge: an attempt cap below one is refused at construction", func(t *testing.T) {
        settings := worker.DefaultSettings()
        settings.MaxAttempts = 0

        _, err := worker.NewRunner(queue.NewMemoryQueue(), registryRunning(t, nil), settings)
        if !errors.Is(err, worker.ErrInvalidSettings) {
            t.Fatalf("expected ErrInvalidSettings, got: %v", err)
        }
    })
}

func TestAJobThatSucceedsIsRemoved(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: the handler runs and the row is gone", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        written := enqueue(t, jobs, queue.KindExpireHold)

        var handled []string

        registry := registryRunning(t, func(_ context.Context, job queue.Job) error {
            handled = append(handled, job.ID)

            return nil
        })

        runner, err := worker.NewRunner(jobs, registry, runnerSettingsAt(runnerMoment, nil))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        claimed, err := runner.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if claimed != 1 || len(handled) != 1 || handled[0] != written.ID {
            t.Fatalf("expected the one job handled, got %d claimed and %v handled", claimed, handled)
        }

        if _, err := jobs.Job(ctx, written.ID); !errors.Is(err, queue.ErrJobNotFound) {
            t.Fatalf("expected the finished job to be gone, got: %v", err)
        }

        counted := runner.Counters().Snapshot()

        if counted.Claimed != 1 || counted.Completed != 1 || counted.Failed != 0 {
            t.Fatalf("expected one claimed and one completed, got %+v", counted)
        }
    })

    t.Run("integration: a poll that finds nothing does nothing", func(t *testing.T) {
        // The ordinary case on a quiet queue, and it must be silent. A report
        // per empty poll would bury the reports that matter.
        collector := &reportCollector{}

        runner, err := worker.NewRunner(queue.NewMemoryQueue(), registryRunning(t, nil), runnerSettingsAt(runnerMoment, collector))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        claimed, err := runner.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if claimed != 0 || len(collector.All()) != 0 {
            t.Fatalf("expected a silent empty poll, got %d claimed and %d reports", claimed, len(collector.All()))
        }
    })
}

func TestAJobThatFailsIsHandedBack(t *testing.T) {
    ctx := context.Background()

    t.Run("integration: the row stays and the backoff is applied", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        written := enqueue(t, jobs, queue.KindExpireHold)

        collector := &reportCollector{}
        settings := runnerSettingsAt(runnerMoment, collector)

        registry := registryRunning(t, func(_ context.Context, _ queue.Job) error {
            return errHandlerRefused
        })

        runner, err := worker.NewRunner(jobs, registry, settings)
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        if _, err := runner.RunOnce(ctx); err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        held, err := jobs.Job(ctx, written.ID)
        if err != nil {
            t.Fatalf("expected the job to still be there, got: %v", err)
        }

        if !held.RunAfter.Equal(runnerMoment.Add(settings.RetryBackoff)) {
            t.Fatalf("expected the backoff applied, got %v", held.RunAfter)
        }

        if held.IsClaimed(runnerMoment) {
            t.Fatal("a released job is held by nobody")
        }

        if counted := runner.Counters().Snapshot(); counted.Failed != 1 || counted.Completed != 0 {
            t.Fatalf("expected one failure and nothing completed, got %+v", counted)
        }

        reports := collector.All()

        if len(reports) != 1 || !errors.Is(reports[0], errHandlerRefused) {
            t.Fatalf("expected the handler's own failure reported, got %v", reports)
        }
    })

    t.Run("integration: a job with no handler is handed back rather than lost", func(t *testing.T) {
        // The constructor refuses an incomplete registry, so this path is only
        // reached when a handler goes away after the runner was built. It is
        // still worth covering: the job must survive, because losing work
        // silently is worse than a queue that visibly stops draining.
        jobs := queue.NewMemoryQueue()
        written := enqueue(t, jobs, queue.KindReconcileRefund)

        collector := &reportCollector{}
        registry := registryRunning(t, func(_ context.Context, _ queue.Job) error { return nil })

        runner, err := worker.NewRunner(jobs, registry, runnerSettingsAt(runnerMoment, collector))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        delete(registry, queue.KindReconcileRefund)

        if _, err := runner.RunOnce(ctx); err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if _, err := jobs.Job(ctx, written.ID); err != nil {
            t.Fatalf("expected the job to survive, got: %v", err)
        }

        reports := collector.All()

        if len(reports) != 1 || !errors.Is(reports[0], worker.ErrHandlerMissing) {
            t.Fatalf("expected ErrHandlerMissing reported, got %v", reports)
        }
    })

    t.Run("integration: a job that keeps failing parks and says so", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindExpireHold)

        collector := &reportCollector{}

        settings := worker.DefaultSettings()
        settings.MaxAttempts = 2
        settings.RetryBackoff = time.Second
        settings.OnError = collector.Record

        attempt := 0

        settings.Clock = func() time.Time {
            // Each poll is one backoff further on, so the released job is due
            // again on the next attempt.
            return runnerMoment.Add(time.Duration(attempt) * settings.RetryBackoff)
        }

        registry := registryRunning(t, func(_ context.Context, _ queue.Job) error {
            return errHandlerRefused
        })

        runner, err := worker.NewRunner(jobs, registry, settings)
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        for attempt = 0; attempt < settings.MaxAttempts; attempt++ {
            claimed, runErr := runner.RunOnce(ctx)
            if runErr != nil {
                t.Fatalf("expected the poll to answer, got: %v", runErr)
            }

            if claimed != 1 {
                t.Fatalf("expected attempt %d to be handed out, got %d", attempt+1, claimed)
            }
        }

        claimed, err := runner.RunOnce(ctx)
        if err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        if claimed != 0 {
            t.Fatalf("expected the job to stop being handed out, got %d", claimed)
        }

        parked := false

        for _, report := range collector.All() {
            if report != nil && strings.Contains(report.Error(), "parked") {
                parked = true
            }
        }

        if !parked {
            t.Fatalf("expected a report naming the parked job, got %v", collector.All())
        }
    })
}

func TestTheLoopStopsWhenItIsToldTo(t *testing.T) {
    t.Run("integration: a cancelled context ends the loop without an error", func(t *testing.T) {
        // A shutdown is not a failure, so the caller must not have to unwrap
        // one to find that out.
        settings := worker.DefaultSettings()
        settings.PollInterval = time.Millisecond
        settings.Clock = func() time.Time { return runnerMoment }

        runner, err := worker.NewRunner(queue.NewMemoryQueue(), registryRunning(t, nil), settings)
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        ctx, cancel := context.WithCancel(context.Background())

        stopped := make(chan error, 1)

        go func() { stopped <- runner.Run(ctx) }()

        cancel()

        select {
        case err := <-stopped:
            if err != nil {
                t.Fatalf("expected a clean stop, got: %v", err)
            }
        case <-time.After(2 * time.Second):
            t.Fatal("the loop did not stop when the context was cancelled")
        }
    })
}
