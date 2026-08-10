# Backend Phase Track

Go api and worker for the trial booking service. This file tracks build progress
for this stack only. A box is ticked when the work is done and its tests pass.
Design reasoning lives in `ADR.md`, `HLD.md`, and `LLD.md`.

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

- [ ] `internal/payment/provider.go` interface
- [ ] `internal/payment/provider_mock.go` with deterministic outcomes
- [ ] `internal/payment/attempt.go`
- [ ] edge tests: zero and negative amounts, malformed idempotency key
- [ ] simulation 2: payment failure never reaches the roster
- [ ] simulation 9: idempotent payment replay

## Phase 4: queue and worker

- [ ] `internal/queue/queue.go` interface, `queue_postgres.go`, `queue_memory.go`
- [ ] `cmd/worker/main.go` with its metrics listener on 9002
- [ ] simulation 7: hold expiry by the worker
- [ ] simulation 8: refund reconciliation
- [ ] test: two workers in parallel never claim the same job, proof tier

## Phase 5: authentication

- [ ] `internal/auth/jwt.go`, sign and verify, claims limited to the agreed set
- [ ] `internal/auth/store.go` interface plus both implementations
- [ ] `internal/auth/refresh.go`, rotation and family revocation
- [ ] `internal/auth/middleware.go`, verification, denylist, role check, Origin check on mutations
- [ ] auth endpoints: login, refresh, logout, me
- [ ] unit and edge tests: tampered signature, wrong algorithm, expired token, missing jti
- [ ] edge test: the encoded payload carries no email and no name
- [ ] simulation 13: refresh rotation and reuse detection

## Phase 6: http surface

- [ ] `internal/httpx/router.go`, `middleware.go`, `errors.go`
- [ ] `internal/operations/health.go` and `version.go`
- [ ] `internal/ratelimit` interface plus both implementations
- [ ] `internal/cache` etag builder, store interface, both implementations
- [ ] `internal/roster/service.go`
- [ ] admin queue and admin bookings endpoints, role gated
- [ ] `cmd/api/main.go` on port 9000
- [ ] unit tests: every typed error maps to its status and code, etag builder
- [ ] simulation 10: bot prevention layers
- [ ] simulation 11: conditional request served without a database read
- [ ] simulation 12: readiness reflects reality
- [ ] test: read routing sends deciding reads to the primary, proof tier

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
