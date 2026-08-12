// What the duration series says after one attempt.
//
// The failure this pins is a metric that is declared and never written. A panel
// bound to one of those is blank forever, which reads as a quiet queue rather
// than as a number nobody publishes, so the timing is asserted here on both ways
// an attempt can end.
package worker_test

import (
    "context"
    "sync"
    "testing"

    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// timedAttempt is one line the runner handed to the sink.
type timedAttempt struct {
    kind    string
    outcome string
    seconds float64
}

// timingSink is a metric sink that keeps the timings and ignores the counts.
//
// It carries a mutex because the runner dispatches concurrently, and a race here
// would be a flake in a test about numbers rather than about concurrency.
type timingSink struct {
    mutex    sync.Mutex
    attempts []timedAttempt
}

func (sink *timingSink) JobsClaimed(_ int) {}

func (sink *timingSink) JobsCompleted() {}

func (sink *timingSink) QueueJobFailed(_ string) {}

func (sink *timingSink) QueueDepth(_ int, _ int, _ int) {}

func (sink *timingSink) QueueDepthUnknown() {}

func (sink *timingSink) QueueJobFinished(kind string, outcome string, seconds float64) {
    sink.mutex.Lock()
    defer sink.mutex.Unlock()

    sink.attempts = append(sink.attempts, timedAttempt{kind: kind, outcome: outcome, seconds: seconds})
}

// only returns the single attempt recorded, and fails when there is not one.
func (sink *timingSink) only(t *testing.T) timedAttempt {
    t.Helper()

    sink.mutex.Lock()
    defer sink.mutex.Unlock()

    if len(sink.attempts) != 1 {
        t.Fatalf("expected one timed attempt, got %d: %v", len(sink.attempts), sink.attempts)
    }

    return sink.attempts[0]
}

// settingsTimedBy is the runner settings with a sink watching the timings.
func settingsTimedBy(sink *timingSink) worker.Settings {
    settings := runnerSettingsAt(runnerMoment, nil)
    settings.Metrics = sink

    return settings
}

func TestEveryJobAttemptIsTimedAndSortedByHowItEnded(t *testing.T) {
    ctx := context.Background()

    t.Run("behaviour: a job that finished is timed as completed", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindExpireHold)

        sink := &timingSink{}

        runner, err := worker.NewRunner(jobs, registryRunning(t, nil), settingsTimedBy(sink))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        if _, err := runner.RunOnce(ctx); err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        attempt := sink.only(t)

        if attempt.kind != string(queue.KindExpireHold) {
            t.Fatalf("expected the attempt named %s, got %q", queue.KindExpireHold, attempt.kind)
        }

        if attempt.outcome != worker.JobCompleted {
            t.Fatalf("expected %q, got %q", worker.JobCompleted, attempt.outcome)
        }

        if attempt.seconds < 0 {
            t.Fatalf("expected a duration, got %v", attempt.seconds)
        }
    })

    t.Run("behaviour: a job whose handler refused is timed as failed", func(t *testing.T) {
        // The failing attempt is the one worth timing. A handler that fails
        // after thirty seconds and one that fails at once are different
        // problems, and a panel that only saw the successes could not tell them
        // apart.
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindReconcileRefund)

        sink := &timingSink{}
        registry := registryRunning(t, func(_ context.Context, _ queue.Job) error {
            return errHandlerRefused
        })

        runner, err := worker.NewRunner(jobs, registry, settingsTimedBy(sink))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        if _, err := runner.RunOnce(ctx); err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        attempt := sink.only(t)

        if attempt.kind != string(queue.KindReconcileRefund) {
            t.Fatalf("expected the attempt named %s, got %q", queue.KindReconcileRefund, attempt.kind)
        }

        if attempt.outcome != worker.JobFailed {
            t.Fatalf("expected %q, got %q", worker.JobFailed, attempt.outcome)
        }
    })

    t.Run("edge: an attempt is timed once, whichever way it ended", func(t *testing.T) {
        // Two lines for one attempt would double every rate on the panel, and
        // none would leave the panel blank on a queue that is working.
        jobs := queue.NewMemoryQueue()

        enqueue(t, jobs, queue.KindExpireHold)
        enqueue(t, jobs, queue.KindReconcileRefund)

        sink := &timingSink{}
        registry := registryRunning(t, func(_ context.Context, job queue.Job) error {
            if job.Kind == queue.KindExpireHold {
                return errHandlerRefused
            }

            return nil
        })

        runner, err := worker.NewRunner(jobs, registry, settingsTimedBy(sink))
        if err != nil {
            t.Fatalf("expected the runner to build, got: %v", err)
        }

        if _, err := runner.RunOnce(ctx); err != nil {
            t.Fatalf("expected the poll to answer, got: %v", err)
        }

        sink.mutex.Lock()
        defer sink.mutex.Unlock()

        if len(sink.attempts) != 2 {
            t.Fatalf("expected two jobs to be timed once each, got %d: %v", len(sink.attempts), sink.attempts)
        }

        outcomes := map[string]string{}
        for _, attempt := range sink.attempts {
            outcomes[attempt.kind] = attempt.outcome
        }

        if outcomes[string(queue.KindExpireHold)] != worker.JobFailed {
            t.Fatalf("expected the refused job timed as failed, got %q", outcomes[string(queue.KindExpireHold)])
        }

        if outcomes[string(queue.KindReconcileRefund)] != worker.JobCompleted {
            t.Fatalf("expected the finished job timed as completed, got %q", outcomes[string(queue.KindReconcileRefund)])
        }
    })

    t.Run("edge: a job with no handler is timed as failed rather than not at all", func(t *testing.T) {
        // It cannot be reached through NewRunner, which refuses a registry
        // missing a kind, so the counters are driven directly. The number still
        // has to exist: a kind nobody can handle is the case where an operator
        // most needs to see attempts happening.
        sink := &timingSink{}
        counters := worker.NewCounters(sink)

        counters.Timed(string(queue.KindExpireHold), worker.JobFailed, runnerMoment)

        if attempt := sink.only(t); attempt.outcome != worker.JobFailed {
            t.Fatalf("expected %q, got %q", worker.JobFailed, attempt.outcome)
        }
    })

    t.Run("edge: counters with nowhere to publish do not panic", func(t *testing.T) {
        var counters *worker.Counters

        counters.Timed(string(queue.KindExpireHold), worker.JobCompleted, runnerMoment)

        worker.NewCounters(nil).Timed(string(queue.KindExpireHold), worker.JobCompleted, runnerMoment)
    })
}
