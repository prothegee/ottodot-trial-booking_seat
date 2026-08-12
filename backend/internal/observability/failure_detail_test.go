package observability_test

import (
    "context"
    "errors"
    "testing"

    "ottodot-trial-booking/backend/internal/observability"
)

func TestCarryingTheDetailBehindAFailure(t *testing.T) {
    t.Run("unit: what was recorded is what comes back", func(t *testing.T) {
        ctx, detail := observability.WithFailureDetail(context.Background())

        recorded := errors.New(`pq: relation "parents" does not exist`)
        observability.RecordFailureDetail(ctx, recorded)

        if !errors.Is(detail.Err(), recorded) {
            t.Fatalf("the detail came back as %v, so the reason for a 500 is still lost", detail.Err())
        }
    })

    t.Run("behaviour: the first error is the one kept", func(t *testing.T) {
        ctx, detail := observability.WithFailureDetail(context.Background())

        first := errors.New("the pool is closed")

        observability.RecordFailureDetail(ctx, first)
        observability.RecordFailureDetail(ctx, errors.New("and then the retry failed too"))

        if !errors.Is(detail.Err(), first) {
            t.Fatalf("a later error replaced the one that caused the answer: %v", detail.Err())
        }
    })

    t.Run("edge: a context carrying nothing is not a failure of its own", func(t *testing.T) {
        observability.RecordFailureDetail(context.Background(), errors.New("nowhere to put this"))
    })

    t.Run("edge: nothing failed, so nothing is recorded", func(t *testing.T) {
        ctx, detail := observability.WithFailureDetail(context.Background())

        observability.RecordFailureDetail(ctx, nil)

        if detail.Err() != nil {
            t.Fatalf("a request that failed in no way reported %v", detail.Err())
        }
    })
}
