package main

import (
    "ottodot-trial-booking/backend/internal/bootstrap"
    "ottodot-trial-booking/backend/internal/config"
    "ottodot-trial-booking/backend/internal/database"
    "ottodot-trial-booking/backend/internal/observability"
    "ottodot-trial-booking/backend/internal/queue"
    "ottodot-trial-booking/backend/internal/worker"
)

// buildRunner wires the queue to its handlers and the policy it runs under.
//
// Param:
// pools - *database.Pools (the primary, which is where the queue lives)
// watch - bootstrap.Observability (where jobs and failures are counted and written down)
// settings - config.Config (the poll interval)
//
// Return:
//   - the runner, ready to poll
//   - an error naming what could not be built
func buildRunner(pools *database.Pools, watch bootstrap.Observability, settings config.Config) (*worker.Runner, error) {
    handlers, err := buildHandlers(pools, watch.Logger, watch.Metrics)
    if err != nil {
        return nil, err
    }

    // The kinds are named here, where the queue is already imported, so the
    // failure panel starts at zero for each of them rather than waiting for the
    // first failure to exist at all.
    kinds := make([]string, 0, len(queue.AllKinds()))
    for _, kind := range queue.AllKinds() {
        kinds = append(kinds, string(kind))
    }

    watch.Metrics.Transaction.DeclareJobKinds(kinds)

    runnerSettings := worker.DefaultSettings()
    runnerSettings.PollInterval = settings.Worker.PollInterval
    runnerSettings.Concurrency = settings.Worker.Concurrency()
    runnerSettings.Metrics = watch.Metrics
    runnerSettings.OnError = func(err error) {
        // A failed job is not a failed worker. It reaches here, gets counted,
        // and is tried again, so this is an error line about one job rather than
        // about the process.
        watch.Logger.Error("a job did not finish", observability.FieldReason, err.Error())
    }

    return worker.NewRunner(queue.NewPostgresQueue(pools.Primary()), handlers, runnerSettings)
}
