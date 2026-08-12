package worker

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/queue"
)

// Settings are the policy the runner applies. They are values rather than
// constants because a busy queue and a quiet one want different numbers, and
// neither should need a rebuild.
type Settings struct {
    // PollInterval is how long the runner waits after a poll that found
    // nothing. A poll that filled its batch does not wait at all.
    PollInterval time.Duration

    // Lease is how long a claim holds. It has to be comfortably longer than the
    // slowest handler: a lease that lapses mid-job lets a second worker start
    // the same work while the first is still doing it.
    Lease time.Duration

    // BatchSize caps how many jobs one poll takes. It exists so one worker
    // cannot lease the whole queue and then die holding it.
    BatchSize int

    // Concurrency is how many of a claimed batch are worked on at once. One
    // means the batch is worked through in order, which is what every test
    // wants.
    //
    // It is separate from BatchSize because the two answer different questions:
    // the batch is how much this process takes responsibility for, and this is
    // how fast it gets through it. Claiming ten and running them one at a time
    // still holds all ten leases for the whole run.
    Concurrency int

    // MaxAttempts is where a failing job stops. Past it the job parks, stays in
    // the table, and waits for somebody to look at it.
    MaxAttempts int

    // RetryBackoff is how long a failed job waits before it is claimable again.
    RetryBackoff time.Duration

    // Clock is where every instant comes from. Nil means the real clock. A test
    // sets it so a lease boundary is exact rather than approximately right.
    Clock func() time.Time

    // OnError is told about anything that went wrong, one call per failure. Nil
    // means silence, which is what a test wants and what production must never
    // have. The worker hands in its logger here.
    OnError func(err error)

    // Fault is where a deliberately injected job failure comes from. Nil is the
    // ordinary state and means no job is ever broken on purpose.
    Fault Fault

    // Metrics is where this runner's counts are published. Nil means nowhere,
    // which is what every test runs with.
    Metrics MetricSink
}

// DefaultSettings are the values the worker runs with when nothing overrides
// them.
//
// Five seconds between empty polls is slow enough to leave the database alone
// and quick enough that a hold released now is bookable before a parent gives
// up. A two minute lease is far longer than any handler here takes, which is
// the safe direction to be wrong in. Five attempts with a thirty second backoff
// spans two and a half minutes of trouble before a job parks, which outlasts an
// ordinary restart and not much else.
func DefaultSettings() Settings {
    return Settings{
        PollInterval: 5 * time.Second,
        Lease:        2 * time.Minute,
        BatchSize:    10,
        MaxAttempts:  5,
        RetryBackoff: 30 * time.Second,
        Concurrency:  1,
    }
}

// Runner claims jobs and hands each to the handler for its kind.
//
// It owns no domain rule. Everything it knows is in this file: what to claim,
// what to do when a handler says it is finished, and what to do when one says
// it is not.
type Runner struct {
    queue    queue.Queue
    handlers Registry
    settings Settings
    counters *Counters
}

// NewRunner builds the runner and fills in whatever the settings left out.
//
// Note:
//   - a registry that does not cover every kind is refused. A job with no
//     handler can only fail, and finding that out at startup is far cheaper
//     than finding it out from a queue that stopped draining.
//
// Param:
// jobs - queue.Queue (the real one in production, the fake in the fast tiers)
// handlers - Registry (one handler per kind)
// settings - Settings (policy, and the two injection points a test needs)
//
// Return:
//   - the runner, ready to poll
//   - ErrInvalidSettings when a policy value cannot work
//   - ErrHandlerMissing when a kind has nothing to run it
func NewRunner(jobs queue.Queue, handlers Registry, settings Settings) (*Runner, error) {
    if jobs == nil {
        return nil, fmt.Errorf("%w: the runner needs a queue", ErrInvalidSettings)
    }

    if !handlers.Covers() {
        return nil, ErrHandlerMissing
    }

    if settings.PollInterval <= 0 || settings.Lease <= 0 || settings.RetryBackoff <= 0 {
        return nil, fmt.Errorf("%w: every interval must be greater than zero", ErrInvalidSettings)
    }

    if settings.BatchSize < 1 || settings.MaxAttempts < 1 {
        return nil, fmt.Errorf("%w: the batch and the attempt cap must be at least one", ErrInvalidSettings)
    }

    if settings.Clock == nil {
        settings.Clock = time.Now
    }

    // Zero is filled in rather than refused. It is what a caller that never
    // heard of this setting hands in, and the answer to that is the behaviour
    // this runner had before the setting existed.
    if settings.Concurrency < 1 {
        settings.Concurrency = 1
    }

    return &Runner{
        queue:    jobs,
        handlers: handlers,
        settings: settings,
        counters: NewCounters(settings.Metrics),
    }, nil
}

// Counters is what this runner has done so far.
func (runner *Runner) Counters() *Counters {
    return runner.counters
}

// RunOnce claims one batch and runs it to the end.
//
// It is exported because it is what every test drives. A loop is hard to assert
// against, and one poll is not.
//
// Return:
//   - how many jobs were claimed, which is what tells a loop whether to wait
//   - an error only when the queue itself could not be polled. A handler that
//     failed is not an error here, it is a job that will be tried again, and it
//     reaches OnError instead
func (runner *Runner) RunOnce(ctx context.Context) (int, error) {
    leased, err := runner.queue.Claim(ctx, queue.ClaimRequest{
        Now:         runner.settings.Clock(),
        Lease:       runner.settings.Lease,
        Limit:       runner.settings.BatchSize,
        MaxAttempts: runner.settings.MaxAttempts,
    })
    if err != nil {
        return 0, fmt.Errorf("the queue could not be polled: %w", err)
    }

    runner.counters.Claimed(len(leased))
    runner.dispatchBatch(ctx, leased)

    return len(leased), nil
}

