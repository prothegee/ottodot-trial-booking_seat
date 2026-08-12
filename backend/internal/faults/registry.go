package faults

import (
    "errors"
    "sync"
    "time"
)

// The bounds an arm request is held to.
//
// Both exist for the same reason: a fault that outlives the person who armed it
// is a broken stack nobody remembers breaking. A count that runs out and a
// deadline that passes are two independent ways for the surface to clean up
// after somebody who walked away.
const (
    DefaultCount = 1
    MaxCount     = 10

    DefaultLifetime = 60 * time.Second
    MaxLifetime     = 10 * time.Minute
)

// The failures this package reports.
var (
    // ErrUnknownPoint means the name is not one of the points above. It is a
    // refusal rather than a silent no-op, so a typo in a demonstration script
    // is found before the run rather than during it.
    ErrUnknownPoint = errors.New("faults: no such injection point")

    // ErrInvalidRequest means the count or the lifetime was outside its bounds.
    ErrInvalidRequest = errors.New("faults: the arm request is outside its bounds")
)

// ArmRequest is one point being armed.
type ArmRequest struct {
    // Point is which place in the code path fails.
    Point string

    // Count is how many times it fails before disarming itself. Zero means the
    // default of one, which is what a demonstration wants: break the next
    // confirm and nothing after it.
    Count int

    // Lifetime is how long the arming lasts even if it is never triggered. Zero
    // means the default.
    Lifetime time.Duration
}

// Armed is one point that is currently live.
type Armed struct {
    Point     string    `json:"point"`
    Remaining int       `json:"remaining"`
    ExpiresAt time.Time `json:"expires_at"`
}

// Settings is what a registry is built with.
type Settings struct {
    // Clock is where now comes from. Nil means the real one. It is handed in so
    // a test can prove the lifetime expires without waiting a minute.
    Clock func() time.Time

    // OnTrigger is called each time a fault actually fires, with the point that
    // fired. It is where the counter and the warn line are raised, and it is a
    // function rather than a metrics dependency so this package imports nothing
    // of the sort.
    OnTrigger func(point string)
}

// armed is one live point's state.
type armed struct {
    remaining int
    expiresAt time.Time
}

// Registry is what is armed right now.
//
// It is per process and holds nothing durable, which is the correct scope: a
// fault is a thing somebody is doing to a running stack in front of them, and it
// must not survive a restart. Restarting the api is the last resort for undoing
// one, and it has to work.
type Registry struct {
    mutex     sync.Mutex
    armed     map[string]*armed
    clock     func() time.Time
    onTrigger func(point string)
}

// NewRegistry builds an empty registry.
//
// Param:
// settings - Settings (the clock and where a trigger is reported)
//
// Return:
//   - the registry, with nothing armed
func NewRegistry(settings Settings) *Registry {
    clock := settings.Clock
    if clock == nil {
        clock = time.Now
    }

    return &Registry{
        armed:     make(map[string]*armed),
        clock:     clock,
        onTrigger: settings.OnTrigger,
    }
}

// Arm makes a point fail the next time it is reached.
//
// Arming a point that is already armed replaces it rather than adding to it. Two
// people arming the same point should end with what the second one asked for,
// not with the sum of two intentions neither of them holds.
//
// Param:
// request - ArmRequest (which point, how many times, for how long)
//
// Return:
//   - what is now armed, so a caller can see the deadline it was given
//   - ErrUnknownPoint when the name is not on the list
//   - ErrInvalidRequest when the count or the lifetime is outside its bounds
func (registry *Registry) Arm(request ArmRequest) (Armed, error) {
    if !Known(request.Point) {
        return Armed{}, ErrUnknownPoint
    }

    count := request.Count
    if count == 0 {
        count = DefaultCount
    }

    lifetime := request.Lifetime
    if lifetime == 0 {
        lifetime = DefaultLifetime
    }

    if count < 1 || count > MaxCount || lifetime < 0 || lifetime > MaxLifetime {
        return Armed{}, ErrInvalidRequest
    }

    registry.mutex.Lock()
    defer registry.mutex.Unlock()

    expiresAt := registry.clock().Add(lifetime)

    registry.armed[request.Point] = &armed{remaining: count, expiresAt: expiresAt}

    return Armed{Point: request.Point, Remaining: count, ExpiresAt: expiresAt}, nil
}

// Trigger reports whether the named point should fail this time, and spends one
// of its remaining triggers when it does.
//
// This is the only method on the hot path, so it is written to be cheap and to
// be safe on a nil receiver. A service with no fault surface holds a nil registry
// and every call site is one nil comparison.
//
// Note:
//   - the expiry is checked here rather than by a sweeper. A point that has
//     timed out is only interesting at the moment something reaches it, and a
//     background goroutine to tidy at most five map entries would cost more than
//     it saves.
//
// Param:
// point - string (the call site's own point name)
//
// Return:
//   - true when the caller should fail, and never twice for a one-shot arming
func (registry *Registry) Trigger(point string) bool {
    if registry == nil {
        return false
    }

    registry.mutex.Lock()

    live, found := registry.armed[point]
    if !found {
        registry.mutex.Unlock()

        return false
    }

    if !registry.clock().Before(live.expiresAt) {
        delete(registry.armed, point)
        registry.mutex.Unlock()

        return false
    }

    live.remaining--
    if live.remaining <= 0 {
        delete(registry.armed, point)
    }

    report := registry.onTrigger

    registry.mutex.Unlock()

    // The report is made outside the lock. It writes a log line and touches a
    // counter, and holding a mutex across either would put the fault surface on
    // the critical path of every other call site.
    if report != nil {
        report(point)
    }

    return true
}

// List is everything armed right now.
//
// Expired entries are dropped as they are read, so the list never shows
// something that would not actually fire.
//
// Return:
//   - the live armings, one per point, in no particular order
func (registry *Registry) List() []Armed {
    if registry == nil {
        return nil
    }

    registry.mutex.Lock()
    defer registry.mutex.Unlock()

    now := registry.clock()
    listed := make([]Armed, 0, len(registry.armed))

    for point, live := range registry.armed {
        if !now.Before(live.expiresAt) {
            delete(registry.armed, point)

            continue
        }

        listed = append(listed, Armed{Point: point, Remaining: live.remaining, ExpiresAt: live.expiresAt})
    }

    return listed
}

// Disarm clears everything.
//
// It is always safe to call, including when nothing is armed, which is what
// makes it usable as the first line of a recovery: whatever state the stack is
// in, this puts it back.
func (registry *Registry) Disarm() {
    if registry == nil {
        return
    }

    registry.mutex.Lock()
    defer registry.mutex.Unlock()

    clear(registry.armed)
}
