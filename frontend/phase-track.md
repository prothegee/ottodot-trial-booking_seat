# Frontend Phase Track

SvelteKit client for the trial booking service. This file tracks build progress
for this stack only. A box is ticked when the work is done and its tests pass.
Design reasoning lives in `ADR.md`, `HLD.md`, and `LLD.md`.

Phases run in order and each closes before the next opens. Work lands as it
finishes, a branch and a pull request per phase and a commit per file, so the
list below reads in the same order as the history.

## Phase 1: scaffold

- [x] SvelteKit project with TypeScript, `ssr = false`
- [x] `vite.config.ts` with the api base url and build identity from environment variables
- [x] dev server and preview both on 9001
- [x] `+layout.svelte` shell with `VersionFooter.svelte`
- [x] `containers/Containerfile` building static assets
- [x] `containers/caddy/Caddyfile` serving on 9001
- [x] `compose.yml`, this stack alone, starting without the other one
- [x] `scripts/dev.sh`, `scripts/test.sh`, `scripts/build.sh`
- [x] `scripts/clean.sh`, sourcing the root `scripts/lib/confirm.sh`, prompts before removing anything
- [x] test: the app boots and renders the shell, footer shows the injected version
- [x] test: `clean.sh` declines on an empty answer and touches nothing on `--dry-run`

## Phase 2: api client and auth

- [x] `lib/api/types.ts`
- [x] `lib/api/transport.ts`, the interface and the fetch transport
- [x] `lib/api/transport_fake.ts`, the fake used by every test
- [x] `lib/api/errors.ts`
- [x] `lib/api/refresh.ts`, single flight and one retry
- [x] `lib/api/client.ts` with `credentials: "include"`
- [x] `lib/stores/auth.ts`, memory only
- [x] `lib/session/sign_out.ts`, what a hard sign out does
- [x] `lib/session/client.ts`, the one wired client the application uses
- [x] `/sign-in` route calling `POST /api/v1/auth/login` then `GET /api/v1/auth/me`
- [x] unit and edge tests: error mapping, unknown code fallback, single flight, one retry
- [x] edge test: the auth store holds no email and no token
- [x] simulation F9: silent refresh, single flight
- [x] simulation F10: hard sign out on a reused token

## Phase 3: internal cache

- [x] `lib/cache/policy.ts`
- [x] `lib/cache/store.ts` with `sessionStorage` backing
- [x] `If-None-Match` and 304 handling in the api client
- [x] invalidation hooks on every mutation and on sign out
- [x] unit and edge tests: freshness boundaries, key collisions, invalidation, clear on sign out
- [x] simulation F12: fresh cache sends no request at all
- [x] simulation F13: stale cache revalidates to 304
- [x] simulation F14: mutation invalidates the cache

The store was split further as it was built: `key.ts` for what may be held,
`session_mirror.ts` for the copy that survives a reload, `read_through.ts` for
the read path, and `mutation.ts` for the write path that owns its invalidation.

## Phase 4: booking flow

- [x] `/` class list with `ClassCard.svelte`
- [x] `lib/stores/classes.ts`
- [x] `/book/[classId]` with `ChildPicker.svelte`
- [x] `lib/stores/booking.ts`
- [x] edge test: zero seats renders as full
- [x] simulation F1: happy path booking
- [x] simulation F2: stale seat count, class full at hold time
- [x] simulation F4: duplicate booking

Landed alongside them, because none of the three simulations can be written
without either: `lib/api/idempotency.ts`, which mints one key per attempt, and
the wire types for a class and a booking, which are the contract phase 6 of the
backend has to serve.

## Phase 5: payment and status

- [x] `/pay/[bookingId]` with `PaymentForm.svelte`
- [x] `HoldCountdown.svelte`
- [x] `/booking/[bookingId]` with `BookingStatus.svelte`
- [x] unit and edge tests: countdown maths including a past deadline
- [x] `internal_error` handling: retry offered, same idempotency key resent, request id rendered
- [x] edge test: a decline earns a fresh idempotency key, an `internal_error` reuses the original
- [x] simulation F3: payment declined
- [x] simulation F5: seat lost after paying
- [x] simulation F6: hold countdown reaching zero
- [x] simulation F8: double submit guard
- [x] simulation F17: the backend transaction breaks mid-payment

Landed alongside them, because none of the screens work without either:
`lib/booking/countdown.ts`, the countdown as arithmetic rather than as state in
a component, and `lib/booking/price.ts`, which holds what a trial costs, in
cents, matching the backend's fixed currency.

Two changes to what phase 4 built. The booking store gained `load`, which reads
a booking straight through the api client rather than through the cache, because
a status is what decides, ADR-F024. And `startNewAttempt` is gone: the store now
decides for itself whether a refusal ends the attempt, so no screen can forget
to mint a key or mint one it should not have, ADR-F021.

## Phase 6: bot prevention cooperation

- [ ] honeypot field and fill timer in `PaymentForm.svelte`
- [ ] mock captcha widget
- [ ] idempotency key lifecycle in the api client
- [ ] edge test: a new attempt after a decline gets a fresh key
- [ ] simulation F7: honeypot and fill timer

## Phase 7: roster, status, telemetry

- [ ] `/roster/[classId]`, hidden from a parent role
- [ ] `/status` with `ReadinessDot.svelte` and `lib/stores/status.ts`
- [ ] `lib/telemetry/emitter.ts` with batching and silent failure
- [ ] `backend/containers/grafana/dashboards/frontend.json` panels agreed with the backend metric names
- [ ] the fault code row on that dashboard: `internal_error` and `dependency_unavailable` split from the auth and booking groups
- [ ] edge test: a failed telemetry post never surfaces to the parent
- [ ] simulation F11: roster view
- [ ] simulation F15: status route reflects backend readiness
- [ ] simulation F16: nothing sensitive is held by the client

## Phase 8: frontend documentation

- [x] `README.md` with both frontend badges
- [x] `ADR.md`
- [x] `HLD.md`
- [x] `LLD.md`
- [x] `how-to.md`
- [x] `phase-track.md`, the phases from the plan copied, brief at the top, this stack only

Written as base documents after phase 2, covering what exists and marking what
does not. Revisited as each remaining phase lands.

## Phase 9: visual polish

Only if time remains after everything above is green.

- [ ] layout and spacing pass
- [ ] readable states for every error case
- [ ] responsive check at a phone width
