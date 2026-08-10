[![frontend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)
[![frontend main-stable](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main-stable)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)

# Frontend

The SvelteKit client parents use to book a trial class. It is a static bundle
served by Caddy on port 9001, and it talks to the Go api over http.

This stack is complete on its own. `compose.yml` in this directory builds and
serves the client with no reference to anything outside it, and it deliberately
shares no network with the backend, because the api checks the Origin header on
mutations and the browser has to send the real one.

The badges go green once the workflows are added, which is the last phase.

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
history reads as small reviewable steps rather than one bulk drop. Phases 1, 2,
and 3 are done. Progress is tracked in `phase-track.md`.

| exists | not yet |
| :- | :- |
| the shell, the version footer, `ssr = false` | the class list and booking screens, phase 4 |
| the api client, its transport, and the typed error mapping | the payment screen and the countdown, phase 5 |
| single-flight refresh with exactly one retry | honeypot, fill timer, captcha, phase 6 |
| the auth store, memory only | roster, status, telemetry, phase 7 |
| the hard sign out and the `/sign-in` screen | |
| the three tier cache, with conditional requests and invalidation | |

The sign-in screen calls an api that is not built yet, so it runs against a fake
transport in tests and will reach a real one when phase 5 of the backend lands.

<br>

## Layout

```
frontend
|
|___/containers
|   |___/caddy
|   |   |___Caddyfile                      (serves 9001, one fallback document)
|   |
|   |___Containerfile
|
|___/scripts
|   |___build.sh
|   |___clean.sh                           (destructive, prompts)
|   |___clean_test.sh
|   |___dev.sh
|   |___test.sh
|
|___/src
|   |___/lib
|   |   |___/api                           (the only place fetch is called)
|   |   |___/cache                         (three tiers, and what makes them untrue)
|   |   |___/config                        (the compile time values, read once)
|   |   |___/components
|   |   |___/session                       (the wired client, cache, and sign out)
|   |   |___/stores
|   |
|   |___/routes
|   |___/tests                             (setup, and the behaviour simulations)
|
|___ADR.md
|___HLD.md
|___LLD.md
|___README.md
|___compose.yml
|___how-to.md
|___package.json
|___phase-track.md
|___svelte.config.js
|___vite.config.ts
```

<br>

## Port

| port | what |
| :- | :- |
| 9001 | the client, dev server, preview, and container alike |

Bound to 127.0.0.1. Set `FRONTEND_HOST=0.0.0.0` to reach it from elsewhere.

<br>

## Quick Start

```sh
npm install
npm run dev        # 9001, against the api base url below

npm test           # every tier, against a fake transport
npm run check      # types
npm run build      # the static bundle
```

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
| `BUILD_VERSION` | dev | the footer and the status route |
| `BUILD_COMMIT` | unknown | the same |

They are compile time constants rather than a runtime fetch, so nothing has to
load before the first paint. They are read in exactly one file,
`src/lib/config/environment.ts`.

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

## Documents

| file | contents |
| :- | :- |
| `ADR.md` | every decision, with what was rejected and what it cost |
| `HLD.md` | routes, stores, the api client's responsibilities, what the screen may assume |
| `LLD.md` | the transport contract, error mapping, the refresh flow, store shapes |
| `how-to.md` | install, dev server, point at an api, test, build, container |
| `phase-track.md` | the build checklist, ticked as tests pass |
