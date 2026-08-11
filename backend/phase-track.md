# Backend Phase Track

Go api and worker for the trial booking service. This file tracks build progress
for this stack only. A box is ticked when the work is done and its tests pass.
Design reasoning lives in `ADR.md`, `HLD.md`, and `LLD.md`.

Phases run in order and each closes before the next opens. Work lands as it
finishes, a branch and a pull request per phase and a commit per file, so the
list below reads in the same order as the history.

## Phase 1: foundation

- [x] `0001_schema.sql` with enums, tables, and the four unique indexes
- [x] `0002_seed.sql` covering every seed case, fake names and `example.test` addresses
- [x] `internal/config/config.go`, every port and secret from configuration
- [x] `internal/database/connection.go` with separate primary and replica pools
- [x] `containers/postgresql/primary` config and init script
- [x] `containers/postgresql/replica` init script
- [x] `containers/redis/redis.conf`
- [x] `containers/Containerfile.api` and `Containerfile.worker`
- [x] `compose.yml` on the agreed ports, bind mounting `.data/` per container
- [x] `scripts/lib/confirm.sh` at the root, the guard and manifest and prompt
- [x] root `scripts/stack_up.sh` and `scripts/stack_down.sh`
- [x] `scripts/migrate.sh` and `scripts/seed.sh`
- [x] `scripts/db_reset.sh`, destructive, prompts, dev guarded
- [x] test: migrations apply cleanly and every index exists
- [x] test: `confirm.sh` refuses without `APP_ENV=development`, declines on an empty answer, exits 2 on a pipe without `--yes`, and touches nothing on `--dry-run`

## Phase 2: booking core

- [x] `internal/identifier/uuidv7.go`, the identifier every table's primary key comes from
- [x] `internal/booking/model.go`, the shapes this domain moves
- [x] `internal/booking/status.go`, allowed transition table
- [x] `internal/booking/seat.go`, the lowest free seat, mirroring the sql
- [x] `internal/booking/hold.go`, the deadline boundary and `capacity + hold_allowance`
- [x] `internal/booking/errors.go`, the typed failures the http layer will map
- [x] `internal/booking/repository.go`, the interface and its request shapes
- [x] `internal/booking/repository_postgres.go`, the confirm transaction
- [x] `internal/booking/repository_memory.go`, the fake
- [x] `internal/booking/service.go`, hold and confirm and cancel
- [x] unit and edge tests: transitions, seat picker, hold deadline, allowance, capacity
- [x] contract suite pointable at either repository
- [x] simulation 1: duplicate booking rejected
- [x] simulation 3: capacity boundary at 3 confirmed
- [x] simulation 4: parallel confirms on one free seat, proof tier
- [x] simulation 5: parallel confirms on an empty 4-seat class, proof tier

## Phase 3: payment

- [x] `internal/payment/provider.go` interface
- [x] `internal/payment/provider_mock.go` with deterministic outcomes
- [x] `internal/payment/attempt.go`
- [x] `internal/payment/amount.go`, the money rules, and `idempotency.go`, the key rules
- [x] `internal/payment/errors.go`, the typed failures the http layer will map
- [x] `internal/payment/repository.go`, `repository_postgres.go`, `repository_memory.go`
- [x] `internal/payment/service.go`, validate then open then charge then settle
- [x] edge tests: zero and negative amounts, malformed idempotency key
- [x] contract suite pointable at either repository
- [x] simulation 2: payment failure never reaches the roster
- [x] simulation 9: idempotent payment replay
- [x] test: parallel calls with one key produce one row, proof tier

The repository was not in the plan's list for this phase. It is here because
simulations 2 and 9 assert persisted state, one `payment_attempts` row and the
`uq_payment_idempotency` index holding, and neither can be shown without a
storage seam. It follows the same shape the booking package uses: an interface,
a fake, a Postgres implementation, and one contract suite pointed at both.

## Phase 4: queue and worker

- [x] `internal/queue/queue.go` interface, `queue_postgres.go`, `queue_memory.go`
- [x] `cmd/worker/main.go` with its metrics listener on 9002
- [x] simulation 7: hold expiry by the worker
- [x] simulation 8: refund reconciliation
- [x] test: two workers in parallel never claim the same job, proof tier

Landed alongside them, because the two simulations cannot be written without
either: `booking.Expire` and its `ErrHoldStillLive` guard, `payment.Refund` with
the idempotency key that stops a retried job refunding twice, `internal/worker`
holding the runner and both handlers, and the worker service in `compose.yml`.

## Phase 5: authentication

- [x] `internal/auth/jwt.go`, sign and verify, claims limited to the agreed set
- [x] `internal/auth/store.go` interface plus both implementations
- [x] `internal/auth/refresh.go`, rotation and family revocation
- [x] `internal/auth/middleware.go`, verification, denylist, role check, Origin check on mutations
- [x] auth endpoints: login, refresh, logout, me
- [x] unit and edge tests: tampered signature, wrong algorithm, expired token, missing jti
- [x] edge test: the encoded payload carries no email and no name
- [x] simulation 13: refresh rotation and reuse detection
- [x] test: one refresh token spent once under real parallelism, proof tier

