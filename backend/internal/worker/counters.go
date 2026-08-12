package worker

import "sync/atomic"

// MetricSink is where this worker's counts are published.
//
// It is an interface declared here rather than the metrics type itself, so this
// package can be tested without a Prometheus registry existing.
//
// Nil is the ordinary state in a test and never the state in a running worker.
type MetricSink interface {
    JobsClaimed(jobs int)
    JobsCompleted()
    QueueJobFailed(kind string)
    QueueJobFinished(kind string, outcome string, seconds float64)
    QueueDepth(ready int, claimed int, parked int)
    QueueDepthUnknown()
}

// Counters is what the worker knows about its own run.
//
// They are counts rather than gauges on purpose: a count only ever goes up, so
// a scrape that lands between two polls cannot read a number that was briefly
// wrong. The one number that is a gauge, queue depth, is read from the queue at
// scrape time rather than tracked here, because the queue is the only thing
// that knows it.
type Counters struct {
    sink MetricSink

    claimed   atomic.Int64
    completed atomic.Int64
    failed    atomic.Int64
}

// NewCounters builds a fresh set, all at zero.
//
// Param:
// sink - MetricSink (where the same counts are published, nil for nowhere)
//
// Return:
//   - the counters
func NewCounters(sink MetricSink) *Counters {
    return &Counters{sink: sink}
}

// Claimed records that jobs were handed to this worker.
func (counters *Counters) Claimed(jobs int) {
    if jobs <= 0 {
        return
    }

    counters.claimed.Add(int64(jobs))

    if counters.sink != nil {
        counters.sink.JobsClaimed(jobs)
    }
}

// Completed records one job that finished and was removed.
func (counters *Counters) Completed() {
    counters.completed.Add(1)

    if counters.sink != nil {
        counters.sink.JobsCompleted()
    }
}

// Failed records one job that was handed back for another try.
//
// The kind is carried into the metric and not into the local count. An operator
// wants to know which kind of job is failing, and a test wants to know whether
// anything failed at all.
func (counters *Counters) Failed(kind string) {
    counters.failed.Add(1)

    if counters.sink != nil {
        counters.sink.QueueJobFailed(kind)
    }
}

// Depth publishes what the queue holds right now.
//
// It is read from the queue at scrape time rather than tracked here, because the
// queue is the only thing that knows it.
func (counters *Counters) Depth(ready int, claimed int, parked int) {
    if counters.sink != nil {
        counters.sink.QueueDepth(ready, claimed, parked)
    }
}

// DepthUnknown publishes that the queue could not be asked.
//
// It is the other half of Depth rather than a silence, because a gauge nobody
// writes keeps its last value and goes on being read as current.
func (counters *Counters) DepthUnknown() {
    if counters.sink != nil {
        counters.sink.QueueDepthUnknown()
    }
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
