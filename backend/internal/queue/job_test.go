package queue_test

import (
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/queue"
)

// moment is the instant every case in this file is judged against.
var moment = time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)

func TestOnlyTheTwoAllowedKindsAreKnown(t *testing.T) {
	t.Run("unit: both kinds the constraint allows are known", func(t *testing.T) {
		for _, kind := range queue.AllKinds() {
			if !kind.IsKnown() {
				t.Fatalf("expected %s to be known", kind)
			}
		}
	})

	t.Run("unit: the list carries exactly the two kinds", func(t *testing.T) {
		if len(queue.AllKinds()) != 2 {
			t.Fatalf("expected two kinds, got %d", len(queue.AllKinds()))
		}
	})

	t.Run("edge: anything else is refused, including an empty kind", func(t *testing.T) {
		for _, kind := range []queue.Kind{"", "expire", "EXPIRE_HOLD", "send_email", " expire_hold"} {
			if kind.IsKnown() {
				t.Fatalf("expected %q to be refused", kind)
			}
		}
	})
}

func TestAClaimIsBelievedOnlyWhileItsLeaseStands(t *testing.T) {
	t.Run("unit: a lease in the future means another worker holds it", func(t *testing.T) {
		held := queue.Job{LockedUntil: moment.Add(time.Minute)}

		if !held.IsClaimed(moment) {
			t.Fatal("expected a job with a standing lease to read as claimed")
		}
	})

	t.Run("unit: no lease at all means nobody holds it", func(t *testing.T) {
		free := queue.Job{}

		if free.IsClaimed(moment) {
			t.Fatal("expected a job with no lease to read as free")
		}
	})

	t.Run("edge: a lease expiring on this exact instant is already lapsed", func(t *testing.T) {
		// The boundary matters because it decides whether a worker that died
		// holding a job blocks it for one more poll. Lapsed at the instant
		// means the recovery happens as early as it honestly can.
		lapsed := queue.Job{LockedUntil: moment}

		if lapsed.IsClaimed(moment) {
			t.Fatal("expected a lease ending on this instant to read as lapsed")
		}
	})

	t.Run("edge: a lease one nanosecond ahead still stands", func(t *testing.T) {
		held := queue.Job{LockedUntil: moment.Add(time.Nanosecond)}

		if !held.IsClaimed(moment) {
			t.Fatal("expected a lease ending after this instant to still stand")
		}
	})
}

func TestAJobIsDueOnceItsInstantArrives(t *testing.T) {
	t.Run("unit: an instant in the past is due", func(t *testing.T) {
		due := queue.Job{RunAfter: moment.Add(-time.Minute)}

		if !due.IsDue(moment) {
			t.Fatal("expected a job scheduled in the past to be due")
		}
	})

	t.Run("unit: an instant in the future is not", func(t *testing.T) {
		waiting := queue.Job{RunAfter: moment.Add(time.Minute)}

		if waiting.IsDue(moment) {
			t.Fatal("expected a job scheduled ahead to wait")
		}
	})

	t.Run("edge: a job scheduled for this exact instant is due", func(t *testing.T) {
		// A hold expiry is scheduled for the deadline itself, so this boundary
		// is the ordinary case rather than an unusual one.
		due := queue.Job{RunAfter: moment}

		if !due.IsDue(moment) {
			t.Fatal("expected a job scheduled for this instant to be due")
		}
	})
}

func TestAJobStopsBeingHandedOutOnceItsAttemptsAreSpent(t *testing.T) {
	t.Run("unit: fewer attempts than the cap is still runnable", func(t *testing.T) {
		runnable := queue.Job{Attempts: 2}

		if runnable.IsParked(3) {
			t.Fatal("expected a job below the cap to still be runnable")
		}
	})

	t.Run("edge: reaching the cap exactly is parked", func(t *testing.T) {
		parked := queue.Job{Attempts: 3}

		if !parked.IsParked(3) {
			t.Fatal("expected a job at the cap to be parked")
		}
	})

	t.Run("edge: a fresh job is runnable under any cap of one or more", func(t *testing.T) {
		fresh := queue.Job{}

		if fresh.IsParked(1) {
			t.Fatal("expected a job that has never run to be runnable")
		}
	})
}
