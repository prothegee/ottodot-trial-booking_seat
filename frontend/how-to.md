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

To run one test file rather than all of them, `test-diagram.md` lists every one
with what it proves, a diagram for each simulation, and the command for that
file on its own.

<br>

## Running It Manually

```sh
scripts/debug.sh
```

The same dev server, with the checks a manual session keeps tripping over done
first. It installs `node_modules` when they have never been installed here,
refuses when the `ottodot-frontend` container is up because that holds 9001, and
says so when no api answers on 9000 and when no Grafana answers on 9004. Then it
hands over to `scripts/dev.sh`, which is the one place that knows how the server
starts.

A missing api is a warning and not a refusal. The failure states of these pages
are worth looking at, and which api to run is the other stack's business: this
one owns a single container, and the dev server is what replaces it.

Monitoring is reported on the same terms and for the same reason. It belongs to
the backend stack, so this script says whether Grafana is answering and names the
two commands that start it, rather than reaching across to start it. This script
starts no container at all, which is why ctrl-c here stops none.

`scripts/debug_test.sh` covers those guards.

<br>

## Pointing At An Api

Settings live in `.env`, which is not committed. `scripts/debug.sh` and
`scripts/build.sh` make it from the committed `.env.template` on the first run,
say that they did, and carry on. Nothing has to be created by hand.

**The file wins over the environment.** A value stated in `.env` is what the
build uses, whatever the shell carries, which is the same rule the backend
applies to its `config.json`. To let a shell variable decide a setting, delete
its line from `.env`.

| variable | default | what it is |
| :- | :- | :- |
| `API_BASE_URL` | http://127.0.0.1:9000 | where every call goes |
| `FRONTEND_HOST` | 127.0.0.1 | set `0.0.0.0` to reach it from another machine or a container |
| `FRONTEND_PORT` | 9001 | dev, preview, and the container all use it |
| `BUILD_VERSION` | the version `package.json` declares | shown in the footer and on the status route |
| `BUILD_COMMIT` | the short commit git recorded | the same |

`BUILD_VERSION` and `BUILD_COMMIT` are deliberately absent from `.env.template`,
because stating either one in the file would pin every build to whatever was
written down. Unset is not empty: `src/lib/config/build_identity.ts` reads the
version from the manifest and asks git for the commit, so a plain `npm run dev`
already names itself. To name a build something else, set it:

```sh
BUILD_VERSION=$(git describe --tags --always) scripts/build.sh
```

A container build has no git history in its context, so the commit there stays
`unknown` unless a pipeline hands it in, which both workflows do. The version
survives, because `package.json` is copied into the image.

These are compile time constants, not a runtime fetch, so changing one needs a
rebuild rather than a restart. That is deliberate: nothing has to load before the
first paint. It is also why `.env` matters at build time and not at run time.

Which routes that api answers is one table in `backend/how-to.md`, under Every
Route. The paths this client calls are in `src/lib/api/client.ts`.

`API_BASE_URL` has to be an address the api lists in its own `ALLOWED_ORIGINS`,
spelled the same way. `127.0.0.1` and `localhost` are different origins to a
browser, so the two are not interchangeable.

For the loopback pair, that difference is handled rather than left to bite. A
cookie belongs to a host and a port is not part of it, so a page on
`http://localhost:9001` calling `http://127.0.0.1:9000` is cross-site: sign in
answers 204, the browser discards both session cookies, and the next call is a
401 with nothing on screen to explain it. The client aligns the api host with the
page host when both are loopback names, so either address signs in.
`src/lib/config/api_base_url.ts` is the whole of that rule, and it leaves the
port, the scheme, the path, and any real api address alone. ADR-F038.

Nothing secret belongs in `.env`. The bundle is served to anybody who opens the
page, so every value in it is published. The session token is a cookie the page
cannot read, which is why there is no token here to leak.

The image build never sees `.env`. `.dockerignore` keeps it out, so a container
is built from its build arguments rather than from whatever this machine happens
to be pointed at.

