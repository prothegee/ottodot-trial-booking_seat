# Frontend How To

Everything for this stack alone. Cross-stack commands are in the root
`how-to.md`.

Every command below is run from `frontend/` unless it says otherwise.

<br>

## What Is Needed

| tool | version | why |
| :- | :- | :- |
| Node | 24 or newer | the toolchain targets it |
| Docker or Podman with compose | any recent | only for the container |

The backend is not needed to develop or test this stack. Every test runs against
a fake transport.

<br>

## Install

```sh
npm install
```

<br>

## Everyday Commands

```sh
npm run dev        # dev server on 9001
npm test           # every tier, fake transport, needs nothing running
npm run check      # svelte-check, types across the whole project
npm run types      # tsc only
npm run build      # the static bundle, into build/
npm run preview    # serve that bundle on 9001
```

Or through the scripts, which are what continuous integration will call:

```sh
scripts/dev.sh
scripts/test.sh
scripts/build.sh
```

<br>

## Pointing At An Api

Three values are compiled into the bundle. Set them before `dev`, `build`, or
the container build.

| variable | default | what it is |
| :- | :- | :- |
| `API_BASE_URL` | http://127.0.0.1:9000 | where every call goes |
| `BUILD_VERSION` | dev | shown in the footer and on the status route |
| `BUILD_COMMIT` | unknown | the same |

```sh
API_BASE_URL=http://127.0.0.1:9000 npm run dev

BUILD_VERSION=$(git describe --tags --always) \
BUILD_COMMIT=$(git rev-parse --short HEAD) \
npm run build
```

They are compile time constants, not a runtime fetch, so changing one needs a
rebuild. That is deliberate: nothing has to load before the first paint.

<br>

## Host And Port

| variable | default | notes |
| :- | :- | :- |
| `FRONTEND_PORT` | 9001 | dev, preview, and the container all use it |
| `FRONTEND_HOST` | 127.0.0.1 | set `0.0.0.0` to reach it from another machine or a container |

The host is set explicitly rather than left to the default. The default resolves
`localhost`, which can bind the IPv6 address alone, and then 127.0.0.1 refuses
the connection while the server insists it is running.

<br>

## The Container

From the repository root:

```sh
export APP_ENV=development
scripts/stack_up.sh frontend
scripts/stack_down.sh frontend
```

The Containerfile builds the bundle and copies it into a Caddy image. Caddy
serves 9001 and hands the same document to any unknown path, which is what makes
a reload on `/book/[classId]` work.

Build arguments are the three values above, so a container build pointed at a
different api is one argument rather than a code change.

<br>

## Cleaning Up

```sh
scripts/clean.sh
```

Destructive, so it behaves like every other destructive script in the
repository:

| behaviour | detail |
| :- | :- |
| `APP_ENV` not `development` | refuses, exit 2. Unset counts as a refusal |
| `--dry-run` | prints the manifest and stops, touching nothing |
| not a terminal | refuses without `--yes` |
| the prompt | the manifest first, then `y/N`, defaulting to No |

It removes build output and local caches. `node_modules` is included, so an
install follows a clean.

`scripts/clean_test.sh` exercises those guards. It deliberately never tests the
confirming path, because a test that answers yes to a destructive prompt is a
test that deletes something.

<br>

## Writing A Test

Every test injects a transport. There is no network, no browser, and no
container anywhere in the suite.

```ts
import { createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";

const transport = createFakeTransport((request) => {
    if (request.path === "/api/v1/classes") {
        return { status: 200, body: [] };
    }

    return { status: 409, body: errorBody("class_full") };
});

const api = createApiClient({ transport, onSignOut: () => {} });
```

`transport.calls` holds every request in order, and `transport.callsTo(method,
path)` filters it. That is how a test asserts a request was never sent at all.

A route test needs three mocks, because a route reaches for the framework and
for the wired singletons:

```ts
vi.mock("$app/navigation", () => ({ goto: vi.fn(() => Promise.resolve()) }));
vi.mock("$app/state", () => ({ page: { params: { bookingId: "..." } } }));
vi.mock("$lib/session/cached_api", () => ({ classReader: { read: vi.fn() }, classMutator: { send: vi.fn() } }));
vi.mock("$lib/session/client", () => ({ api: { request: vi.fn() } }));
```

`$app/navigation` and `$app/state` have no router in a unit test. What is under
test is that a screen asks to leave, and which parameter it read, not how the
framework gets there.

The last two are the seams the stores read and write through. A screen that
reached past them would be the bug, so a test that had to mock something else to
work would be telling you something.

Anything with a countdown needs the clock held still:

```ts
vi.useFakeTimers({ shouldAdvanceTime: true });
vi.setSystemTime(Date.parse("2026-08-11T09:00:00.000Z"));
```

`shouldAdvanceTime` is what keeps promises resolving while the timers are faked.
Without it a test that awaits anything hangs.

<br>

## Common Failures

| symptom | cause | fix |
| :- | :- | :- |
| `Cannot find name '__API_BASE_URL__'` | the constants are declared in `src/app.d.ts` inside `declare global` | run `npm run check`, which syncs the generated types first |
| a component renders nothing under vitest | the server build of Svelte was resolved | `vite.config.ts` sets browser conditions under vitest. Check it was not removed |
| the dev server says it is running but 127.0.0.1 refuses | `FRONTEND_HOST` was overridden | unset it, or set `127.0.0.1` |
| `clean.sh` exits 2 | the guard, working | `export APP_ENV=development` |
| a call fails with a network error in the browser | the api is not running | start the backend stack, `../scripts/stack_up.sh backend`, then `go run ./cmd/api` from `backend/` |
| every write answers `invalid_request` | the api is refusing the origin | `FRONTEND_ORIGIN` on the api has to be exactly the address this stack serves on |

<br>

## Looking At What The Client Reports

The client cannot be scraped, so it posts what it saw to the api and the api
turns those events into series. Nothing about that is visible in the browser on
purpose: every failure of it is swallowed, so a telemetry problem can never be
something a parent sees.

To watch it working, look at the other end.

```sh
# what the client has reported, on the api's own exposition
curl -s 127.0.0.1:9000/metrics | grep '^frontend_'
```

Four events, and nothing else can be sent:

| what happened | series | label |
| :- | :- | :- |
| a screen became usable | `frontend_page_load_seconds` | the route pattern, never a path |
| a typed failure was shown | `frontend_api_error_total` | the api's own code |
| a step was reached | `frontend_booking_funnel_total` | list, hold, pay, confirmed |
| this client's cache answered | `frontend_cache_lookup_total` | fresh, stale, revalidated, miss |

The route is a pattern, so `/booking/[bookingId]` is one series and not one per
booking. The api drops anything it does not recognise, so a page that has not
been reloaded cannot create a series of its own. Both halves are ADR-F032.

The `Ottodot frontend` dashboard in Grafana on 9004 draws all four.

<br>

## The Status Screen

`/status` is the only screen in this client that polls, and it stops when it is
left. It reads the backend's `/version` and `/readyz` every fifteen seconds while
it is open.

| dot | means |
| :- | :- |
| green | every dependency answered |
| amber | the required ones answered and the replica did not. No seat is decided from the replica, so this is correct and a class list may be a moment stale |
| red | a required dependency is down |
| grey | the backend answered nothing at all, which is a different fact from being unready |

The build identity of both halves is on the same screen, because the interesting
case is when they disagree: a deployment that moved one and not the other.

<br>

## What Is Not Here Yet

| screen or feature | phase |
| :- | :- |
| layout and responsiveness | 9 |

Progress is tracked in `phase-track.md`.
