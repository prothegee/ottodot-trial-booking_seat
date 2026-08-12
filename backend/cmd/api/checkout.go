package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/checkout"
    "ottodot-trial-booking/backend/internal/payment"
    "ottodot-trial-booking/backend/internal/queue"
)

// deciding is the half of the service that decides things: the seat, the money,
// and the work that has to survive a restart.
//
// The name is the point. Everything in here reads and writes the primary pool,
// and the reason is the same for all three: a seat is decided, a charge is
// written, and a job has to be found again after a crash. None of those may read
// a replica that is a moment behind.
type deciding struct {
    checkout *checkout.Service
    bookings *booking.Service
    provider *payment.MockProvider
    jobs     queue.Queue
    seats    *booking.PostgresRepository
}

// buildCheckout wires the seat, the money, and the queue into the order a
// checkout happens in.
//
// Param:
// deps - *dependencies (the primary pool)
// watch - bootstrap.Observability (where confirms and charges are counted)
//
// Return:
//   - the assembled half, including the two pieces the fault surface reaches
//     into and the repository the admin worklist reads through
//   - an error naming the piece that could not be built
func buildCheckout(deps *dependencies, watch bootstrap.Observability) (deciding, error) {
    seats := booking.NewPostgresRepository(deps.pools.Primary())

    // Every seat transaction is counted where it ends, so the panel reads
    // commits against rollbacks rather than only the confirms the service layer
    // times.
    seats.CountTransactions(watch.Metrics)

    // Declared once here rather than in both processes, because the panel sums
    // over them and the api is the one always running. Without it the panel has
    // nothing at all until the first booking of the day.
    watch.Metrics.Transaction.DeclareTransactionNames(booking.TransactionNames())

    bookings, err := booking.NewService(seats, booking.DefaultSettings())
    if err != nil {
        return deciding{}, fmt.Errorf("the booking service: %w", err)
    }

    // The mock provider is held rather than passed straight through, because the
    // fault surface arms it by name. A real provider would be built here instead
    // and would have no equivalent, which is honest: nobody can arm a failure
    // inside somebody else's payment service.
    provider := payment.NewMockProvider()

    payments, err := payment.NewService(
        payment.NewPostgresRepository(deps.pools.Primary()),
        provider,
        payment.Settings{})
    if err != nil {
        return deciding{}, fmt.Errorf("the payment service: %w", err)
    }

    jobs := queue.NewPostgresQueue(deps.pools.Primary())

    checkoutSettings := checkout.DefaultSettings()
    checkoutSettings.Metrics = watch.Metrics

    service, err := checkout.NewService(bookings, payments, jobs, checkoutSettings)
    if err != nil {
        return deciding{}, fmt.Errorf("the checkout service: %w", err)
    }

    return deciding{
        checkout: service,
        bookings: bookings,
        provider: provider,
        jobs:     jobs,
        seats:    seats,
    }, nil
}
