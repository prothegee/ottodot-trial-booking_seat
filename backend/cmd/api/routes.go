package main

import (
    "fmt"
    "net/http"

    "ottodot-trial-booking/backend/internal/booking"
    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/httpx"
    "ottodot-trial-booking/backend/internal/observability"
)

// buildSurface assembles the whole api out of the halves the other files built.
//
// It reads as an order rather than a list, and the order matters in one place:
// the guards need the booking service, so the deciding half is built before
// them, and the fault surface needs both, so it is built last.
//
// Param:
// deps - *dependencies (the open stores)
// watch - bootstrap.Observability (the registry, the metrics, and the logger)
// settings - config.Config (everything the wiring reads)
//
// Return:
//   - the handler, ready to serve
//   - the booking service, which the gauge sampler reads the refund backlog from
//   - an error naming the piece that could not be built
func buildSurface(deps *dependencies, watch bootstrap.Observability, settings config.Config) (http.Handler, *booking.Service, error) {
    signedIn, err := buildSession(deps, watch, settings)
    if err != nil {
        return nil, nil, err
    }

    decided, err := buildCheckout(deps, watch)
    if err != nil {
        return nil, nil, err
    }

    reads, err := buildReads(deps)
    if err != nil {
        return nil, nil, err
    }

    guarded, err := buildGuards(deps, watch, signedIn, decided)
    if err != nil {
        return nil, nil, err
    }

    operationsHandler, err := buildOperations(deps)
    if err != nil {
        return nil, nil, err
    }

    handlers, err := buildHandlers(signedIn, decided, reads, guarded, watch, settings)
    if err != nil {
        return nil, nil, err
    }

    handlers.Operations = operationsHandler
    handlers.Guard = signedIn.guard
    handlers.Limits = guarded.limits
    handlers.Counters = guarded.counters
    handlers.Exposition = watch.Exposition
    handlers.Faults = buildFaults(settings, watch, decided, guarded)
    handlers.Recovery = func(requestID string, err error) {
        watch.Logger.Error("a handler panicked",
            observability.FieldRequestID, requestID,
            observability.FieldReason, err.Error())
    }

    router, err := httpx.NewRouter(handlers)
    if err != nil {
        return nil, nil, err
    }

    return router, decided.bookings, nil
}

// buildHandlers builds one handler per group of routes.
//
// It is separate from the surface above because it is a different question. That
// one is about the order things are assembled in, and this one is about which
// collaborator each route reads through, which is where a wiring mistake would
// actually be: handing the roster the catalogue, or the payment route the wrong
// price flag.
func buildHandlers(
    signedIn session,
    decided deciding,
    reads advisory,
    guarded guards,
    watch bootstrap.Observability,
    settings config.Config,
) (httpx.Routes, error) {
    classHandler, err := httpx.NewClassHandler(reads.classes, guarded.conditional)
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the class routes: %w", err)
    }

    studentHandler, err := httpx.NewStudentHandler(signedIn.directory)
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the student route: %w", err)
    }

    bookingHandler, err := httpx.NewBookingHandler(decided.checkout, decided.bookings, guarded.owner, guarded.botCheck, guarded.conditional)
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the booking routes: %w", err)
    }

    paymentHandler, err := httpx.NewPaymentHandler(decided.checkout, guarded.owner, guarded.botCheck, guarded.conditional, settings.IsDevelopment())
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the payment route: %w", err)
    }

    rosterHandler, err := httpx.NewRosterHandler(reads.rosters)
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the roster route: %w", err)
    }

    adminHandler, err := httpx.NewAdminHandler(decided.bookings, decided.jobs, nil)
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the admin routes: %w", err)
    }

    telemetryHandler, err := httpx.NewTelemetryHandler(observability.NewTelemetry(watch.Metrics.Frontend))
    if err != nil {
        return httpx.Routes{}, fmt.Errorf("the telemetry route: %w", err)
    }

    return httpx.Routes{
        Auth:      signedIn.handler,
        Classes:   classHandler,
        Students:  studentHandler,
        Bookings:  bookingHandler,
        Payments:  paymentHandler,
        Roster:    rosterHandler,
        Admin:     adminHandler,
        Telemetry: telemetryHandler,
    }, nil
}
