[![backend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)
[![backend main-stable](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main-stable)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)

# Backend

The Go half of the trial booking service. It owns the seat: every rule that has
to hold while several parents are acting at once is enforced here, and the
confirm transaction on the primary is the only authority on who owns which seat.

This stack is complete on its own. `compose.yml` in this directory starts
everything it needs and references nothing outside it, so this half can be split
out or deployed separately without untangling the repository.

The badges go green once the workflows are added, which is the last phase.

<br>

## State

Phases run in order and land as each one finishes, one commit per file, so the
history reads as small reviewable steps rather than one bulk drop. Phases 1, 2,
and 3 are done. Progress is tracked in `phase-track.md`.

| exists | not yet |
| :- | :- |
| the schema, its four unique indexes, and seed data | the http api and the roster on 9000, phase 6 |
| configuration, with secrets that mask themselves | the job queue and the worker, phase 4 |
| separate primary and replica connection pools | authentication, phase 5 |
| the booking core: hold, confirm, cancel, the audit trail | monitoring and fault injection, phase 7 |
| the payment path: a deterministic provider, one charge per idempotency key | the continuous integration workflows, phase 9 |
| the last-seat race proven against real Postgres | a real payment provider, out of scope by design |

<br>

## Layout

```
backend
|
|___/containers
|   |___/postgresql
|   |   |___/primary                       (config and the init script)
|   |   |___/replica                       (the clone and follow script)
|   |
|   |___/redis
|   |___Containerfile.api
|   |___Containerfile.worker
|
|___/internal
|   |___/booking                           (the seat, and everything that decides it)
|   |___/config                            (every port and secret, from the environment)
|   |___/database                          (primary and replica pools, no queries)
|   |___/identifier                        (the only place an id is minted)
|   |___/payment                           (the charge, and nothing about seats)
|
|___/migrations
|   |___0001_schema.sql
|   |___0002_seed.sql
|
|___/scripts
|   |___/lib
|   |   |___database.sh                    (psql inside the primary container)
|   |
|   |___db_reset.sh                        (destructive, dev only, prompts)
|   |___migrate.sh
|   |___seed.sh
|
|___ADR.md
|___HLD.md
|___LLD.md
|___README.md
|___compose.yml
|___go.mod
|___how-to.md
|___phase-track.md
```

`cmd/api` and `cmd/worker` join in the phases that build them, and their compose
services join with them. `compose.yml` never references a service that cannot
start.

<br>

## Ports

| port | what | state |
| :- | :- | :- |
| 5432 | postgres primary | running |
| 5433 | postgres replica | running |
| 6379 | redis | running |
| 9000 | api | phase 6 |
| 9002 | worker metrics | phase 4 |

Containers keep their own default ports, and a second instance takes the next
number up, which is why the replica is on 5433. Every port is a configuration
value, never a literal in code.

<br>

## Quick Start

```sh
export APP_ENV=development

../scripts/stack_up.sh backend
scripts/migrate.sh
scripts/seed.sh

go test ./...                     # four fast tiers, nothing needs to run
go test -tags=containers ./...    # the real database proof
```

Full detail, including what to do when something refuses to start, is in
`how-to.md`.

<br>

## The Shape Of It

Three ideas carry most of the design.

**The database holds the invariants.** Four partial unique indexes make the
rules impossible to violate, even from a manual sql edit or a buggy code path.
The application enforces them too, so a parent gets a readable reason rather
than a driver error, but the database is what makes them true.

**One transaction decides the seat.** The confirm transaction locks the class
row, counts the seats taken under that lock, and takes the lowest free one.
Everything else in the system that reports a seat count is advisory by
construction.

**Fakes are fast, and they are not trusted alone.** The four fast tiers run
against an in-memory repository in about a second. The same behaviour suite runs
against real Postgres behind a build tag, because a fake cannot prove that a
lock serialized anything.

<br>

## Documents

| file | contents |
| :- | :- |
| `ADR.md` | every decision, with what was rejected and what it cost |
| `HLD.md` | component boundaries, the request and job flow, read routing |
| `LLD.md` | the schema table by table, the confirm transaction step by step, the state machine |
| `how-to.md` | run, migrate, seed, test, and what to do when something refuses |
| `phase-track.md` | the build checklist, ticked as tests pass |
