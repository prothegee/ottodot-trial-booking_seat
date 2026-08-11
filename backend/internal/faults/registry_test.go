package faults_test

import (
    "errors"
    "sync"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/faults"
)

// fixedClock is a clock a test moves by hand, so a lifetime can be proven to
// expire without a case that sleeps for a minute.
type fixedClock struct {
    mutex sync.Mutex
    now   time.Time
}

func newClock() *fixedClock {
    return &fixedClock{now: time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)}
}

func (clock *fixedClock) Now() time.Time {
    clock.mutex.Lock()
    defer clock.mutex.Unlock()

    return clock.now
}

func (clock *fixedClock) Advance(by time.Duration) {
    clock.mutex.Lock()
    defer clock.mutex.Unlock()

    clock.now = clock.now.Add(by)
}

func TestRegistry(t *testing.T) {
    t.Run("integration: an armed point fires once and then stops", func(t *testing.T) {
        // One shot is the default because it is what a demonstration wants:
        // break the next confirm, and leave everything after it alone.
        registry := faults.NewRegistry(faults.Settings{})

        if _, err := registry.Arm(faults.ArmRequest{Point: faults.PointConfirmBeforeCommit}); err != nil {
            t.Fatalf("arming was refused: %v", err)
        }

        if !registry.Trigger(faults.PointConfirmBeforeCommit) {
            t.Fatal("the first call through the point did not fire")
        }

        if registry.Trigger(faults.PointConfirmBeforeCommit) {
            t.Fatal("a one shot fired twice, so the next parent's booking would break too")
        }
    })

    t.Run("unit: a count fires exactly that many times", func(t *testing.T) {
        registry := faults.NewRegistry(faults.Settings{})

        if _, err := registry.Arm(faults.ArmRequest{Point: faults.PointQueueJobError, Count: 3}); err != nil {
            t.Fatalf("arming was refused: %v", err)
        }

        fired := 0

        for attempt := 0; attempt < 5; attempt++ {
            if registry.Trigger(faults.PointQueueJobError) {
                fired++
            }
        }

        if fired != 3 {
            t.Fatalf("the point fired %d times out of five attempts", fired)
        }
    })

    t.Run("edge: an arming that is never triggered expires on its own", func(t *testing.T) {
        // This is the guard against the worst outcome: somebody arms a point,
        // walks away, and the stack is broken tomorrow with nobody remembering
        // why.
        clock := newClock()
        registry := faults.NewRegistry(faults.Settings{Clock: clock.Now})

        if _, err := registry.Arm(faults.ArmRequest{Point: faults.PointCacheRedisError, Lifetime: 30 * time.Second}); err != nil {
            t.Fatalf("arming was refused: %v", err)
        }

        clock.Advance(31 * time.Second)

        if registry.Trigger(faults.PointCacheRedisError) {
            t.Fatal("an expired arming still fired")
        }

        if len(registry.List()) != 0 {
            t.Fatal("an expired arming is still listed, so the list shows something that would not happen")
        }
    })

    t.Run("edge: an unknown point is refused rather than quietly ignored", func(t *testing.T) {
        // A silent no-op would mean a typo in a recording script is found during
        // the take rather than before it.
        registry := faults.NewRegistry(faults.Settings{})

        if _, err := registry.Arm(faults.ArmRequest{Point: "confirm.before_comit"}); !errors.Is(err, faults.ErrUnknownPoint) {
            t.Fatalf("a misspelt point answered %v", err)
        }
    })

    t.Run("edge: a count or a lifetime outside its bounds is refused", func(t *testing.T) {
        registry := faults.NewRegistry(faults.Settings{})

        cases := []faults.ArmRequest{
            {Point: faults.PointConfirmBeforeCommit, Count: faults.MaxCount + 1},
            {Point: faults.PointConfirmBeforeCommit, Count: -1},
            {Point: faults.PointConfirmBeforeCommit, Lifetime: faults.MaxLifetime + time.Second},
            {Point: faults.PointConfirmBeforeCommit, Lifetime: -time.Second},
        }

        for _, request := range cases {
            if _, err := registry.Arm(request); !errors.Is(err, faults.ErrInvalidRequest) {
                t.Errorf("count %d lifetime %s answered %v", request.Count, request.Lifetime, err)
            }
        }
    })

    t.Run("unit: arming the same point twice replaces rather than adds", func(t *testing.T) {
        // Two people arming the same point should end with what the second one
        // asked for, not with the sum of two intentions neither of them holds.
        registry := faults.NewRegistry(faults.Settings{})

        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointConfirmLockWait, Count: 5})
        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointConfirmLockWait, Count: 1})

        if !registry.Trigger(faults.PointConfirmLockWait) {
            t.Fatal("the replacement did not fire at all")
        }

        if registry.Trigger(faults.PointConfirmLockWait) {
            t.Fatal("the first arming's remaining count survived the replacement")
        }
    })

    t.Run("unit: a point nobody armed never fires", func(t *testing.T) {
        registry := faults.NewRegistry(faults.Settings{})

        for _, point := range faults.Points() {
            if registry.Trigger(point) {
                t.Errorf("%s fired without being armed", point)
            }
        }
    })

    t.Run("unit: a nil registry is the off switch", func(t *testing.T) {
        // A service with no fault surface holds a nil registry, so every call
        // site in the real code path is one nil comparison and nothing else.
        var registry *faults.Registry

        if registry.Trigger(faults.PointConfirmBeforeCommit) {
            t.Fatal("a nil registry fired")
        }

        if registry.List() != nil {
            t.Fatal("a nil registry listed something")
        }

        registry.Disarm()
    })

    t.Run("unit: disarm clears everything and is safe with nothing armed", func(t *testing.T) {
        registry := faults.NewRegistry(faults.Settings{})

        registry.Disarm()

        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointConfirmBeforeCommit, Count: 5})
        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointQueueJobError, Count: 5})

        registry.Disarm()

        if len(registry.List()) != 0 {
            t.Fatal("something survived a disarm")
        }
    })

    t.Run("integration: every trigger is reported once", func(t *testing.T) {
        // The report is what raises fault_injected_total and the warn line. A
        // fault that fires without being counted is a broken stack with no trace
        // of why.
        var reported []string

        registry := faults.NewRegistry(faults.Settings{
            OnTrigger: func(point string) { reported = append(reported, point) },
        })

        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointPaymentProviderError, Count: 2})

        registry.Trigger(faults.PointPaymentProviderError)
        registry.Trigger(faults.PointPaymentProviderError)
        registry.Trigger(faults.PointPaymentProviderError)

        if len(reported) != 2 {
            t.Fatalf("%d triggers were reported for two firings", len(reported))
        }
    })

    t.Run("behaviour: a count is spent once under real parallelism", func(t *testing.T) {
        // Twenty goroutines through a point armed once. Exactly one of them has
        // to fail, because a fault that can fire more often than it was armed
        // for is a fault nobody can reason about during a recording.
        registry := faults.NewRegistry(faults.Settings{})

        _, _ = registry.Arm(faults.ArmRequest{Point: faults.PointConfirmBeforeCommit, Count: 1})

        var (
            waiting sync.WaitGroup
            counter sync.Mutex
            fired   int
        )

        for attempt := 0; attempt < 20; attempt++ {
            waiting.Add(1)

            go func() {
                defer waiting.Done()

                if registry.Trigger(faults.PointConfirmBeforeCommit) {
                    counter.Lock()
                    fired++
                    counter.Unlock()
                }
            }()
        }

        waiting.Wait()

        if fired != 1 {
            t.Fatalf("a one shot fired %d times under twenty parallel callers", fired)
        }
    })
}

func TestPoints(t *testing.T) {
    t.Run("unit: every point is known and the list cannot be edited by a caller", func(t *testing.T) {
        listed := faults.Points()

        if len(listed) == 0 {
            t.Fatal("there are no injection points at all")
        }

        for _, point := range listed {
            if !faults.Known(point) {
                t.Errorf("%s is listed but not recognised", point)
            }
        }

        listed[0] = "something else"

        if !faults.Known(faults.Points()[0]) {
            t.Fatal("editing the returned list changed the real one")
        }
    })
}
