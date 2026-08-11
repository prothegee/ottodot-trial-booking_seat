package worker_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// alwaysFires is a fault source that fires every time for the named point,
// which is what a case about the retry path wants: the same job has to fail on
// each attempt until it parks.
func alwaysFires(point string) worker.Fault {
    return func(reached string) bool { return reached == point }
}

func TestJobsUnderInjectedFaults(t *testing.T) {
    t.Run("integration: an armed job fails and is handed back for another try", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindExpireHold)

        collector := &reportCollector{}
        settings := runnerSettingsAt(runnerMoment, collector)
        settings.Fault = alwaysFires(faults.PointQueueJobError)

        ran := 0

        runner, err := worker.NewRunner(jobs, registryRunning(t, func(_ context.Context, _ queue.Job) error {
            ran++

            return nil
        }), settings)
        if err != nil {
            t.Fatalf("cannot build the runner: %v", err)
        }

        if _, err := runner.RunOnce(context.Background()); err != nil {
            t.Fatalf("the poll answered %v", err)
        }

        if ran != 0 {
            t.Fatal("the handler ran, so the fault was checked in the wrong place")
        }

        if runner.Counters().Snapshot().Failed != 1 {
            t.Fatalf("the runner counted %d failures", runner.Counters().Snapshot().Failed)
        }
    })

    t.Run("edge: the report says the failure was arranged", func(t *testing.T) {
        // Anybody reading a log during a demonstration should be able to tell at
        // a glance which failures were on purpose and which were not.
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindReconcileRefund)

        collector := &reportCollector{}
        settings := runnerSettingsAt(runnerMoment, collector)
        settings.Fault = alwaysFires(faults.PointQueueJobError)

        runner, err := worker.NewRunner(jobs, registryRunning(t, nil), settings)
        if err != nil {
            t.Fatalf("cannot build the runner: %v", err)
        }

        if _, err := runner.RunOnce(context.Background()); err != nil {
            t.Fatalf("the poll answered %v", err)
        }

        named := false

        for _, reported := range collector.All() {
            if errors.Is(reported, worker.ErrFaultInjected) {
                named = true
            }
        }

        if !named {
            t.Fatalf("no report named the injected fault, got %v", collector.All())
        }
    })

    t.Run("unit: a runner with no fault source runs its handlers as usual", func(t *testing.T) {
        jobs := queue.NewMemoryQueue()
        enqueue(t, jobs, queue.KindExpireHold)

        ran := 0

        runner, err := worker.NewRunner(jobs, registryRunning(t, func(_ context.Context, _ queue.Job) error {
            ran++

            return nil
        }), runnerSettingsAt(runnerMoment, nil))
        if err != nil {
            t.Fatalf("cannot build the runner: %v", err)
        }

        if _, err := runner.RunOnce(context.Background()); err != nil {
            t.Fatalf("the poll answered %v", err)
        }

        if ran != 1 {
            t.Fatalf("the handler ran %d times without any fault armed", ran)
        }
    })
}
