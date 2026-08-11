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
    handlers, err := buildHandlers(pools, watch.Logger)
    if err != nil {
        return nil, err
    }

    runnerSettings := worker.DefaultSettings()
    runnerSettings.PollInterval = settings.Worker.PollInterval
    runnerSettings.Metrics = watch.Metrics
    runnerSettings.OnError = func(err error) {
        // A failed job is not a failed worker. It reaches here, gets counted,
        // and is tried again, so this is an error line about one job rather than
        // about the process.
        watch.Logger.Error("a job did not finish", observability.FieldReason, err.Error())
    }

    return worker.NewRunner(queue.NewPostgresQueue(pools.Primary()), handlers, runnerSettings)
}
