package worker_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// noopHandler is a handler that succeeds and does nothing, which is all the
// registry cases need.
func noopHandler() worker.Handler {
    return worker.HandlerFunc(func(_ context.Context, _ queue.Job) error { return nil })
}

func TestTheRegistryRefusesWhatCouldNeverRun(t *testing.T) {
    t.Run("unit: a known kind registers", func(t *testing.T) {
        registry := worker.Registry{}

        if err := registry.Register(queue.KindExpireHold, noopHandler()); err != nil {
            t.Fatalf("expected the handler to register, got: %v", err)
        }
    })

    t.Run("edge: a kind this service does not run is refused", func(t *testing.T) {
        registry := worker.Registry{}

        if err := registry.Register("send_newsletter", noopHandler()); !errors.Is(err, worker.ErrUnknownKind) {
            t.Fatalf("expected ErrUnknownKind, got: %v", err)
        }
    })

    t.Run("edge: a kind with nothing to run it is refused", func(t *testing.T) {
        // Refusing here rather than panicking on the first job is the point.
        registry := worker.Registry{}

        if err := registry.Register(queue.KindExpireHold, nil); !errors.Is(err, worker.ErrHandlerMissing) {
            t.Fatalf("expected ErrHandlerMissing, got: %v", err)
        }
    })

    t.Run("edge: registering a kind twice is refused rather than replacing it", func(t *testing.T) {
        // A replaced handler is a change nobody can see, and the one that runs
        // would depend on registration order.
        registry := worker.Registry{}

        if err := registry.Register(queue.KindExpireHold, noopHandler()); err != nil {
            t.Fatalf("expected the first registration to land, got: %v", err)
        }

        err := registry.Register(queue.KindExpireHold, noopHandler())
        if !errors.Is(err, worker.ErrHandlerAlreadyRegistered) {
            t.Fatalf("expected ErrHandlerAlreadyRegistered, got: %v", err)
        }
    })
}

func TestCoverageIsAnsweredAgainstEveryKindThatExists(t *testing.T) {
    t.Run("unit: a registry with both kinds covers", func(t *testing.T) {
        registry := worker.Registry{}

        for _, kind := range queue.AllKinds() {
            if err := registry.Register(kind, noopHandler()); err != nil {
                t.Fatalf("expected %s to register, got: %v", kind, err)
            }
        }

        if !registry.Covers() {
            t.Fatal("expected a registry with every kind to cover")
        }
    })

    t.Run("edge: an empty registry covers nothing", func(t *testing.T) {
        if (worker.Registry{}).Covers() {
            t.Fatal("expected an empty registry to cover nothing")
        }
    })

    t.Run("edge: one kind short does not cover", func(t *testing.T) {
        // The case that matters: a new job kind is added and its handler is
        // forgotten. The queue would fill up and nothing would say why.
        registry := worker.Registry{}

        if err := registry.Register(queue.KindExpireHold, noopHandler()); err != nil {
            t.Fatalf("expected the handler to register, got: %v", err)
        }

        if registry.Covers() {
            t.Fatal("expected a registry missing a kind to say so")
        }
    })
}