<br>

## Host And Port

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

The Containerfile builds the bundle and copies it into an nginx image. nginx
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
| a call fails with a network error in the browser | the api is not running | `../scripts/stack_up.sh backend` for the containerised one, or `../backend/scripts/debug.sh` for one from source |
| `debug.sh` exits 2 naming `ottodot-frontend` | that container is serving the built bundle on 9001, which the dev server needs | `../scripts/stack_down.sh frontend` |
| the dev server exits saying the port is taken, with no container running | something else on this machine holds 9001 | the port is strict on purpose, see Host And Port |
| every write answers `invalid_request` | the api is refusing the origin | the address this stack serves on has to be listed in `ALLOWED_ORIGINS` on the api, spelled exactly |
| every call fails in the browser, but the same call works with curl | the page was opened on an address the api does not list | use `http://127.0.0.1:9001` or `http://localhost:9001`, the two the api lists. Any other address has to be added to `ALLOWED_ORIGINS` |
| sign in answers 204 and the next call is a 401, the console naming a rejected cookie | the page host and the api host are different sites, so the browser keeps neither cookie | the two loopback names are aligned for you. Seeing this means `API_BASE_URL` names a third host: point it at the host the page is served from |
| a value changed in `.env` has no effect | it is read when the bundle is built, not when it is served | restart the dev server, or rebuild |
| a value exported in the shell is ignored | `.env` states it, and the file wins | change it in `.env`, or delete the line there |
| sign in is refused for a seeded address | the password is wrong | it is `otto123` for all four accounts |
| a booking or a payment fails in the browser with a cors message naming `idempotency-key`, but the same call works with curl | the api read a request header its preflight did not permit | fixed in the api. An older image still has it: `../scripts/stack_up.sh backend` rebuilds |
| the status screen says the backend version is `dev` or unknown | the api is running an image built before its version was written down | `../scripts/stack_up.sh backend`, which rebuilds and stamps the images |
| pressing sign out shows the notice about a session used somewhere else | two logouts raced the same refresh token | the control disables itself while the first is running, so this means it was reached another way |

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

The `Ottodot frontend` dashboard in Grafana on 9004 draws all four. Grafana asks
for a sign in: `admin` with `admin`.

<br>

## Signing In

`/sign-in` takes an email and a password, and the four seeded accounts all share
`otto123`.

Somebody already signed in never sees that form. The session is two HttpOnly
cookies this code cannot read, so a fresh tab starts knowing nothing about it:
the shell asks `GET /api/v1/auth/me` once on load, and `/sign-in` sends anybody
with a session to the class list. `src/lib/session/restore.ts` is that question.

A refusal there means nobody was signed in, which is the ordinary answer on a
first visit and is not shown as anything. `/status` needs no session at all and
is left where it is, rather than bounced to the sign-in form. A session that
really ended is still reported, with the notice the screen shows.

The password field carries a control on its right that shows and hides what was
typed. It opens hidden, and it is never remembered: somebody who revealed their
password once at a desk did not agree to reveal it on the next screen they open.
The control is a `button` rather than an icon somebody has to guess at, so it
does not submit the form and it can be reached with a keyboard. It reads
`Show password` or `Hide password`, which is what a screen reader announces,
because the icon itself carries no words.

`src/lib/components/PasswordField.svelte` owns it. The input's type changes
rather than the element being swapped, so revealing does not move the caret or
take the focus away. ADR-F042.

<br>

## Signing Out

The header carries a `Sign out` control while somebody is signed in, and nothing
in its place when nobody is. Pressing it does two things in order:

| order | what | why |
| :- | :- | :- |
| 1 | `POST /api/v1/auth/logout` | the api revokes the refresh token and expires both cookies. Clearing this tab alone would leave a session a stolen cookie could still be used with |
| 2 | the local clear | the auth store, the class cache in memory, and the whole of `sessionStorage` |

