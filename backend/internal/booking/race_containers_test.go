//go:build containers

// The proof the four fast tiers cannot give.
//
// A fake repository proves the service calls the right things in the right
// order. It cannot prove that SELECT ... FOR UPDATE serializes two
// transactions, because there is no transaction. Tests 4 and 5 run real
// parallel connections against real Postgres, which is the only way that claim
// gets tested.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/booking/...
package booking_test

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "testing"
    "time"

    "ottodot-trial-booking/backend/internal/booking"
)

// raceClass is the class every fan-out below is run against. One class per
// scratch schema, so the identifier can be fixed.
const raceClass = "0192d000-0000-7000-8000-000000000021"

// raceOutcome is what one fan-out of parallel confirms produced.
type raceOutcome struct {
    confirmed int
    lost      int
    seats     []int16
    statuses  map[booking.Status]int
}

// runParallelConfirms puts the given number of parents on the payment screen of
// one class, then has every one of them settle at the same instant.
//
// Each parent has their own child and their own account, so the per-parent hold
// cap plays no part in what is being measured. The class allowance is opened
// wide enough for all of them, because the point of the exercise is what
// happens when they all reach the confirm transaction together.
//
// Param:
// capacity - int16 (how many seats the class has)
// holders - int (how many parents race for them)
//
// Return:
//   - what each parent ended up with, and the seats the class ended up handing
//     out
func runParallelConfirms(t *testing.T, capacity int16, holders int) raceOutcome {
    t.Helper()

    ctx := context.Background()

    pool := newScratchPool(t)
    repository := booking.NewPostgresRepository(pool)
    fixture := &postgresFixture{pool: pool, repository: repository}

    fixture.AddClass(t, booking.Class{
        ID: raceClass, Subject: "science", Title: "Science Lab Race",
        StartsAt: time.Now().Add(72 * time.Hour), DurationMinutes: 60,
        Capacity: capacity, HoldAllowance: int16(holders) - capacity,
    })

    held := make([]string, 0, holders)

    for index := range holders {
        parentID := fmt.Sprintf("0192d000-0000-7000-8000-%012d", 1000+index)
        studentID := fmt.Sprintf("0192d000-0000-7000-8000-%012d", 2000+index)

        fixture.AddStudent(t, studentID, parentID)

        granted, err := repository.Hold(ctx, holdRequestFor(t, studentID, raceClass, time.Now()))
        if err != nil {
            t.Fatalf("every racer must reach the payment screen, holder %d got: %v", index, err)
        }

        held = append(held, granted.ID)
    }

    var (
        start     = make(chan struct{})
        waitGroup sync.WaitGroup
        mutex     sync.Mutex
        outcome   = raceOutcome{statuses: make(map[booking.Status]int)}
    )

    for _, bookingID := range held {
        waitGroup.Add(1)

        go func(bookingID string) {
            defer waitGroup.Done()

            // Every goroutine waits on the same channel, so they arrive at the
            // row lock together rather than in the order they were started.
            <-start

            settled, err := repository.Confirm(context.Background(), booking.ConfirmRequest{
                BookingID: bookingID,
                Now:       time.Now(),
            })

            mutex.Lock()
            defer mutex.Unlock()

            outcome.statuses[settled.Status]++

            switch {
            case err == nil:
                outcome.confirmed++
            case errors.Is(err, booking.ErrSeatLost):
                outcome.lost++
            default:
                t.Errorf("unexpected failure from a parallel confirm: %v", err)
            }
        }(bookingID)
    }

    close(start)
    waitGroup.Wait()

    seats, err := repository.SeatsTaken(ctx, raceClass)
    if err != nil {
        t.Fatalf("expected the taken seats, got: %v", err)
    }

    outcome.seats = seats

    return outcome
}

// assertSeatsAreExactly checks the class handed out one seat per number, with
// no gap and no repeat.
func assertSeatsAreExactly(t *testing.T, seats []int16, expected int) {
    t.Helper()

    if len(seats) != expected {
        t.Fatalf("expected %d seats to be taken, got %d: %v", expected, len(seats), seats)
    }

    for index, seat := range seats {
        if seat != int16(index+1) {
            t.Fatalf("expected seats 1 to %d in order with no gaps, got %v", expected, seats)
        }
    }
}

func TestSimulation04ParallelConfirmsOnOneFreeSeat(t *testing.T) {
    t.Run("proof: ten parallel confirms on a one seat class produce exactly one winner", func(t *testing.T) {
        // This is the bug the whole exercise is about. Ten connections reach
        // the confirm transaction at the same instant. The row lock lets them
        // through one at a time, the first finds seat 1 free, and the other
        // nine each find nothing.
        outcome := runParallelConfirms(t, 1, 10)

        if outcome.confirmed != 1 {
            t.Fatalf("expected exactly one confirmed booking, got %d", outcome.confirmed)
        }

        if outcome.lost != 9 {
            t.Fatalf("expected nine parents to lose the seat, got %d", outcome.lost)
        }

        assertSeatsAreExactly(t, outcome.seats, 1)

        if outcome.statuses[booking.StatusRefundRequired] != 9 {
            t.Fatalf("expected nine bookings in refund_required, got %d", outcome.statuses[booking.StatusRefundRequired])
        }
    })
}

func TestSimulation05ParallelConfirmsOnAnEmptyFourSeatClass(t *testing.T) {
    t.Run("proof: twenty parallel confirms on a four seat class fill it exactly once", func(t *testing.T) {
        outcome := runParallelConfirms(t, 4, 20)

        if outcome.confirmed != 4 {
            t.Fatalf("expected exactly four confirmed bookings, got %d", outcome.confirmed)
        }

        if outcome.lost != 16 {
            t.Fatalf("expected sixteen parents to lose a seat, got %d", outcome.lost)
        }

        assertSeatsAreExactly(t, outcome.seats, 4)

        // Every loser ends in refund_required, which is how this test also
        // proves uq_seat_taken never fired. A unique violation would have
        // rolled its transaction back and left that booking in
        // pending_payment, because the refund outcome is written in the same
        // transaction that failed.
        if outcome.statuses[booking.StatusRefundRequired] != 16 {
            t.Fatalf("expected sixteen bookings in refund_required, got %d", outcome.statuses[booking.StatusRefundRequired])
        }

        if outcome.statuses[booking.StatusPendingPayment] != 0 {
            t.Fatalf("the seat index fired instead of the lock, %d bookings were rolled back",
                outcome.statuses[booking.StatusPendingPayment])
        }
    })
}
