[![backend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)

# Backend

The Go half of the trial booking service. It owns the seat: every rule that has
to hold while several parents are acting at once is enforced here, and the
confirm transaction on the primary is the only authority on who owns which seat.

This stack is complete on its own. `compose.yml` in this directory starts
everything it needs and references nothing outside it, so this half can be split
out or deployed separately without untangling the repository.

The badge reports `pull-request-backend.yml` on `main`. It runs the formatter
check, the four fake tiers, the shell suites for the root and backend scripts,
the alert rules through `promtool`, and the proof tier and test 16 against real
containers.

<br>

## State

Phases run in order and land as each one finishes, one commit per file, so the
history reads as small reviewable steps rather than one bulk drop. Every phase is
done. Progress is tracked in `phase-track.md`.

| exists | not yet |
| :- | :- |
| the schema, its four unique indexes, and seed data | a real payment provider, out of scope by design |
| configuration, with secrets that mask themselves | a real captcha provider, same reason |
| separate primary and replica connection pools | a password, cut on purpose, see the note below |
| the booking core: hold, confirm, cancel, fail, expire, the audit trail | |
| the payment path: a deterministic provider, one charge per idempotency key | |
| the job queue, and the worker that drains it on 9002 | |
| authentication: HS256 access tokens, rotating refresh tokens, the four auth routes | |
| the whole http surface on 9000, including the operator and roster routes | |
| ETag caching, a Redis token bucket, and the cooperative bot checks | |
| every metric in the plan, on `/metrics`, with no identifier in any label | |
| structured logs, redacted at the writer rather than at the call site | |
| Prometheus, Grafana, node_exporter, and cAdvisor, provisioned as files | |
| thirteen alert rules, five of them proven by `promtool test rules` | |
| fault injection, guarded four ways and off by default | |
| the last-seat race proven against real Postgres | |
| two workers proven never to claim one job | |
| one refresh token proven to be spendable exactly once | |
| read routing proven against a real primary and a real standby | |
| the broken transaction followed from the api to the alert, on a live stack | |

Sign in takes an email and a password, hashed with argon2id. The four seeded
accounts share one password and it is written down in `how-to.md`, which is a
development convenience rather than how it is stored. An identity provider later
replaces one method.

<br>

## Layout

```
backend
|
|___/containers                            (one directory per service, each with its own .data)
|   |___/postgres-primary                  (config and the init script)
|   |___/postgres-replica                  (the clone and follow script)
|   |___/redis
|   |___/prometheus                        (the scrape config, the rules, and their proof)
|   |___/grafana
|   |   |___/provisioning                  (the data source and the dashboard loader)
|   |   |___/dashboards                    (backend, frontend, and the host fallback)
|   |
|   |___Containerfile.api
|   |___Containerfile.worker
|
|___/cmd
|   |___/api                               (the http surface on 9000, one file per thing assembled)
|   |   |___build.go                       (what this binary was built from, and who gets asked)
|   |   |___checkout.go                    (the seat, the money, and the queue)
|   |   |___dependencies.go                (the two stores held open for the run)
|   |   |___faults.go                      (the development only injection surface)
|   |   |___guards.go                      (the cache, the limits, and the two refusals)
|   |   |___listener.go                    (the socket, and how it is given up)
|   |   |___main.go                        (the entry point, and nothing else)
|   |   |___operations.go                  (liveness, readiness, build identity)
|   |   |___process.go                     (the order it all happens in)
|   |   |___reads.go                       (the two advisory readers, on the replica)
|   |   |___routes.go                      (the router, assembled from the above)
|   |   |___sampler.go                     (the gauges, on their own timer)
|   |   |___session.go                     (authentication)
|   |
|   |___/worker                            (the queue consumer, metrics on 9002)
|       |___build.go
|       |___handlers.go                    (the two job kinds and what runs them)
|       |___listener.go                    (the metrics socket)
|       |___main.go
|       |___process.go
|       |___refunds.go                     (where a settled refund is written down)
|       |___runner.go                      (the claim loop and its policy)
|
|___/internal
|   |___/auth                              (tokens, rotation, and who may act)
|   |___/booking                           (the seat, and everything that decides it)
|   |___/bootstrap                         (what both binaries open at startup)
|   |___/cache                             (the etag, the stored body, the version counter)
|   |___/captcha                           (a challenge provider's shape, and a mock)
|   |___/catalogue                         (the class list, advisory, from the replica)
|   |___/checkout                          (the order a hold and a payment happen in)
|   |___/config                            (every port and secret, from config.json then the environment)
|   |___/database                          (primary and replica pools, and their statistics)
|   |___/faults                            (named failures, on demand, development only)
|   |___/httpx                             (the routes, the chain, and the one envelope)
|   |___/identifier                        (the only place an id is minted)
|   |___/observability                     (the metrics, the log, and the redaction)
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
|   |___debug.sh                           (one process from source, in the foreground)
|   |___debug_test.sh                      (its guards, never the part that starts anything)
|   |___format.sh                          (gofmt, then leading tabs to four spaces)
|   |___migrate.sh
|   |___seed.sh                            (asks before creating the demo accounts)
|   |___test.sh                            (build, vet, and the four fast tiers)
|   |___test_all.sh                        (every backend test, starts a stack for the proofs)
|   |___test_proof.sh                      (the proof tier, needs the stack up)
|
|___ADR.md
|___HLD.md
|___LLD.md
|___README.md
|___compose.yml
|___config.json.template               (copied to config.json, which is never committed)
|___go.mod
|___how-to.md
|___phase-track.md
|___tests-and-diagram.md               (every test, its diagram, and the command for it)
|___work-rules.md                      (the ceiling on a delegated run, from the root)
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
| 9003 | prometheus | running |
| 9004 | grafana | running |
| 9005 | node_exporter | running |
| 9006 | cadvisor | running, and the one service allowed to fail |

Containers keep their own default ports, and a second instance takes the next
number up, which is why the replica is on 5433. Every port is a configuration
value, never a literal in code.

<br>

## Routes

Everything on 9000, by group. Operations routes are unversioned because a probe
and a scrape are hard coded by whatever runs the process. Everything else carries
`/api/v1`, so a breaking change becomes `/api/v2` rather than a silent contract
shift.

| group | paths | who may call it |
| :- | :- | :- |
| operations | `/healthz`, `/readyz`, `/version`, `/metrics` | anyone, no token |
| sign in | `/api/v1/auth/login`, `/refresh`, `/logout`, `/me` | anyone from a listed origin |
| parent reads | `/api/v1/students`, `/classes`, `/classes/{id}`, `/bookings`, `/bookings/{id}`, `/bookings/{id}/events` | a signed-in parent |
| parent writes | `/api/v1/bookings`, `/bookings/{id}`, `/bookings/{id}/payments`, `/telemetry` | the same, plus an `Origin` header |
| admin | `/api/v1/classes/{id}/roster`, `/admin/queue`, `/admin/bookings` | the admin role only |
| development | `/dev/faults` | admin, and only when fault injection is armed |

Creating a booking and paying for one also need an `Idempotency-Key`. That is
what makes a retry produce one booking and one charge rather than two. It is also
named by the cors preflight, because a browser sends only the headers that answer
permitted and a header this api reads and does not permit is a route a browser
cannot reach at all. See ADR-055.

There is no `/status` here. `/version` is what says which build is running, and
the status screen that draws it beside the client's own build is served by the
frontend on 9001.

Every route on its own line, with what guards each, is one table in `how-to.md`
under Every Route. In code they are constants in `internal/httpx/paths.go`,
`internal/auth/handler.go`, and `internal/operations/health.go`, because the
frontend names the same paths and a route that exists in one and not the other is
a screen that does nothing.

<br>

## Quick Start

```sh
export APP_ENV=development

../scripts/stack_up.sh backend
scripts/migrate.sh
scripts/seed.sh

go run -buildvcs=true ./cmd/api   # the http surface on 9000

go test ./...                     # four fast tiers, nothing needs to run
go test -tags=containers ./...    # the real database and Redis proofs
```

`scripts/debug.sh` does the first four of those in one command, brings the
monitoring up with them, and `scripts/debug.sh worker` does the same for the
worker. Each stops the containers it started when it ends, without removing any
of them, and leaves alone any that were already running.

Full detail, including what to do when something refuses to start, is in
`how-to.md`.

<br>

## Monitoring

It comes up with `../scripts/stack_up.sh backend`, because every scrape target
listed in Ports is a surface of this stack. The data source and the dashboards
are files under `containers/grafana/`, so a panel change is a diff rather than a
click somebody made once.

| open | what is there |
| :- | :- |
| `http://127.0.0.1:9004` | Grafana. Sign in as `admin` with `admin`, then the `backend`, `frontend`, and `resources` dashboards |
| `http://127.0.0.1:9003` | Prometheus, its scrape targets and its thirteen alert rules |
| `http://127.0.0.1:9000/metrics` | this api, including the events the client posts to `/api/v1/telemetry` |
| `http://127.0.0.1:9002/metrics` | the worker, on its own listener so it answers while the api is down |

Cpu, memory, and drive usage arrive in three layers: the Go collectors on the two
exposition endpoints, node_exporter for the host, cAdvisor per container.
`how-to.md` under Monitoring has the queries, the administrator sign in, what to
do when cAdvisor will not start, and what the per container panels need beyond a
cAdvisor that is running.

<br>

## Why This Stack

Every part was chosen to keep one thing true: the seat is decided in a single
transaction, and nothing else in the system is allowed to decide it. The full
reasoning for each choice, including what was rejected and what it cost, is in
`ADR.md`.

| part | choice | why |
| :- | :- | :- |
| language | Go with `net/http`, no framework | the interesting part is a database transaction, not a routing layer, so nothing on the request path is hidden behind a framework's conventions (ADR-001) |
| state | Postgres holds everything a decision reads | the seat is taken under a row lock, and a lock is only worth anything where the data itself lives (ADR-010) |
| read scale | one asynchronous replica, promoted by hand | advisory reads move off the primary without every confirm paying replication latency (ADR-012) |
| cache and limits | Redis, for cached responses, rate limit buckets, and the token denylist | none of that is a source of truth, so Redis can be down without a single invariant being at risk (ADR-010) |
| background work | a `job_queue` table claimed with `for update skip locked` | a broker would be a second store to keep honest, and rows survive a restart (ADR-011) |
| login | two cookies, a short-lived one and a long-lived one | checking the short-lived one is maths, so a signed-in request never asks the database who you are. The long-lived one is swapped for a new one every time it is used, so a stolen copy shows up the moment somebody tries it (ADR-013, ADR-027) |
| packaging | `compose.yml` in this directory, nothing at the repository root | this stack deploys on its own, and it cannot grow a quiet dependency on the client (ADR-014) |

Four direct dependencies carry all of it: `pgx` for Postgres, `go-redis` for
Redis, the Prometheus client for the metrics, and `argon2` for password hashing.
The router, the middleware, the json, and the JWT signing and verification are
standard library (ADR-027).

<br>

## The Shape Of It

Six ideas carry most of the design.

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

**A metric nobody has seen move is a decoration.** The confirm transaction can
be broken on demand, on a running stack, through a route that exists only in
development and disarms itself after one firing. That is what makes
`confirm_transaction_total{outcome="error"}` and the alert on it worth believing.
The dashboards and the rules are read by a Go test, because a metric name is a
contract with a file nobody compiles.

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
| `tests-and-diagram.md` | every test file, what it proves, its diagram, and the command that runs it alone |
| `phase-track.md` | the build checklist, ticked as tests pass |
| `work-rules.md` | the ceiling on a delegated backend run, restated from the root |