Landed alongside them, because none of the above works without them:
`claims.go` and `token.go`, which are the two token shapes the plan folded into
`jwt.go`. `directory.go` with both implementations, because login needs to look
a parent up by email and the session read needs their children, and neither is
the refresh token lifecycle. `denylist.go` with a per-process implementation,
because the middleware has to check one and Redis does not arrive until phase 6,
recorded in ADR-031. `cookie.go`, `handler.go`, and `failure.go`, because the
endpoints are in this phase and the router is not.

Two decisions were taken here rather than in planning, and both are written up.
The refresh cookie is scoped to the auth group rather than to the refresh route,
because sign out cannot revoke a token it never receives, ADR-030. An unknown
email answers with the generic refusal rather than a code of its own, so the
endpoint cannot be used to find out who has an account, ADR-032.

## Phase 6: http surface

- [x] `internal/httpx/router.go`, `middleware.go`, `errors.go`
- [x] `internal/operations/health.go` and `version.go`
- [x] `internal/ratelimit` interface plus both implementations
- [x] `internal/cache` etag builder, store interface, both implementations
- [x] `internal/roster/service.go`
- [x] admin queue and admin bookings endpoints, role gated
- [x] `cmd/api/main.go` on port 9000
- [x] unit tests: every typed error maps to its status and code, etag builder
- [x] simulation 10: bot prevention layers
- [x] simulation 11: conditional request served without a database read
- [x] simulation 12: readiness reflects reality
- [x] test: read routing sends deciding reads to the primary, proof tier

Landed alongside them, because the routes above cannot be served without them:

`internal/catalogue`, the advisory read side. The class list and its seat counts
read the replica and are safe to cache, and the booking repository is bound to
the primary because it decides. One package cannot be both.

`internal/checkout`, where the seat, the money, and the queue meet. Booking knows
nothing about money, payment knows nothing about seats, and the queue knows
nothing about either, which left the order of a checkout owned by nobody. It is
a package rather than a handler method so the sequence can be tested without an
http request anywhere near it.

`internal/captcha`, the interface and a deterministic mock, recorded in ADR-039.
`internal/operations/readiness.go`, split from `health.go` because liveness and
readiness answer opposite questions. `auth/denylist_redis.go`, which closes the
gap ADR-031 wrote down.

Three additions to what phase 2 built, each with a case in the shared contract
suite: `Repository.Fail`, so a declined payment can reach `payment_failed` at
all, ADR-034. `Repository.Worklist`, which the admin screen reads.
`Repository.LiveBooking`, so a refused duplicate can name the booking the parent
already has instead of sending them looking for it.

Two decisions were taken here rather than in planning, and both are written up.
The client still sends the amount and the service refuses anything but its own
price, ADR-033. A not-found and a not-yours give the same answer, so the api
cannot be asked which identifiers exist, ADR-038.

## Phase 7: monitoring and data hygiene

- [ ] `internal/observability/metrics.go` with every metric in the table
- [ ] `internal/observability/logger.go`, request id and booking id on every state change
- [ ] `internal/observability/redact.go`, cookie and authorization scrubbing at the writer
- [ ] `internal/observability/telemetry.go`, frontend events into metrics
- [ ] access failure metrics: `access_denied_total`, `rate_limit_rejected_total`, `bot_check_rejected_total`
- [ ] transaction failure metrics: `db_transaction_total`, `confirm_transaction_total`, `payment_attempt_total`, `queue_job_failed_total`
- [ ] node_exporter and cadvisor in `compose.yml`, with the podman socket note in `how-to.md`
- [ ] `internal/faults/points.go`, `registry.go`, and `handler.go`, guarded and off by default
- [ ] fault call sites: confirm before commit, confirm lock wait, mock provider error, worker job error, Redis boundary
- [ ] `fault_injection_enabled` gauge and `fault_injected_total` counter
- [ ] `containers/prometheus/prometheus.yml` scraping 9000, 9002, 9005, 9006 every 5 seconds
- [ ] `containers/prometheus/rules.yml` with all twelve alerts
- [ ] `containers/prometheus/rules_test.yml`, promtool proof that `TransactionErrorSpike` and `RefundBacklog` fire
- [ ] `containers/grafana/provisioning` datasource and dashboard loaders
- [ ] `containers/grafana/dashboards/backend.json`, fault banner row, then resources, then failures
- [ ] `containers/grafana/dashboards/resources.json`, the host-wide fallback view
- [ ] test: `/metrics` exposes every named metric and no label holds a uuid
- [ ] test: cpu, memory, and drive panels resolve from layer 1 and layer 2 alone
- [ ] test: the transaction failure panel queries `confirm_transaction_total{outcome="error"}` by exact name
- [ ] unit and edge: fault registry counting, ttl, unknown point, and all four guards
- [ ] simulation 14: nothing sensitive leaks
- [ ] simulation 15: the core transaction fails and leaves nothing behind

## Phase 8: backend documentation

- [x] `README.md` with both backend badges
- [x] `ADR.md`
- [x] `HLD.md`
- [x] `LLD.md`
- [x] `how-to.md`
- [x] `phase-track.md`, the phases from the plan copied, brief at the top, this stack only

Written as base documents after phase 2, covering what exists and marking what
does not. Revisited as each remaining phase lands.

## Phase 9: repository wide

Tracked at the root, not here. This stack contributes `scripts/test.sh` and
`scripts/test_proof.sh`.

- [ ] `scripts/test.sh`, the four fake tiers
- [ ] `scripts/test_proof.sh`, containers tag, real database
