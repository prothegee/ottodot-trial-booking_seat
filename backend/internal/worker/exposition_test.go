package worker_test

import (
    "strings"
    "testing"

    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// renderExposition writes the exposition into a string, which is what every
// case here reads.
func renderExposition(t *testing.T, counted worker.Snapshot, depth queue.Depth) string {
    t.Helper()

    var written strings.Builder

    if err := worker.WriteExposition(&written, counted, depth); err != nil {
        t.Fatalf("expected the exposition to be written, got: %v", err)
    }

    return written.String()
}

func TestTheExpositionCarriesEveryNumberADashboardNeeds(t *testing.T) {
    t.Run("unit: every metric name appears", func(t *testing.T) {
        // A panel and an alert rule both name a metric by string. A rename
        // nobody notices turns a panel blank rather than red, so the names are
        // asserted here and again against the dashboard in phase 7.
        rendered := renderExposition(t,
            worker.Snapshot{Claimed: 5, Completed: 4, Failed: 1},
            queue.Depth{Ready: 2, Claimed: 1, Parked: 3})

        for _, name := range []string{
            "queue_depth",
            "worker_jobs_claimed_total",
            "worker_jobs_completed_total",
            "queue_job_failed_total",
        } {
            if !strings.Contains(rendered, name) {
                t.Fatalf("expected %s in the exposition, got:\n%s", name, rendered)
            }
        }
    })

    t.Run("unit: the three depth states are separate series", func(t *testing.T) {
        // One total would hide the difference between a worker that is behind
        // and a queue full of jobs nobody can run.
        rendered := renderExposition(t,
            worker.Snapshot{},
            queue.Depth{Ready: 2, Claimed: 1, Parked: 3})

        for line, expected := range map[string]string{
            `queue_depth{state="ready"}`:   `queue_depth{state="ready"} 2`,
            `queue_depth{state="claimed"}`: `queue_depth{state="claimed"} 1`,
            `queue_depth{state="parked"}`:  `queue_depth{state="parked"} 3`,
        } {
            if !strings.Contains(rendered, expected) {
                t.Fatalf("expected %s, got:\n%s", line, rendered)
            }
        }
    })

    t.Run("unit: every counter carries its value", func(t *testing.T) {
        rendered := renderExposition(t,
            worker.Snapshot{Claimed: 5, Completed: 4, Failed: 1},
            queue.Depth{})

        for _, expected := range []string{
            "worker_jobs_claimed_total 5",
            "worker_jobs_completed_total 4",
            "queue_job_failed_total 1",
        } {
            if !strings.Contains(rendered, expected) {
                t.Fatalf("expected %q, got:\n%s", expected, rendered)
            }
        }
    })

    t.Run("edge: a metric with several series declares its type once", func(t *testing.T) {
        // The text format requires it, and a second TYPE line makes Prometheus
        // drop the whole scrape.
        rendered := renderExposition(t, worker.Snapshot{}, queue.Depth{})

        if declared := strings.Count(rendered, "# TYPE queue_depth "); declared != 1 {
            t.Fatalf("expected one TYPE line for queue_depth, got %d in:\n%s", declared, rendered)
        }
    })

    t.Run("edge: a fresh worker publishes zeroes rather than nothing", func(t *testing.T) {
        // An absent series and a series at zero read differently on a graph.
        // A worker that has done nothing yet is not a worker that is missing.
        rendered := renderExposition(t, worker.Snapshot{}, queue.Depth{})

        if !strings.Contains(rendered, "worker_jobs_claimed_total 0") {
            t.Fatalf("expected a zero rather than an absent series, got:\n%s", rendered)
        }
    })

    t.Run("edge: no line carries an identifier", func(t *testing.T) {
        // The rule for /metrics is that it holds no identifier, because a label
        // with a booking id in it turns a dashboard into a personal record.
        rendered := renderExposition(t,
            worker.Snapshot{Claimed: 1},
            queue.Depth{Ready: 1})

        for _, line := range strings.Split(strings.TrimSpace(rendered), "\n") {
            if strings.Count(line, "-") >= 4 {
                t.Fatalf("a line that looks like it carries a uuid: %q", line)
            }
        }
    })
}
