package worker

import (
	"fmt"
	"io"

	"ottodot-trial-booking/backend/internal/queue"
)

// The metric names this worker publishes.
//
// They are constants rather than literals inside the writer for one reason: a
// dashboard panel and an alert rule both name a metric by string, and a rename
// that nobody notices turns a panel blank rather than red. Phase 7 asserts
// these names against the dashboard json, and it can only do that if they have
// somewhere to be read from.
const (
	metricQueueDepth      = "queue_depth"
	metricJobsClaimed     = "worker_jobs_claimed_total"
	metricJobsCompleted   = "worker_jobs_completed_total"
	metricJobsFailedTotal = "queue_job_failed_total"
)

// WriteExposition writes the worker's numbers in the Prometheus text format.
//
// It is written by hand rather than through a client library, and that is a
// deliberate limit rather than a preference: this worker publishes four
// numbers, and four numbers do not justify a dependency. Phase 7 replaces this
// with the shared registry in `internal/observability`, which is where process
// and runtime collectors come from, and these four names carry over unchanged.
//
// Param:
// writer - io.Writer (where the exposition goes, one response body in practice)
// counted - Snapshot (what this worker has done since it started)
// depth - queue.Depth (what the queue holds right now)
//
// Return:
//   - nil once every line is written
//   - the writer's failure, unchanged, so a broken connection is not reported
//     as a metrics problem
func WriteExposition(writer io.Writer, counted Snapshot, depth queue.Depth) error {
	lines := []struct {
		name   string
		metric string
		help   string
		kind   string
		value  int64
	}{
		{
			name:   metricQueueDepth,
			metric: metricQueueDepth + `{state="ready"}`,
			help:   "Jobs waiting to be claimed, by state.",
			kind:   "gauge",
			value:  int64(depth.Ready),
		},
		{
			metric: metricQueueDepth + `{state="claimed"}`,
			value:  int64(depth.Claimed),
		},
		{
			metric: metricQueueDepth + `{state="parked"}`,
			value:  int64(depth.Parked),
		},
		{
			name:   metricJobsClaimed,
			metric: metricJobsClaimed,
			help:   "Jobs this worker has taken from the queue.",
			kind:   "counter",
			value:  counted.Claimed,
		},
		{
			name:   metricJobsCompleted,
			metric: metricJobsCompleted,
			help:   "Jobs this worker finished and removed.",
			kind:   "counter",
			value:  counted.Completed,
		},
		{
			name:   metricJobsFailedTotal,
			metric: metricJobsFailedTotal,
			help:   "Jobs this worker handed back for another attempt.",
			kind:   "counter",
			value:  counted.Failed,
		},
	}

	for _, line := range lines {
		// Only the first series of a metric carries the HELP and TYPE lines,
		// which is what the format requires when one name has several label
		// sets.
		if line.name != "" {
			if _, err := fmt.Fprintf(writer, "# HELP %s %s\n# TYPE %s %s\n", line.name, line.help, line.name, line.kind); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(writer, "%s %d\n", line.metric, line.value); err != nil {
			return err
		}
	}

	return nil
}
