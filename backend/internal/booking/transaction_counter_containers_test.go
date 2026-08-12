//go:build containers

// What the counter records when the transactions are real ones.
//
// The fast tier proves the sorting of an error into an outcome. It cannot prove
// that a method which commits reports a commit, because that needs a database
// that can commit. This is that half.
//
// Run it with:
//
//	scripts/stack_up.sh backend
//	cd backend && go test -tags=containers ./internal/booking/...
package booking_test

import (
    "context"
    "testing"
)

// countedTransaction is one line the repository handed to the counter.
type countedTransaction struct {
    name    string
    outcome string
}

// countingSpy stands in for the metrics sink and keeps what it was told.
type countingSpy struct {
    lines []countedTransaction
}

func (spy *countingSpy) DatabaseTransaction(name string, outcome string) {
    spy.lines = append(spy.lines, countedTransaction{name: name, outcome: outcome})
}

// only returns the single line recorded, and fails when there is not exactly one.
func (spy *countingSpy) only(t *testing.T) countedTransaction {
    t.Helper()

    if len(spy.lines) != 1 {
        t.Fatalf("expected one counted transaction, got %d: %v", len(spy.lines), spy.lines)
    }

    return spy.lines[0]
}

func TestARealTransactionIsCountedByHowItEnded(t *testing.T) {
    ctx := context.Background()

    t.Run("proof: a hold that committed is counted as a commit", func(t *testing.T) {
        fixture, ok := newPostgresFixture(t).(*postgresFixture)
        if !ok {
            t.Fatal("the postgres fixture no longer carries the concrete repository")
        }

        seedContractFixture(t, fixture)

        spy := &countingSpy{}
        fixture.repository.CountTransactions(spy)

        if _, err := fixture.repository.Hold(ctx,
            holdRequestFor(t, studentOne, classOpen, contractMoment)); err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }

        line := spy.only(t)

        if line.name != "hold_seat" {
            t.Fatalf("expected the line to be named hold_seat, got %q", line.name)
        }

        if line.outcome != "commit" {
            t.Fatalf("expected a commit, got %q", line.outcome)
        }
    })

    t.Run("proof: a second live hold for the same child is counted as a conflict", func(t *testing.T) {
        // The refusal comes from uq_booking_active rather than from a check in
        // Go, so this is the case that shows the outcome is read from what the
        // database actually did.
        fixture, ok := newPostgresFixture(t).(*postgresFixture)
        if !ok {
            t.Fatal("the postgres fixture no longer carries the concrete repository")
        }

        seedContractFixture(t, fixture)

        if _, err := fixture.repository.Hold(ctx,
            holdRequestFor(t, studentOne, classOpen, contractMoment)); err != nil {
            t.Fatalf("expected the first hold to be granted, got: %v", err)
        }

        spy := &countingSpy{}
        fixture.repository.CountTransactions(spy)

        if _, err := fixture.repository.Hold(ctx,
            holdRequestFor(t, studentOne, classOpen, contractMoment)); err == nil {
            t.Fatal("expected the second hold to be refused")
        }

        line := spy.only(t)

        if line.outcome != "conflict" {
            t.Fatalf("expected a conflict, got %q", line.outcome)
        }
    })

    t.Run("proof: a repository nobody counted still grants a hold", func(t *testing.T) {
        // Counting is optional, and the path without it is the one every test
        // and every caller outside the two processes takes.
        fixture := newPostgresFixture(t)

        seedContractFixture(t, fixture)

        if _, err := fixture.Repository().Hold(ctx,
            holdRequestFor(t, studentOne, classOpen, contractMoment)); err != nil {
            t.Fatalf("expected the hold to be granted, got: %v", err)
        }
    })
}
