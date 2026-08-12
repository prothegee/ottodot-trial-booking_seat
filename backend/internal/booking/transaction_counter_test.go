package booking

import (
    "errors"
    "fmt"
    "os"
    "regexp"
    "testing"
)

// recordingCounter remembers every line it was given, in order.
type recordingCounter struct {
    names    []string
    outcomes []string
}

func (counter *recordingCounter) DatabaseTransaction(name string, outcome string) {
    counter.names = append(counter.names, name)
    counter.outcomes = append(counter.outcomes, outcome)
}

func TestATransactionIsSortedIntoOneOfThreeOutcomes(t *testing.T) {
    t.Run("unit: a transaction that returned nothing committed", func(t *testing.T) {
        if outcome := transactionOutcome(nil); outcome != transactionCommit {
            t.Fatalf("expected %q, got %q", transactionCommit, outcome)
        }
    })

    t.Run("unit: a race another parent won is a conflict, not a failure", func(t *testing.T) {
        // Folding these into rollback would make an alert on rollbacks fire on
        // a busy morning, when every one of them is the design working.
        for _, err := range []error{
            ErrSeatLost,
            ErrClassFull,
            ErrAlreadyBooked,
            ErrTooManyHolds,
            ErrLockWaitTimeout,
        } {
            if outcome := transactionOutcome(err); outcome != transactionConflict {
                t.Fatalf("expected %q for %v, got %q", transactionConflict, err, outcome)
            }
        }
    })

    t.Run("unit: anything else that did not commit rolled back", func(t *testing.T) {
        if outcome := transactionOutcome(ErrBookingNotFound); outcome != transactionRollback {
            t.Fatalf("expected %q, got %q", transactionRollback, outcome)
        }
    })

    t.Run("edge: a wrapped error is sorted by what it wraps", func(t *testing.T) {
        // Every method here returns its failure wrapped, so a check that only
        // compared values would call each of them a rollback.
        wrapped := fmt.Errorf("the hold transaction: %w", ErrClassFull)

        if outcome := transactionOutcome(wrapped); outcome != transactionConflict {
            t.Fatalf("expected %q, got %q", transactionConflict, outcome)
        }
    })
}

func TestTheRepositoryCountsOnlyWhenItWasGivenACounter(t *testing.T) {
    t.Run("edge: a repository without a counter records nothing and does not panic", func(t *testing.T) {
        // This is the ordinary case. Every caller that publishes no metrics
        // builds a repository and never calls the setter.
        repository := &PostgresRepository{}

        repository.countTransaction(transactionHold, nil)
    })

    t.Run("unit: a counted transaction arrives under its own name", func(t *testing.T) {
        counter := &recordingCounter{}
        repository := &PostgresRepository{}

        repository.CountTransactions(counter)
        repository.countTransaction(transactionConfirm, nil)
        repository.countTransaction(transactionExpire, ErrSeatLost)
        repository.countTransaction(transactionCancel, errors.New("the connection went away"))

        if len(counter.names) != 3 {
            t.Fatalf("expected three lines, got %d", len(counter.names))
        }

        expectedNames := []string{transactionConfirm, transactionExpire, transactionCancel}
        expectedOutcomes := []string{transactionCommit, transactionConflict, transactionRollback}

        for index := range expectedNames {
            if counter.names[index] != expectedNames[index] {
                t.Fatalf("line %d: expected name %q, got %q",
                    index, expectedNames[index], counter.names[index])
            }

            if counter.outcomes[index] != expectedOutcomes[index] {
                t.Fatalf("line %d: expected outcome %q, got %q",
                    index, expectedOutcomes[index], counter.outcomes[index])
            }
        }
    })

    t.Run("unit: every transactional method counts under a name of its own", func(t *testing.T) {
        // A name reused by two methods would add their two lines together on the
        // panel, and neither would be readable afterwards.
        names := []string{
            transactionHold,
            transactionConfirm,
            transactionCancel,
            transactionFail,
            transactionExpire,
        }

        seen := make(map[string]bool, len(names))

        for _, name := range names {
            if seen[name] {
                t.Fatalf("two transactions count under %q", name)
            }

            seen[name] = true
        }
    })
}

func TestTheNamesHandedOutCoverEveryTransaction(t *testing.T) {
    t.Run("integration: a name declared in this file is one a caller can create at zero", func(t *testing.T) {
        // Read from the source rather than listed here, because a sixth
        // transaction added to that block and left out of TransactionNames would
        // pass a list written twice, and its panel line would stay missing until
        // the first one ran.
        source, err := os.ReadFile("transaction_counter.go")
        if err != nil {
            t.Fatalf("expected to read this package's source, got: %v", err)
        }

        outcomes := map[string]bool{transactionCommit: true, transactionRollback: true, transactionConflict: true}

        handed := make(map[string]bool, len(TransactionNames()))
        for _, name := range TransactionNames() {
            handed[name] = true
        }

        for _, found := range regexp.MustCompile(`transaction\w+\s+= "([a-z_]+)"`).FindAllStringSubmatch(string(source), -1) {
            declared := found[1]

            if outcomes[declared] || handed[declared] {
                continue
            }

            t.Errorf("%q is a transaction name that TransactionNames does not hand out", declared)
        }
    })
}
