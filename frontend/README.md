[![frontend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)

# Frontend

The SvelteKit client parents use to book a trial class. It is a static bundle
served by nginx on port 9001, and it talks to the Go api over http.

This stack is complete on its own. `compose.yml` in this directory builds and
serves the client with no reference to anything outside it, and it deliberately
shares no network with the backend, because the api checks the Origin header on
mutations and the browser has to send the real one.

The badge reports `pull-request-frontend.yml` on `main`. It runs the types, the
four tiers, the static build, the shell suites for this stack's scripts, and the
container image.

<br>

## The One Rule This Client Follows

Seat counts shown to a parent are a hint, never a decision.

The screen may grey out a full class to save a wasted click. It must never treat
its own count as truth, and every step handles a rejection even when the screen
said the seat was free one second ago. The only decision in the whole system is
a transaction on the backend.

That is also what makes caching safe here: everything cacheable is advisory by
construction.

<br>

## State

Phases run in order and land as each one finishes, one commit per file, so the
history reads as small reviewable steps rather than one bulk drop. Every phase is
done. Progress is tracked in `phase-track.md`.

| exists | not yet |
| :- | :- |
| the shell, the version footer, `ssr = false` | a browser end-to-end runner, cut on purpose |
| the api client, its transport, and the typed error mapping | |
| single-flight refresh with exactly one retry | |
| the auth store, memory only | |
| the `/sign-in` screen, a sign out in the header, and the hard sign out behind both | |
| the three tier cache, with conditional requests and invalidation | |
| the class list, the booking screen, and their two stores | |
| the payment screen, the hold countdown, and the booking status screen | |
| the account's bookings, and the header link that is the only way back to one | |
| one idempotency key per attempt, across both of its calls | |
| a retry that knows whether it is a new attempt or the same one | |
| the honeypot, the fill timer, and a mock challenge widget | |
| the roster screen, admin only, never cached | |
| the status screen, polling readiness while it is open and stopping when it is not | |
| batched telemetry that swallows every failure of its own | |
| one named palette, readable failures on every screen, and a phone width | |

Every route the client calls is served by the backend today. The tests still run
against a fake transport, on purpose: a test that needed a database running is a
test nobody runs.

<br>

## Layout

```
frontend
|
|___/containers
|   |___/nginx
|   |   |___nginx.conf                     (serves 9001, one fallback document)
|   |
|   |___Containerfile
|
|___/scripts
|   |___build.sh
|   |___clean.sh                           (destructive, prompts)
|   |___clean_test.sh
|   |___debug.sh                           (the checks, then dev.sh)
|   |___debug_test.sh
|   |___dev.sh
|   |___test.sh
|   |___test_all.sh                        (every frontend test, and the build)
|
|___/src
|   |___/lib
|   |   |___/api                           (the only place fetch is called)
|   |   |___/cache                         (three tiers, and what makes them untrue)
|   |   |___/config                        (the compile time values, read once)
|   |   |___/booking                       (the countdown maths, the price, the bot signals)
|   |   |___/components                    (the cards, the fields, the pickers, the countdown)
|   |   |___/session                       (the wired client, cache, sign out, telemetry)
|   |   |___/stores                        (auth, classes, the booking in flight, the account's bookings, roster, status)
|   |   |___/telemetry                     (the closed vocabulary, the batching, the timing)
|   |
|   |___/routes
|   |___/tests                             (setup, and the behaviour tests)
|
|___.env.template                       (copied to .env, which is never committed)
|___ADR.md
|___HLD.md
|___LLD.md
|___README.md
|___compose.yml
|___how-to.md
|___package.json
|___phase-track.md
|___svelte.config.js
|___tests-and-diagram.md                (every test, its diagram, and the command for it)
|___vite.config.ts
|___work-rules.md
```

<br>

## Port

| port | what |
| :- | :- |
| 9001 | the client, dev server, preview, and container alike |

Bound to 127.0.0.1. Set `FRONTEND_HOST=0.0.0.0` to reach it from elsewhere.

<br>

## Screens

Everything served on 9001.

| url | what |
| :- | :- |
| `/` | the class list, with advisory seat counts |
| `/sign-in` | email and password |
| `/book/[classId]` | pick a child and ask for a hold |
| `/pay/[bookingId]` | the mock payment, with the hold counting down |
| `/booking/[bookingId]` | the booking in whichever state it ended in |
| `/bookings` | every booking on the account, linked from the header |
| `/roster/[classId]` | the roster, admin role only |
| `/status` | build identity and backend readiness, reached by typing the address |

The header carries the title and, once somebody is signed in, a `Your bookings`
link, their name, and a `Sign out` control. Nothing operational, so `/status` has
no link on it: that page answers an operations question, and it sat one click
away from a parent halfway through a booking. ADR-F039.

`Your bookings` is there because every other link to a booking sits on the
payment screen. A parent who paid and closed the tab had no way back to it, and
no way to find out whether the payment completed. ADR-F043.

The password field on the sign in screen can be shown and hidden. It opens
hidden and never remembers being revealed, and the control that reveals it is a
button rather than a bare icon, so it does not submit the form and a keyboard can
reach it. ADR-F042.

Signing out is not a screen, because there is nothing to fill in. Pressing it
tells the api to revoke the refresh token and expire both cookies, then empties
this client: the session in memory, the class cache, and `sessionStorage`. A
refused or unreachable api does not stop the local half, so the screen in front
of the parent is signed out either way. ADR-F040.

Every one of them is client rendered. nginx hands the same document to any
unknown path and the router takes it from there, which is why reloading the page
on `/book/[classId]` works.

The api behind them is on 9000 and its routes are listed in
`../backend/README.md`. The paths this client sends are not collected in one
file: each store exports the one it owns (`stores/classes.ts`,
`stores/booking.ts`, `stores/roster.ts`, `stores/status.ts`,
`telemetry/event.ts`), and `api/client.ts` holds the four session paths.

<br>

## Quick Start

```sh
npm install
npm run dev        # 9001, against the api base url below

npm test           # every tier, against a fake transport
npm run check      # types
npm run build      # the static bundle
```

`scripts/debug.sh` is the same dev server with the checks first: it installs the
dependencies when they are missing, refuses when the container is holding 9001,
and says so when no api answers.

Or as a container, from the repository root:

```sh
export APP_ENV=development
scripts/stack_up.sh frontend
```

Full detail is in `how-to.md`.

<br>

## Build Time Values

Three values reach the bundle from the environment and none of them is ever
written into a component.

| variable | default | used by |
| :- | :- | :- |
| `API_BASE_URL` | http://127.0.0.1:9000 | every api call |
| `BUILD_VERSION` | the version `package.json` declares | the footer and the status route |
| `BUILD_COMMIT` | the short commit git recorded | the same |

They are compile time constants rather than a runtime fetch, so nothing has to
load before the first paint. They are read in exactly one file,
`src/lib/config/environment.ts`.

Neither identity value has to be set. An unnamed build takes both from the
repository, so the footer names a real version and a real commit on a plain
`npm run dev`, and a release still names itself by setting them. A container
build has no git history in its context, so the commit there is `unknown` unless
a pipeline hands it in. ADR-F037.

The api address is the one exception to "compile time and then fixed". When the
page is served from one loopback name and the api is configured with the other,
the host is aligned to the page's, because a browser treats `localhost` and
`127.0.0.1` as different sites and refuses to keep session cookies across them.
Port, scheme, and path are untouched, and a real api address is never rewritten.
ADR-F038.

<br>

## Tokens

This client cannot read a token, and that is the point.

The access token is a JWT and the refresh token is opaque. Both live in HttpOnly
cookies. No code path here reads, decodes, or stores either one. Every request
sends `credentials: "include"` and that is the whole of the token handling.

Whether a session is valid is learned from a response, never decided locally. A
401 saying the token expired triggers one refresh and one retry, invisibly and
only once. Anything worse ends the session, clears the store and session
storage, and routes to sign in with a reason.

<br>

## Tests

Four tiers, all against a fake transport. No network, no browser, no containers.

```sh
npm test
```

The fake records every call, so a test can assert not only what a parent saw but
that a request was never sent at all. That is how the single-flight refresh is
proven, and how a fresh cache is shown to send nothing rather than to send
something cheap.

There is no browser end-to-end runner. Nothing in this stack owns an invariant,
so nothing here needs a real dependency to be believed.

<br>

## Monitoring

This client cannot be scraped, so it posts what it saw to the api and the api
turns those events into series. Every failure of that post is swallowed on
purpose, so a telemetry problem is never something a parent sees.

| open | what is there |
| :- | :- |
| `http://127.0.0.1:9004` | Grafana, the `Ottodot frontend` dashboard, sign in as `admin` with `admin` |
| `http://127.0.0.1:9000/metrics` | the same numbers raw, every one of them prefixed `frontend_` |

Four events and nothing else: a screen became usable, a typed failure was shown,
a booking step was reached, and this client's cache answered. The route travels
as a pattern, so `/booking/[bookingId]` is one series rather than one per booking,
and the api drops anything it does not recognise. The monitoring itself belongs
to the backend stack and starts with either `../scripts/stack_up.sh backend` or
`../backend/scripts/debug.sh`. `scripts/debug.sh` here reports whether Grafana is
answering and starts nothing. `how-to.md` under Looking At What The Client
Reports has the series names and their labels.

<br>

## Why This Stack

Every part was chosen to keep this client out of the decision. It shows what the
api says and refuses nothing on its own. The full reasoning for each choice,
including what was rejected, is in `ADR.md`.

| part | choice | why |
| :- | :- | :- |
| framework | SvelteKit with `ssr = false` and `prerender = false`, built static | a rendering server would be a second moving part in front of the one place a seat count can be trusted (ADR-F001) |
| serving | nginx in its own container, one config file | a static bundle needs a server, and client routes such as `/book/[classId]` only exist at runtime, so every unknown path is handed the same document (ADR-F002) |
| session | both tokens in HttpOnly cookies, never read here | no token sits anywhere a script on the page can reach, and whether a session is valid is learned from a response (ADR-F004) |
| seat counts | advisory on every screen, never a gate | the count may be stale by the time a parent clicks, so a refusal from the api is the real answer and each screen handles it (ADR-F003, ADR-F019) |
| tests | an injected fake transport, no browser and no network | the whole suite runs in about two seconds, and a test can prove that no request was sent at all (ADR-F012) |

The built output ships zero runtime dependencies. Everything in `package.json`
is a build or test tool, because what is deployed is a directory of files and an
nginx container, with no Node process anywhere.

<br>

## Documents

| file | contents |
| :- | :- |
| `ADR.md` | every decision, with what was rejected and what it cost |
| `HLD.md` | routes, stores, the api client's responsibilities, what the screen may assume |
| `LLD.md` | the transport contract, error mapping, the refresh flow, store shapes |
| `how-to.md` | install, dev server, point at an api, test, build, container |
| `tests-and-diagram.md` | every test file, what it proves, its diagram, and the command that runs it alone |
| `phase-track.md` | the build checklist, ticked as tests pass |
| `work-rules.md` | the ceiling on a delegated frontend run, restated from the root |
