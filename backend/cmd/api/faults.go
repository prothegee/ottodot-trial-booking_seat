package main

import (
    "log/slog"

    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/faults"
    "ottodot-trial-booking/backend/internal/observability"
)

// buildFaults arms the development only injection surface, or returns nothing at
// all.
//
// Returning nil is the off state and it is the whole design of this file. When
// the surface is off nothing is armed, nothing is injected, and the routes are
// never registered, so `/dev/faults` answers a plain not found rather than a
// refusal that would confirm there is something there to find.
//
// The configuration cannot be wrong in the dangerous direction. `config.Load`
// refuses to start the process at all when the flag is true outside development,
// so the check here is the second of two rather than the only one.
//
// Param:
// settings - config.Config (the flag and the environment)
// watch - bootstrap.Observability (where a trigger is counted and written down)
// decided - deciding (the seat repository and the payment provider to arm)
// guarded - guards (the response cache to arm)
//
// Return:
//   - the routes, when every guard passes
//   - nil, which is the ordinary state and means no call site is ever reached
func buildFaults(settings config.Config, watch bootstrap.Observability, decided deciding, guarded guards) *faults.Handler {
    // The gauge is published either way, so the series always exists. A banner
    // bound to a metric that is simply absent reads the same as one bound to a
    // metric that says zero, and the point of the banner is that nobody mistakes
    // a deliberately broken stack for a healthy one.
    watch.Metrics.Application.FaultInjectionEnabled(settings.Faults.Enabled && settings.IsDevelopment())

    if !settings.Faults.Enabled || !settings.IsDevelopment() {
        return nil
    }

    registry := faults.NewRegistry(faults.Settings{
        OnTrigger: func(point string) {
            watch.Metrics.Application.FaultInjected(point)

            // Warn rather than info. A stack with a fault firing is not one
            // anybody should read as healthy, and the level is what says so to
            // whatever is reading the log.
            watch.Logger.Warn("a fault was injected on purpose",
                slog.String(observability.FieldPoint, point))
        },
    })

    // Every call site is pointed at the same registry, so one arm reaches
    // whichever of them the request happens to run through.
    decided.seats.InjectFaults(registry.Trigger)
    decided.provider.InjectFaults(registry.Trigger)
    guarded.store.InjectFaults(registry.Trigger)

    watch.Logger.Warn("fault injection is live, this service can be broken on purpose")

    return faults.NewHandler(registry)
}