A refused or unreachable api does not stop the second step. Somebody who pressed
sign out on a shared machine has to end up signed out of the screen in front of
them whatever the network did, so that failure is swallowed rather than shown.

`src/lib/session/sign_out_request.ts` is the parent asking.
`src/lib/session/sign_out.ts` is the local half on its own, which is what runs
when the api ends the session instead: an expired session, or a refresh token
used twice. The two are separate files because `client.ts` imports the local one,
so the half that reaches for the client cannot live beside it. ADR-F040.

The control is disabled while the call is in flight. Two logouts race the same
refresh token and the api answers the second one as a reuse, which would reach
the parent as the notice meant for a stolen session.

<br>

## Cancelling A Booking

Open `Your bookings` in the header, or the booking's own screen, and press
`Cancel this booking` on the card. It asks once more before anything is sent,
and the second press is what calls `DELETE /api/v1/bookings/{bookingId}`.

The control is on a hold and nowhere else. These are the six statuses and what
each one can still do:

| status | offered on the card |
| :- | :- |
| `pending_payment` | pay for it, or cancel it |
| `confirmed` | nothing. The seat is owned |
| `payment_failed` | nothing. No money was taken |
| `refund_required` | nothing. The refund is already on its way |
| `expired` | nothing. The hold ran out |
| `cancelled` | nothing. It is finished |

A confirmed booking is left alone deliberately. The api allows the transition and
sends none of the money back when it does, so a cancel control there would be a
button that quietly costs a parent the fee.

Cancelling twice is answered `409 invalid_request`, "this booking has already
moved on". The card words that one itself, because the shared wording for that
code talks about a form and there is no form on a booking card.

The same thing from a terminal, which is what the button sends:

```bash
curl --silent --request DELETE \
    --cookie jar.txt --header 'origin: http://127.0.0.1:9001' \
    "http://127.0.0.1:9000/api/v1/bookings/${BOOKING_ID}"
```

<br>

## Getting A Refund

There is no control for it, and that is the design rather than a gap. A refund is
never asked for: it is what the system owes when a payment settled and the seat
had already gone, and the worker sends it without anybody pressing anything.

| what happened | status | who acts |
| :- | :- | :- |
| the payment was declined | `payment_failed` | nobody. No money moved |
| the payment settled and the seat was lost | `refund_required` | the worker, on its own |
| the refund settled | `cancelled` | nobody. It is finished |

The card says so in those words: "Your payment is being refunded automatically.
There is nothing for you to do." A button next to that sentence would invite a
parent to ask for something already happening.

The backend half is `cmd/worker/refunds.go` and `internal/payment/refund.go`.
Backend simulation 08 drives a lost seat through to a settled refund and asserts
the same job run twice refunds once, and frontend simulation F05 covers what the
parent reads while it happens.

<br>

## The Status Screen

`/status` is the only screen in this client that polls, and it stops when it is
left. It reads the backend's `/version` and `/readyz` every fifteen seconds while
it is open.

It has no link on it. Open `http://127.0.0.1:9001/status` directly, or
`http://localhost:9001/status`. The header carries the title and the session
control, because this screen answers an operations question and it sat one click
from a parent halfway through a booking. ADR-F039, amended by ADR-F040.

The backend half of that screen comes from the api's `/version`, which is a
different route from this screen. Typing `127.0.0.1:9000/status` gets a 404: the
api has `/healthz`, `/readyz`, `/version`, and `/metrics`, and nothing else
unversioned. A backend version reading `unknown` means the api is running an
image built before its version was written down, and `../scripts/stack_up.sh
backend` rebuilds it.

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

Nothing in this stack. Every phase is done, including the layout pass: the
palette is named once in `+layout.svelte` and used by every screen, a refusal
reads the same way everywhere, and the shell reduces its padding below 40rem so
a phone spends its width on content.

A browser end-to-end runner is a deliberate cut rather than a gap. The last-seat
race is proven at the api level by `../scripts/race_last_seat.sh`, which is the
harder and faster proof.

Progress is tracked in `phase-track.md`.
