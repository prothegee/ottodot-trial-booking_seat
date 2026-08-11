package worker

import "sync/atomic"

// Counters is what the worker knows about its own run.
//
// They are counts rather than gauges on purpose: a count only ever goes up, so
// a scrape that lands between two polls cannot read a number that was briefly
// wrong. The one number that is a gauge, queue depth, is read from the queue at
// scrape time rather than tracked here, because the queue is the only thing
// that knows it.
type Counters struct {
    claimed   atomic.Int64
    completed atomic.Int64
    failed    atomic.Int64
}

// NewCounters builds a fresh set, all at zero.
func NewCounters() *Counters {
    return &Counters{}
}

// Claimed records that jobs were handed to this worker.
func (counters *Counters) Claimed(jobs int) {
    if jobs > 0 {
        counters.claimed.Add(int64(jobs))
    }
}

// Completed records one job that finished and was removed.
func (counters *Counters) Completed() {
    counters.completed.Add(1)
}

// Failed records one job that was handed back for another try.
func (counters *Counters) Failed() {
    counters.failed.Add(1)
}

// Snapshot is the three counts read together.
//
// They are read one after another rather than under a lock, so a scrape landing
// mid-job can see a job counted as claimed before it is counted as completed.
// That is the honest reading rather than a flaw: the job really is in flight at
// that instant.
type Snapshot struct {
    Claimed   int64
    Completed int64
    Failed    int64
}

// Snapshot reads all three counts.
func (counters *Counters) Snapshot() Snapshot {
    return Snapshot{
        Claimed:   counters.claimed.Load(),
        Completed: counters.completed.Load(),
        Failed:    counters.failed.Load(),
    }
}
