package worker_test

import (
	"sync"
	"testing"

	"ottodot-trial-booking/backend/internal/worker"
)

func TestTheCountersOnlyEverGoUp(t *testing.T) {
	t.Run("unit: a fresh set is all zeroes", func(t *testing.T) {
		counted := worker.NewCounters().Snapshot()

		if counted.Claimed != 0 || counted.Completed != 0 || counted.Failed != 0 {
			t.Fatalf("expected a fresh set to be empty, got %+v", counted)
		}
	})

	t.Run("unit: each count moves on its own", func(t *testing.T) {
		counters := worker.NewCounters()

		counters.Claimed(3)
		counters.Completed()
		counters.Failed()
		counters.Failed()

		counted := counters.Snapshot()

		if counted.Claimed != 3 || counted.Completed != 1 || counted.Failed != 2 {
			t.Fatalf("expected 3, 1, 2, got %+v", counted)
		}
	})

	t.Run("edge: a poll that claimed nothing changes nothing", func(t *testing.T) {
		// An empty poll is the ordinary case on a quiet queue, and counting it
		// would make the claimed number describe polls rather than jobs.
		counters := worker.NewCounters()

		counters.Claimed(0)
		counters.Claimed(-1)

		if counted := counters.Snapshot(); counted.Claimed != 0 {
			t.Fatalf("expected nothing counted, got %d", counted.Claimed)
		}
	})
}

func TestTheCountersAreSafeToShare(t *testing.T) {
	t.Run("integration: parallel jobs all land in the totals", func(t *testing.T) {
		// One worker runs a batch concurrently and a scrape reads at the same
		// time. A count that dropped writes would under-report the failures an
		// alert fires on.
		counters := worker.NewCounters()

		const writers = 16

		var waitGroup sync.WaitGroup

		for writer := 0; writer < writers; writer++ {
			waitGroup.Add(1)

			go func() {
				defer waitGroup.Done()

				counters.Claimed(1)
				counters.Completed()
				counters.Snapshot()
			}()
		}

		waitGroup.Wait()

		counted := counters.Snapshot()

		if counted.Claimed != writers || counted.Completed != writers {
			t.Fatalf("expected %d of each, got %+v", writers, counted)
		}
	})
}
