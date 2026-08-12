package worker

import "time"

// The outcomes one attempt at a job can end in.
//
// They are this package's own words rather than the ones a database transaction
// uses. A job is not committed or rolled back: it either finished, and the row
// is gone, or it is coming back for another try.
const (
    JobCompleted = "completed"
    JobFailed    = "failed"
)

// Timed records how long one attempt took and how it ended.
//
// Note:
//   - it is timed around the whole attempt rather than around the handler, so a
//     job that spent its time being completed in the queue rather than doing its
//     work still shows up as slow. That is the number an operator wants: how
//     long a job holds a worker for
//   - a failure is timed too. A handler that fails after thirty seconds and one
//     that fails immediately are different problems, and a panel that only ever
//     saw the successes could not tell them apart
//
// Param:
// kind - string (the job kind, one of the queue's closed list)
// outcome - string (JobCompleted or JobFailed)
// started - time.Time (when this attempt was picked up)
func (counters *Counters) Timed(kind string, outcome string, started time.Time) {
    if counters == nil || counters.sink == nil {
        return
    }

    counters.sink.QueueJobFinished(kind, outcome, time.Since(started).Seconds())
}
