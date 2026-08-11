package main

import (
    "fmt"

    "ottodot-trial-booking/backend/internal/catalogue"
    "ottodot-trial-booking/backend/internal/roster"
)

// advisory is the half of the service that only reports: the class list with its
// seat counts, and the roster for one class.
//
// The name is the point, and so is the fact that this is a separate file from
// the deciding half. These are the only two readers in the whole api pointed at
// the replica, and they may be a second behind because neither of them decides
// anything. A stale seat count costs a parent one wasted click, which every
// screen is built to handle, and a stale roster is a list somebody is reading a
// few minutes before a class starts.
type advisory struct {
    classes *catalogue.Service
    rosters *roster.Service
}

// buildReads wires the two advisory readers.
//
// Param:
// deps - *dependencies (the replica pool, and only the replica pool)
//
// Return:
//   - the two readers
//   - an error naming the one that could not be built
func buildReads(deps *dependencies) (advisory, error) {
    classes, err := catalogue.NewService(catalogue.NewPostgresReader(deps.pools.Replica()))
    if err != nil {
        return advisory{}, fmt.Errorf("the catalogue: %w", err)
    }

    rosters, err := roster.NewService(roster.NewPostgresReader(deps.pools.Replica()))
    if err != nil {
        return advisory{}, fmt.Errorf("the roster: %w", err)
    }

    return advisory{classes: classes, rosters: rosters}, nil
}
