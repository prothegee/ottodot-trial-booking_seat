package booking

import "errors"

// Counting the transactions this repository opens.
//
// The interface is declared here rather than taken from the observability
// package, so this package keeps deciding seats without importing the monitoring
// and a repository built without a counter counts nothing.

// TransactionCounter receives one line per finished transaction.
//
// Note:
//   - the name is a constant from the caller, never a query or anything a
//     request carries. A label taken from outside would let a caller invent
//     series at will
type TransactionCounter interface {
    DatabaseTransaction(name string, outcome string)
}

// How a transaction ended, in the words the metric already uses.
const (
    transactionCommit   = "commit"
    transactionRollback = "rollback"
    transactionConflict = "conflict"
)

// The name each transactional method counts under.
const (
    transactionHold    = "hold_seat"
    transactionConfirm = "confirm_seat"
    transactionCancel  = "cancel_hold"
    transactionFail    = "fail_booking"
    transactionExpire  = "expire_hold"
)

// TransactionNames is every name a transaction is counted under.
//
// It is handed out so a caller can create the series at zero before any booking
// happens, which is what stops a panel on a quiet stack reading No data.
//
// Return:
//   - the five names, in the order they occur in a booking
func TransactionNames() []string {
    return []string{transactionHold, transactionConfirm, transactionCancel, transactionFail, transactionExpire}
}

// CountTransactions points this repository at a counter.
//
// A setter rather than a constructor argument, for the same reason as
// InjectFaults: every caller that does not publish metrics builds a repository
// and never learns this method exists.
//
// Param:
// counter - TransactionCounter (where each finished transaction is recorded)
func (repository *PostgresRepository) CountTransactions(counter TransactionCounter) {
    repository.transactions = counter
}

// countTransaction records one finished transaction under its own name.
//
// Param:
// name - string (which transaction, one of the constants above)
// err - error (what the method is returning, nil when the transaction committed)
func (repository *PostgresRepository) countTransaction(name string, err error) {
    if repository.transactions == nil {
        return
    }

    repository.transactions.DatabaseTransaction(name, transactionOutcome(err))
}

// transactionOutcome sorts one returned error into the three outcomes.
//
// Note:
//   - a conflict is a transaction that did its job. Two parents reached the last
//     seat and one was turned away, which is the design working rather than a
//     failure, and folding it into rollback would make an alert on rollbacks
//     fire on a busy morning
//
// Param:
// err - error (what the method is returning)
//
// Return:
//   - commit, conflict, or rollback
func transactionOutcome(err error) string {
    if err == nil {
        return transactionCommit
    }

    switch {
    case errors.Is(err, ErrSeatLost),
        errors.Is(err, ErrClassFull),
        errors.Is(err, ErrAlreadyBooked),
        errors.Is(err, ErrTooManyHolds),
        errors.Is(err, ErrLockWaitTimeout):
        return transactionConflict
    }

    return transactionRollback
}
