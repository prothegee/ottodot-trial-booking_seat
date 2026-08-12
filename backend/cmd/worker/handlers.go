package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/observability"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// buildHandlers wires one handler per job kind.
//
// Everything here reads and writes the primary pool. A worker decides whether a
// hold has lapsed and whether money has gone back, and both of those are
// decisions, so neither may read a replica that is a moment behind. A hold
// expired from a stale read would take a seat away from a parent who is still
// on the payment screen.
//
// Param:
// pools - *database.Pools (the primary, and only the primary)
// logger - *observability.Logger (where a settled refund is written down)
// counter - booking.TransactionCounter (where each seat transaction is recorded)
//
// Return:
//   - a registry covering every kind this service runs
//   - an error naming the handler that could not be built or registered
func buildHandlers(pools *database.Pools, logger *observability.Logger, counter booking.TransactionCounter) (worker.Registry, error) {
    seats := booking.NewPostgresRepository(pools.Primary())

    // The expiry transaction ends here rather than in the api, so without this
    // the panel would show every seat transaction but that one.
    seats.CountTransactions(counter)

    bookings, err := booking.NewService(seats, booking.DefaultSettings())
    if err != nil {
        return nil, fmt.Errorf("the booking service: %w", err)
    }

    payments, err := payment.NewService(
        payment.NewPostgresRepository(pools.Primary()),
        payment.NewMockProvider(),
        payment.Settings{})
    if err != nil {
        return nil, fmt.Errorf("the payment service: %w", err)
    }

    expireHold, err := worker.NewExpireHoldHandler(bookings)
    if err != nil {
        return nil, fmt.Errorf("the expiry handler: %w", err)
    }

    reconcile, err := worker.NewReconcileRefundHandler(bookings, payments, refundRecorder(logger))
    if err != nil {
        return nil, fmt.Errorf("the reconciliation handler: %w", err)
    }

    handlers := worker.Registry{}

    if err := handlers.Register(queue.KindExpireHold, expireHold); err != nil {
        return nil, fmt.Errorf("registering the expiry handler: %w", err)
    }

    if err := handlers.Register(queue.KindReconcileRefund, reconcile); err != nil {
        return nil, fmt.Errorf("registering the reconciliation handler: %w", err)
    }

    return handlers, nil
}
