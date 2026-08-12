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
- [x] `containers/nginx/nginx.conf` serving on 9001
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

- [x] honeypot field and fill timer in `PaymentForm.svelte`
- [x] mock captcha widget
- [x] idempotency key lifecycle in the api client
- [x] edge test: a new attempt after a decline gets a fresh key
- [x] simulation F7: honeypot and fill timer

Landed alongside them: `lib/booking/bot_signals.ts`, the measurement as
arithmetic rather than as state in a component, and `BotSignals` in
`lib/api/types.ts`, because the three fields are a wire shape and belong with
the rest of the contract.

The key lifecycle moved out of the booking store into `lib/api/attempt.ts`, next
to the client. The rule was already correct, so this is not a fix: it is the
difference between a rule that can only be tested through a store and one that
can be read on its own. `withIdempotencyKey` moved into `lib/api/idempotency.ts`
for the same reason, so no caller writes the header name for itself.

The edge test the plan lists was already written in phase 5, under ADR-F021. It
is ticked here because the rule it covers now lives in its own file and has a
suite of its own around it.

One thing this phase deliberately does not do: block anything. Every signal is
measured and sent, and the backend decides. A client that refused its own
submission would teach a script exactly which value to change, and would refuse
a slow person on a bad connection for no reason. ADR-F026.

## Phase 7: roster, status, telemetry

- [x] `/roster/[classId]`, hidden from a parent role
- [x] `/status` with `ReadinessDot.svelte` and `lib/stores/status.ts`
- [x] `lib/telemetry/emitter.ts` with batching and silent failure
- [x] `backend/containers/grafana/dashboards/frontend.json` panels agreed with the backend metric names
- [x] the fault code row on that dashboard: `internal_error` and `dependency_unavailable` split from the auth and booking groups
- [x] edge test: a failed telemetry post never surfaces to the parent
- [x] simulation F11: roster view
- [x] simulation F15: status route reflects backend readiness
- [x] simulation F16: nothing sensitive is held by the client

Landed alongside them, because none of the three works without them:
`lib/telemetry/event.ts`, the closed vocabulary both sides check, ADR-F032.
`lib/telemetry/report.ts`, the one place a store reports from, so nothing else
touches the emitter. `lib/telemetry/page_load.ts`, the measurement as arithmetic
rather than as state in a component, ADR-F033. `lib/stores/roster.ts`, which
reads through the api client and never through the cache, ADR-F034. And the
roster, readiness, and version shapes in `lib/api/types.ts`, because all three
are wire contracts and belong with the rest of them.

One change to what phase 2 built. `ApiError` now carries the parsed response
body, because `/readyz` answers 503 with the report naming which dependency is
down, and that report is the only thing the status screen exists to show.
Throwing it away because the status was a failure would lose the answer along
with the failure, ADR-F035.

Where the telemetry is recorded from is worth reading as a decision rather than
as wiring. The cache tier and the first funnel step are recorded in the class
list store, because the screen does not know which tier answered. The api error
is recorded where the parent is about to be told, not where the failure was
caught: "the api refused something" and "somebody was told no" are different
numbers. And only a confirmed seat closes the funnel, because a settled payment
that lost the race is a parent owed a refund, and counting it as reaching the end
would make the one failure the whole design is about invisible on the panel.

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

- [x] layout and spacing pass
- [x] readable states for every error case
- [x] responsive check at a phone width