// dispatchBatch works through one claimed batch, Concurrency jobs at a time.
//
// It returns only when every job in the batch is finished, which is what keeps
// the lease, the poll loop, and the count of claimed jobs all describing the
// same batch. Handing back early would leave jobs running against a lease the
// next poll has already moved past.
func (runner *Runner) dispatchBatch(ctx context.Context, leased []queue.Job) {
    if runner.settings.Concurrency == 1 || len(leased) < 2 {
        for _, job := range leased {
            runner.dispatch(ctx, job)
        }

        return
    }

    slots := make(chan struct{}, runner.settings.Concurrency)

    var running sync.WaitGroup

    for _, job := range leased {
        running.Add(1)
        slots <- struct{}{}

        go func() {
            defer running.Done()
            defer func() { <-slots }()

            runner.dispatch(ctx, job)
        }()
    }

    running.Wait()
}

// Run polls until the context is cancelled.
//
// A poll that filled its batch is followed immediately by another, because a
// full batch means there is probably more waiting. A poll that found less than
// a batch waits, because the queue is empty and hammering it proves nothing.
//
// Return:
//   - nil on cancellation, which is the ordinary way this ends. A shutdown is
//     not a failure, so the caller does not have to unwrap one to find out
func (runner *Runner) Run(ctx context.Context) error {
    for {
        if ctx.Err() != nil {
            return nil
        }

        claimed, err := runner.RunOnce(ctx)
        if err != nil {
            runner.report(err)
        }

        if err == nil && claimed >= runner.settings.BatchSize {
            continue
        }

        select {
        case <-ctx.Done():
            return nil
        case <-time.After(runner.settings.PollInterval):
        }
    }
}

// dispatch runs one job and decides what becomes of it.
//
// Every path ends with the job either removed or handed back. A job that is
// neither stays leased until the lease lapses, which looks to an operator like
// a worker that is stuck, so there is no early return here without one or the
// other.
func (runner *Runner) dispatch(ctx context.Context, job queue.Job) {
    started := time.Now()
    outcome := JobFailed

    // Registered before anything can fail below, and it reads the outcome at the
    // moment it runs, so every path out of this method is timed exactly once.
    // Failed is the value it starts at, because the one path that must never go
    // uncounted is the one that ends badly.
    defer func() { runner.counters.Timed(string(job.Kind), outcome, started) }()

    handler, found := runner.handlers[job.Kind]
    if !found {
        runner.handBack(ctx, job, fmt.Errorf("job %s: %w", job.ID, ErrHandlerMissing))

        return
    }

    // Injection point: a job blowing up. It is checked before the handler rather
    // than inside one, so the same fault reaches every kind and the retry and
    // then the parking can be watched happening without a real failure being
    // arranged.
    if runner.settings.Fault.triggered(faults.PointQueueJobError) {
        runner.handBack(ctx, job, fmt.Errorf("job %s (%s) failed on attempt %d: %w", job.ID, job.Kind, job.Attempts, ErrFaultInjected))

        return
    }

    if err := handler.Handle(ctx, job); err != nil {
        runner.handBack(ctx, job, fmt.Errorf("job %s (%s) failed on attempt %d: %w", job.ID, job.Kind, job.Attempts, err))

        return
    }

    if err := runner.queue.Complete(ctx, job.ID); err != nil {
        // The work is done and the row is not gone. The lease will lapse and
        // another worker will run the same job, which is exactly why every
        // handler here is written to be safe when there is nothing to do.
        runner.report(fmt.Errorf("job %s ran but could not be completed: %w", job.ID, err))

        return
    }

    outcome = JobCompleted

    runner.counters.Completed()
}

// handBack releases a job for another try and reports why.
func (runner *Runner) handBack(ctx context.Context, job queue.Job, cause error) {
    runner.counters.Failed(string(job.Kind))
    runner.report(cause)

    if job.IsParked(runner.settings.MaxAttempts) {
        // It has spent its attempts, so this release is the last one. Saying so
        // is what turns a line in a log into something an operator acts on.
        runner.report(fmt.Errorf("job %s (%s) has spent its %d attempts and is parked", job.ID, job.Kind, runner.settings.MaxAttempts))
    }

    release := queue.ReleaseRequest{
        JobID:    job.ID,
        RunAfter: runner.settings.Clock().Add(runner.settings.RetryBackoff),
    }

    if err := runner.queue.Release(ctx, release); err != nil && !errors.Is(err, queue.ErrJobNotFound) {
        runner.report(fmt.Errorf("job %s could not be released: %w", job.ID, err))
    }
}

// report hands one failure to whoever is listening, and drops it when nobody
// is. Silence is a deliberate choice a test makes, never a default production
// falls into, because NewRunner is given the logger by the process that starts
// it.
func (runner *Runner) report(err error) {
    if runner.settings.OnError == nil {
        return
    }

    runner.settings.OnError(err)
}
