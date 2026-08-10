# Frontend Phase Track

SvelteKit client for the trial booking service. This file tracks build progress
for this stack only. A box is ticked when the work is done and its tests pass.
Design reasoning lives in `ADR.md`, `HLD.md`, and `LLD.md`.

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

- [ ] `lib/cache/policy.ts`
- [ ] `lib/cache/store.ts` with `sessionStorage` backing
- [ ] `If-None-Match` and 304 handling in the api client
- [ ] invalidation hooks on every mutation and on sign out
- [ ] unit and edge tests: freshness boundaries, key collisions, invalidation, clear on sign out
- [ ] simulation F12: fresh cache sends no request at all
- [ ] simulation F13: stale cache revalidates to 304
- [ ] simulation F14: mutation invalidates the cache

## Phase 4: booking flow

- [ ] `/` class list with `ClassCard.svelte`
- [ ] `lib/stores/classes.ts`
- [ ] `/book/[classId]` with `ChildPicker.svelte`
- [ ] `lib/stores/booking.ts`
- [ ] edge test: zero seats renders as full
- [ ] simulation F1: happy path booking
- [ ] simulation F2: stale seat count, class full at hold time
- [ ] simulation F4: duplicate booking

## Phase 5: payment and status

- [ ] `/pay/[bookingId]` with `PaymentForm.svelte`
- [ ] `HoldCountdown.svelte`
- [ ] `/booking/[bookingId]` with `BookingStatus.svelte`
- [ ] unit and edge tests: countdown maths including a past deadline
- [ ] `internal_error` handling: retry offered, same idempotency key resent, request id rendered
- [ ] edge test: a decline earns a fresh idempotency key, an `internal_error` reuses the original
- [ ] simulation F3: payment declined
- [ ] simulation F5: seat lost after paying
- [ ] simulation F6: hold countdown reaching zero
- [ ] simulation F8: double submit guard
- [ ] simulation F17: the backend transaction breaks mid-payment

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
