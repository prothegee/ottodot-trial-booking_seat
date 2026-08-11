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
history reads as small reviewable steps rather than one bulk drop. Phases 1 to 6
are done. Progress is tracked in `phase-track.md`.

| exists | not yet |
| :- | :- |
| the schema, its four unique indexes, and seed data | monitoring and fault injection, phase 7 |
| configuration, with secrets that mask themselves | the continuous integration workflows, phase 9 |
| separate primary and replica connection pools | a real payment provider, out of scope by design |
| the booking core: hold, confirm, cancel, fail, expire, the audit trail | a real captcha provider, same reason |
| the payment path: a deterministic provider, one charge per idempotency key | a password, cut on purpose, see the note below |
| the job queue, and the worker that drains it on 9002 | |
| authentication: HS256 access tokens, rotating refresh tokens, the four auth routes | |
| the whole http surface on 9000, including the operator and roster routes | |
| ETag caching, a Redis token bucket, and the cooperative bot checks | |
| the last-seat race proven against real Postgres | |
| two workers proven never to claim one job | |
| one refresh token proven to be spendable exactly once | |
| read routing proven against a real primary and a real standby | |

Sign in takes a seeded email and no password. That is the brief's cut rather
than an oversight: everything around it is real, so a password or a provider
later replaces one method.

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
|___/cmd
|   |___/api
|   |   |___main.go                        (the http surface on 9000, the wiring and nothing else)
|   |
|   |___/worker
|       |___main.go                        (the queue consumer, metrics on 9002)
|
|___/internal
|   |___/auth                              (tokens, rotation, and who may act)
|   |___/booking                           (the seat, and everything that decides it)
|   |___/cache                             (the etag, the stored body, the version counter)
|   |___/captcha                           (a challenge provider's shape, and a mock)
|   |___/catalogue                         (the class list, advisory, from the replica)
|   |___/checkout                          (the order a hold and a payment happen in)
|   |___/config                            (every port and secret, from the environment)
|   |___/database                          (primary and replica pools, no queries)
|   |___/httpx                             (the routes, the chain, and the one envelope)
|   |___/identifier                        (the only place an id is minted)
|   |___/operations                        (liveness, readiness, build identity)
|   |___/payment                           (the charge, and nothing about seats)
|   |___/queue                             (jobs, leases, and attempts. No domain at all)
|   |___/ratelimit                         (the token bucket, and two implementations)
|   |___/roster                            (who owns a seat, admin only)
|   |___/worker                            (where the queue meets the domain)
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
|   |___format.sh                          (gofmt, then leading tabs to four spaces)
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

Every service in `compose.yml` can start today. A service joins the file in the
phase that builds it, so it never references something that does not exist.

<br>

## Ports

| port | what | state |
| :- | :- | :- |
| 5432 | postgres primary | running |
| 5433 | postgres replica | running |
| 6379 | redis | running |
| 9000 | api | running |
| 9002 | worker metrics | running |

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

go run ./cmd/api                  # the http surface on 9000

go test ./...                     # four fast tiers, nothing needs to run
go test -tags=containers ./...    # the real database and Redis proofs
```

Full detail, including what to do when something refuses to start, is in
`how-to.md`.

<br>

## The Shape Of It

Four ideas carry most of the design.

**The database holds the invariants.** Four partial unique indexes make the
rules impossible to violate, even from a manual sql edit or a buggy code path.
The application enforces them too, so a parent gets a readable reason rather
than a driver error, but the database is what makes them true.

**One transaction decides the seat.** The confirm transaction locks the class
row, counts the seats taken under that lock, and takes the lowest free one.
Everything else in the system that reports a seat count is advisory by
construction.

**A token is cheap to believe and expensive to forge.** Verifying an access
token is a signature check and a clock comparison, so the request path touches
no database. The refresh token is the opposite: opaque, stored only as a hash,
rotated on every use, so presenting a spent one is how a stolen token becomes
visible. Both live in HttpOnly cookies, which is why no code in the client ever
reads one.

**Nothing that decides anything is cached.** Only the class list and one class
carry an ETag, and both are advisory by construction, so a stale copy costs a
parent a wasted click and can never cost anybody a seat. A repeat request costs
one Redis read and zero database queries, and a test counts the reader's calls
to prove it.

**Fakes are fast, and they are not trusted alone.** The four fast tiers run
against an in-memory repository in about a second. The same behaviour suite runs
against real Postgres behind a build tag, because a fake cannot prove that a
lock serialized anything, and a mutex will make a fake pass a concurrency test
for entirely the wrong reason.

<br>

## Documents

| file | contents |
| :- | :- |
| `ADR.md` | every decision, with what was rejected and what it cost |
| `HLD.md` | component boundaries, the request and job flow, read routing |
| `LLD.md` | the schema table by table, the confirm transaction step by step, the state machine |
| `how-to.md` | run, migrate, seed, test, and what to do when something refuses |
| `phase-track.md` | the build checklist, ticked as tests pass |
