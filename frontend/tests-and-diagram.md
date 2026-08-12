# Frontend Test Diagram

Every test in this stack, what it proves, and the command that runs that one
file on its own. `how-to.md` has the whole-suite commands, this file is the
index underneath them.

Run everything from `frontend/` unless a line says otherwise. The backend has
its own copy of this file at `backend/tests-and-diagram.md`.

<br>

## What Runs What

```mermaid
flowchart TD
    dev["a clone, nothing running"] --> install["npm install"]
    install --> script["scripts/test.sh"]
    script --> types["npm run check<br/>svelte-check, types first"]
    types --> tiers["npm test<br/>unit, edge, integration, behaviour"]
    tiers --> fake["every call goes to the fake transport"]
    fake --> none["no api, no database, no browser, no containers"]

    every["scripts/test_all.sh"] --> script
    every --> bundle["scripts/build.sh<br/>the static bundle"]
    every --> guards["scripts/debug_test.sh<br/>scripts/clean_test.sh"]
```

Nothing in this stack needs the backend running. Every test drives a fake
transport, so the whole suite passes on a machine with no Docker and no Go.

`scripts/test_all.sh` is the one command for this stack: the two runs above,
the bundle, and the guards on the scripts beside them.

Type checking runs first on purpose. A suite that passes while the types are
broken is reporting on a build nobody can ship.

<br>

## The Four Tiers

| tier | what a case in it asks | needs |
| :- | :- | :- |
| unit | one function or one component, one answer | nothing |
| edge | the same thing at its boundaries and its refusals | nothing |
| integration | a store, a client, and a screen wired together | nothing |
| behaviour | a whole scenario as a parent would meet it | nothing |

There is no fifth tier here. The proof tier belongs to the backend, where the
seat is actually decided. What this stack proves is that the client asks
correctly and is honest about every answer it is given.

The tier is written into the case name, so it can be run on its own:

```sh
npm test -- -t "edge:"
```

<br>

## Running One Test

```sh
npm test                                    # every file
npm test -- src/lib/cache                   # one folder
npm test -- src/lib/cache/policy.test.ts    # one file
npm test -- -t "behaviour:"                 # one tier, everywhere
npm test -- --reporter=verbose src/lib/cache/policy.test.ts
```

A path with brackets in it is quoted, because the shell would otherwise read
them:

```sh
npm test -- 'src/routes/book/[classId]/page.test.ts'
```

<br>

## The Tests

Seventeen scenarios, numbered 1 to 17. They live in `src/tests/`, one file each,
and they are the behaviour tier: a whole flow, from what a parent does to what
they end up looking at.

The backend numbers its own scenarios the same way, and the two sets are
unrelated. A bare test number only means something once the stack it belongs to
is named, so anything crossing between the two says which one it means.

<br>

### Test 1: the happy path

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: pick a class, pick a child, submit
    Client->>API: create a booking, with an idempotency key
    API-->>Client: pending_payment, and the hold deadline
    Client->>Parent: the payment screen, countdown running
    Parent->>Client: submit the payment
    Client->>API: pay, with the same key
    API-->>Client: confirmed, seat 2
    Client->>Parent: confirmed, the seat number shown
```

Reading it:

1. The parent picks a class, picks a child, and submits.
2. The client asks the api to create a booking, carrying an idempotency key.
3. The api answers `pending_payment` with the hold deadline.
4. The payment screen opens, and the countdown runs from that deadline.
5. The parent submits the payment.
6. The client pays, carrying the same key as the booking call.
7. The api answers confirmed, with seat 2.
8. The seat number is shown, and the class list is invalidated so the next view
   of it is the count after this seat was taken.

Proves: the countdown runs from the deadline the api returned, both calls carry
the same idempotency key, the confirmed seat number is what the store ends up
holding, and the booking invalidates the class list so the next view of it is
the count after the seat was taken.

```sh
npm test -- src/tests/simulation_01_happy_path_booking.test.ts
```

<br>

### Test 2: the class filled while the parent was choosing

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Note over Client: the cached list says one seat left
    Parent->>Client: submit the booking
    Client->>API: create the booking
    API-->>Client: 409 class_full
    Client->>API: read the class list again
    API-->>Client: no seats left
    Client->>Parent: the class filled while you were choosing
```

Reading it:

1. The cached list on screen says one seat is left.
2. The parent submits the booking.
3. The client asks the api to create it.
4. The api answers 409 `class_full`, because the seat went while the parent was
   choosing.
5. The client throws its own entry away and reads the class list again.
6. The api answers with no seats left.
7. The parent is told the class filled while they were choosing, which is the
   real cause rather than a generic failure.

Proves: the client never insists its own count was right. The entry is
invalidated, the list is read again, and the message names the real cause. A
cached count is a hint, the api is the only thing that decides.

```sh
npm test -- src/tests/simulation_02_stale_seat_count.test.ts
```

<br>

### Test 3: the payment was declined

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: submit the payment
    Client->>API: pay
    API-->>Client: 402 payment_declined
    Client->>Parent: declined, retry on the same booking
    Note over Client: the countdown keeps running, the booking is still pending_payment
```

Reading it:

1. The parent submits the payment.
2. The client pays.
3. The api answers 402 `payment_declined`, so no money moved.
4. The parent is told it was declined, and offered a retry on the same booking.
5. The countdown keeps running, because the booking is still `pending_payment`
   and the hold was never touched.
6. The cached class list is left alone, because a decline says nothing about
   seat counts.
7. A retry mints a fresh idempotency key, because the declined attempt is
   finished and sending the same key back would replay the decline.

Proves: the booking is not abandoned, the countdown continues, the retry reuses
the booking while minting a fresh idempotency key, and the cached class list is
left alone, because a decline says nothing about seat counts. A decline is a
finished attempt, so sending the same key back would replay the decline for as
long as the parent kept trying.

```sh
npm test -- src/tests/simulation_03_payment_declined.test.ts
```

<br>

### Test 4: this child already has a booking

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: book the same child into the same class
    Client->>API: create the booking
    API-->>Client: 409 already_booked, with the existing booking id
    Client->>Parent: already booked, and a link to that booking
```

Reading it:

1. The parent books the same child into the same class again.
2. The client asks the api to create the booking. It does not check first: a
   check followed by a write is the bug this whole exercise is about.
3. The api answers 409 `already_booked`, carrying the id of the booking that
   already exists.
4. The parent is told, and given a link that resolves to that booking.

Proves: no second booking is attempted, and the link resolves to the booking
the child already has. Nothing here checks first and then books, because a
check followed by a write is the bug this whole exercise is about.

```sh
npm test -- src/tests/simulation_04_duplicate_booking.test.ts
```

<br>

### Test 5: the seat was lost after paying

```mermaid
sequenceDiagram
    participant Parent as parent A
    participant Client as client
    participant API as api

    Parent->>Client: submit the payment for the last seat
    Client->>API: pay
    API-->>Client: 409 seat_lost, refund queued
    Client->>Parent: the seat was taken, the payment is being refunded
    Note over Client: no retry control, this one is terminal
```

Reading it:

1. Parent A submits the payment for the last seat.
2. The client pays.
3. The api answers 409 `seat_lost`: the money moved, and the seat went to
   somebody else. A refund is already queued.
4. The parent is told the seat was taken and that the payment is being refunded.
5. No retry control is offered, because this one is terminal. There is nothing
   left to pay for.
6. The class list is sent to be read again, because a lost seat is proof the
   cached count was wrong.

Proves: `SeatLost` renders a terminal message with no retry control, reads
differently from `PaymentDeclined`, and sends the class list to be read again,
because a lost seat is proof the cached count was wrong. Unlike a decline, the
state is carried by the status element rather than by the wording, so no test
and no screen matches on prose. Both are refusals of the same call, and
telling them apart is the difference between a parent who waits for a refund
and a parent who tries to pay again.

```sh
npm test -- src/tests/simulation_05_seat_lost_after_paying.test.ts
```

<br>

### Test 6: the hold countdown reaches zero

```mermaid
flowchart TD
    mount["the payment screen mounts with a deadline"] --> tick["the countdown ticks"]
    tick --> zero{"deadline reached"}
    zero -->|no| tick
    zero -->|yes| lock["the payment control disables"]
    lock --> check["ask the api for the booking status"]
    check --> expired["expired, and an offer to start again"]
```

Reading it:

1. The payment screen mounts, carrying the deadline the api gave it.
2. The countdown ticks.
3. Each tick asks whether the deadline has been reached. While it has not, it
   keeps ticking.
4. At zero the payment control disables at once, without waiting for a round
   trip, so there is never a live button on a hold that has gone.
5. Only then does the screen ask the api for the real booking status, once
   rather than once per tick.
6. The api says expired, and the parent is offered a fresh start. The client
   never declares that itself, because only the backend decides it.

Proves: the control disables at zero without waiting for a round trip, the
screen still confirms the real status with the api, and it asks once rather
than once per tick. A hold that had already ended before the screen opened
closes it at once. Waiting for the api before
disabling would leave a live button on a hold that has gone. Declaring the
booking expired from the browser's clock would be this client deciding
something only the backend decides.

```sh
npm test -- src/tests/simulation_06_hold_countdown.test.ts
```

<br>

### Test 7: the honeypot and the fill timer

```mermaid
flowchart TD
    mount["the form mounts, the timer starts"] --> fill["the fields are filled"]
    fill --> submit["submit"]
    submit --> payload["the payload carries the honeypot value and the elapsed milliseconds"]
    payload --> backend["the backend decides, the client blocks nobody"]
```

Reading it:

1. The form mounts, and a timer starts.
2. The parent fills the fields in. The honeypot input is hidden from sight, from
   the keyboard, and from the accessibility tree, so only a script fills it.
3. The parent submits.
4. The payload carries the honeypot value and the elapsed milliseconds, both
   measured rather than assumed.
5. The backend decides what that means. This client blocks nobody on its own.

Proves: the honeypot input is present, hidden, empty by default, out of the
accessibility tree and out of the keyboard order, the elapsed time is a real
measurement rather than a constant, and none of the three signals says anything
about the parent. A hard coded number would pass the backend's check while
proving nothing about who filled the form in.

```sh
npm test -- src/tests/simulation_07_honeypot_and_fill_timer.test.ts
```

<br>

### Test 8: the submit control is clicked twice

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: click submit
    Client->>API: pay with key K
    Parent->>Client: click submit again immediately
    Note over Client: the control is disabled, no second call is issued
    API-->>Client: settled
    Client->>Parent: the result, once
```

Reading it:

1. The parent clicks submit.
2. The client pays with key K, and disables the control for as long as that call
   is in flight.
3. The parent clicks submit again straight away.
4. Nothing is sent, because the control is disabled. The click never becomes a
   second call.
5. The api answers settled.
6. The parent sees one result. Had a second call escaped, key K would have made
   it harmless anyway: the control is the cheap layer, the key is the correct
   one.

Proves: exactly one call is recorded, the control is disabled for the whole
in-flight window, and the idempotency key would have made a leaked second call
harmless anyway. The control is the cheap layer, the key is the correct one.

```sh
npm test -- src/tests/simulation_08_double_submit.test.ts
```

<br>

### Test 9: silent refresh, single flight

```mermaid
sequenceDiagram
    participant Client as client
    participant API as api

    Client->>API: three calls in parallel
    API-->>Client: 401 token_expired, three times
    Client->>API: refresh, once only
    API-->>Client: new cookies
    Client->>API: retry all three originals
    API-->>Client: 200, 200, 200
```

Reading it:

1. The client has three calls in flight at once.
2. All three come back 401 `token_expired`, because the access token aged out
   between the page opening and these calls.
3. The client refreshes once. The other two do not each start a refresh of their
   own, which is what single flight means.
4. The api returns new cookies.
5. All three original calls are retried, once each.
6. All three answer 200, and the parent never saw an interruption.

Proves: exactly one refresh call, all three originals retried once each,
nothing retried twice, and no sign out reported, so the parent sees no
interruption.

```sh
npm test -- src/tests/simulation_09_silent_refresh.test.ts
```

<br>

### Test 10: hard sign out on a reused token

```mermaid
sequenceDiagram
    participant Client as client
    participant API as api

    Client->>API: a call
    API-->>Client: 401 token_expired
    Client->>API: refresh
    API-->>Client: 401 token_reused
    Note over Client: clear the auth store, clear session storage
    Client->>Client: route to sign in, with a security notice
```

Reading it:

1. The client makes an ordinary call.
2. The api answers 401 `token_expired`.
3. The client refreshes, exactly as it would in test 9.
4. This time the api answers 401 `token_reused`: that refresh token had already
   been spent, so the whole family was revoked.
5. The client clears the auth store and the whole of `sessionStorage`. It does
   not retry, because there is nothing left to retry with.
6. It routes to sign in carrying the reason, so the screen can state why.

Proves: no retry loop, the auth store and the whole of `sessionStorage`
cleared, and the reason surviving so the sign-in screen can state it.

```sh
npm test -- src/tests/simulation_10_hard_sign_out.test.ts
```

<br>

### Test 11: the roster view

```mermaid
flowchart TD
    teacher["a teacher opens the roster for a class"] --> api["the api answers with the confirmed students and their seats"]
    api --> screen["confirmed only, seats in order, capacity and seats used"]
    screen --> never["never written to the cache"]
    parent["a parent tries the same route"] --> refused["the api refuses the role"]
```

Reading it:

1. A teacher opens the roster for a class.
2. The api answers with the confirmed students and their seat numbers.
3. The screen renders confirmed bookings only, seats in order, with the capacity
   and how many are used.
4. Nothing here is written to the cache. This is the one screen that puts a
   child's name next to a seat, so a cached copy would be the one place in the
   browser where another family's name outlives the screen that showed it.
5. A parent typing the same address is refused by the api on their role, not by
   a hidden link.

Proves: only `confirmed` bookings appear, seat numbers render in order, the
roster is never cached, a parent is refused by the api rather than by a hidden
link, and a class with nobody confirmed says so rather than rendering an empty
table. This is the only screen that puts a child's name next to a seat,
so a cached copy would be the one place in the browser where another family's
name outlives the screen that showed it.

```sh
npm test -- src/tests/simulation_11_roster_view.test.ts
```

<br>

### Test 12: a fresh cache sends no request

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: open the class list
    Client->>API: GET the classes
    API-->>Client: 200 with a body and an ETag
    Parent->>Client: navigate away and back within five seconds
    Note over Client: served from memory
    Note over API: no second call is recorded
```

Reading it:

1. The parent opens the class list.
2. The client asks the api for the classes.
3. The api answers 200 with a body and an ETag.
4. The parent navigates away and comes back within five seconds.
5. The list is served from memory, and the lookup reports itself as fresh.
6. No second call is recorded at all. Not a cheap call, none.

Proves: exactly one call, an identical rendered list, and the lookup reporting
itself as fresh.

```sh
npm test -- src/tests/simulation_12_fresh_cache.test.ts
```

<br>

### Test 13: a stale cache revalidates to a 304

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Note over Client: the entry is ten seconds old
    Parent->>Client: open the class list
    Client->>Parent: the stale list renders immediately
    Client->>API: GET the classes, If-None-Match "v41"
    API-->>Client: 304 Not Modified
    Note over Client: body kept, age refreshed, nothing repainted
```

Reading it:

1. The stored entry is ten seconds old, so it is stale rather than absent.
2. The parent opens the class list.
3. The stale list renders immediately, before any request has resolved.
4. The client asks the api for the classes, carrying the stored tag as
   `If-None-Match: "v41"`.
5. The api answers 304 Not Modified.
6. The body is kept, its age is refreshed, and nothing repaints, so subscribers
   are not woken for an answer that changed nothing.

Proves: the stale body is returned before the request resolves, the request
carries the stored tag, the 304 does not replace the body, subscribers are not
needlessly notified, and the entry is fresh again afterwards.

```sh
npm test -- src/tests/simulation_13_stale_revalidation.test.ts
```

<br>

### Test 14: a mutation invalidates the cache

```mermaid
flowchart TD
    paid["the payment is confirmed"] --> cold["the class list entry goes cold"]
    cold --> back["the parent returns to the list"]
    back --> blocking["a blocking GET, carrying the stored tag"]
    blocking --> fresh["200 with a new tag and the updated seat counts"]
```

Reading it:

1. The payment is confirmed, which is a seat leaving the class.
2. At that moment the class list entry goes cold, rather than at the next read.
3. The parent returns to the list.
4. Because the entry is cold, the read blocks on a real GET, carrying the stored
   tag.
5. The api answers 200 with a new tag and the updated seat counts, so what the
   parent sees is the count after their own booking.

A mutation that failed changes none of this, because nothing is known to have
changed.

Proves: the entry stops being usable at the moment the mutation succeeds, the
next read is not served from memory, and the new seat count is what the parent
sees. A mutation that failed changes nothing, because nothing is known to have
changed.

```sh
npm test -- src/tests/simulation_14_mutation_invalidates.test.ts
```

<br>

### Test 15: the status route reflects backend readiness

```mermaid
flowchart TD
    open["/status opens"] --> ask["GET /version and GET /readyz"]
    ask --> green["all ok: green, every dependency listed"]
    ask --> amber["the replica is behind: amber, degraded"]
    ask --> red["503: red, unready"]
    ask --> grey["no answer at all: grey, and a different message from unready"]
    green --> poll["poll every fifteen seconds while open, and stop on the way out"]
    amber --> poll
    red --> poll
    grey --> poll
```

Reading it:

1. The `/status` route opens.
2. It asks the api two questions at once: `/version` for the build identity, and
   `/readyz` for the dependencies.
3. Everything answers ok, and the screen is green with every dependency listed.
4. The replica is behind: amber, degraded. That costs a class list some accuracy
   and costs nobody a seat, so it is not shown as an outage.
5. The api answers 503: red, unready.
6. Nothing answers at all: grey, and a different message from unready, because
   unreachable and unready are different problems.
7. Whichever of the four it landed on, the screen polls every fifteen seconds
   while it is open, and stops the moment the route is left.

Proves: the build identity renders from `/version`, each dependency renders its
own row, amber reads differently from red, and the polling stops when the route
is left. A replica that has fallen behind costs a class list some accuracy and
costs nobody a seat, so showing it as an outage would send somebody looking for
a problem that is not costing anything.

```sh
npm test -- src/tests/simulation_15_status_route.test.ts
```

<br>

### Test 16: nothing sensitive is held by the client

```mermaid
flowchart TD
    drive["drive a full booking, signed in, through to confirmed"] --> capture["capture four surfaces"]
    capture --> stores["every store"]
    capture --> storage["every sessionStorage entry"]
    capture --> urls["every route visited"]
    capture --> telemetry["every queued telemetry payload"]
    stores --> scan["scan all four for an email or a token"]
    storage --> scan
    urls --> scan
    telemetry --> scan
    scan --> cookie["and watch document.cookie: no code path reads it"]
```

Reading it:

1. A full booking is driven through, signed in, all the way to confirmed.
2. Four surfaces are then captured: every store, every `sessionStorage` entry,
   every route visited, and every queued telemetry payload.
3. All four are scanned for an email or a token.
4. `document.cookie` is watched at the same time, so the claim is that no code
   path reads it rather than that nobody wrote the line.

Proves: the seeded email appears in no store, no storage entry, no url, and no
telemetry payload, and a sign out leaves nothing of the previous parent behind.
A client that never reads a token cannot leak one however
badly it is written, which is why the case watches `document.cookie` rather
than trusting that nobody wrote the line.

```sh
npm test -- src/tests/simulation_16_nothing_held.test.ts
```

<br>

### Test 17: the backend transaction broke mid-payment

```mermaid
sequenceDiagram
    participant Parent as parent
    participant Client as client
    participant API as api

    Parent->>Client: submit the payment
    Client->>API: pay with idempotency key K
    API-->>Client: 500 internal_error, with a request id
    Client->>Parent: something broke, the booking is untouched, retry
    Note over Client: no claim about the seat, no claim about the money
    Parent->>Client: retry
    Client->>API: pay again with the same key K
    API-->>Client: 200 confirmed
    Client->>Parent: confirmed, the seat number shown
```

Reading it:

1. The parent submits the payment.
2. The client pays with idempotency key K.
3. The api answers 500 `internal_error`, with a request id.
4. The parent is told something broke and that the booking is untouched. The
   message never says the seat was lost and never says the payment was
   declined, because neither is known.
5. The countdown keeps running and the cached list is left alone, for the same
   reason: nothing is known to have changed.
6. The parent retries.
7. The client pays again with the same key K. An `internal_error` is an attempt
   of unknown outcome, so a fresh key could charge twice.
8. The api answers 200 confirmed, and the seat number is shown.

The client half of backend test 15. Proves: the message never says the
seat was lost and never says the payment was declined, the booking stays on
screen as pending, the countdown keeps running because the hold was not
touched, the cached list is left alone because nothing is known to have
changed, the retry reuses the same key, and the request id is rendered for
quoting. A declined payment is a finished attempt and earns a
fresh key, as in test 3. An `internal_error` is an attempt of unknown outcome, so
the same key must go back or a retry risks charging twice.

```sh
npm test -- src/tests/simulation_17_transaction_broke.test.ts
```

<br>

## Every Other Test, By Folder

One line per file: the tiers its own cases declare, what it covers, and the
command for that file alone. The tests above are not repeated here.

### src/lib/api

```sh
# errors.test.ts (unit, edge): the whole backend error table, an unknown code, a
# missing envelope, and wording that leaks nothing
npm test -- src/lib/api/errors.test.ts

# transport.test.ts (unit, edge): credentials, url building, headers, a 204, and a
# body that is not json
npm test -- src/lib/api/transport.test.ts

# client.test.ts (unit, edge, integration, behaviour): the request pipeline, one retry,
# never a loop, login never refreshing, and the conditional request
npm test -- src/lib/api/client.test.ts

# refresh.test.ts (unit, edge): single flight, one report per failure, and a cleared
# in-flight promise
npm test -- src/lib/api/refresh.test.ts

# idempotency.test.ts (unit, edge): uniqueness, length, and the fallback still being random
npm test -- src/lib/api/idempotency.test.ts

# attempt.test.ts (unit, edge, behaviour): which kinds of failure spend a key, and an
# unknown kind keeping it
npm test -- src/lib/api/attempt.test.ts
```

### src/lib/cache

```sh
# policy.test.ts (unit, edge): the three freshness tiers, both boundaries, and an entry
# dated in the future
npm test -- src/lib/cache/policy.test.ts

# key.test.ts (unit, edge): what the cache may hold, and two query strings never colliding
npm test -- src/lib/cache/key.test.ts

# store.test.ts (unit, edge, integration): save, touch, invalidate, clear, who is notified,
# and surviving a reload
npm test -- src/lib/cache/store.test.ts

# read_through.test.ts (edge, integration): all four lookup results, a swallowed background
# failure, and a shared revalidation
npm test -- src/lib/cache/read_through.test.ts

# mutation.test.ts (unit, edge, integration): a success invalidates, a failure does not
npm test -- src/lib/cache/mutation.test.ts

# session_mirror.test.ts (unit, edge): a round trip, the namespace, a storage that throws,
# and an entry that will not parse
npm test -- src/lib/cache/session_mirror.test.ts
```

### src/lib/stores

```sh
# auth.test.ts (unit, edge): what the auth store holds, and what it drops
npm test -- src/lib/stores/auth.test.ts

# classes.test.ts (unit, edge, integration): the path read, the freshness tier reported,
# and a failed read leaving the list alone
npm test -- src/lib/stores/classes.test.ts

# booking.test.ts (unit, edge, integration): one key across both calls, a fresh key per
# attempt, and a duplicate carrying the booking that exists
npm test -- src/lib/stores/booking.test.ts

# bookings.test.ts (unit, edge, integration, behaviour): an empty answer told apart from
# one never asked for, and a failed read leaving the list alone
npm test -- src/lib/stores/bookings.test.ts

# roster.test.ts (unit, edge, integration, behaviour): a refusal for the role told apart
# from any other failure, and a failed read clearing the names
npm test -- src/lib/stores/roster.test.ts

# status.test.ts (unit, edge, integration, behaviour): polling that stops, a 503 read as an
# answer, and degraded distinguished from unavailable
npm test -- src/lib/stores/status.test.ts
```

### src/lib/components

```sh
# ClassCard.test.ts (unit, edge, integration): zero seats, capacity taken, a negative count,
# one seat left, and the actions being the last block in the card
npm test -- src/lib/components/ClassCard.test.ts

# ChildPicker.test.ts (unit, edge, integration): every child offered, one preselected, and
# none on an empty account
npm test -- src/lib/components/ChildPicker.test.ts

# PasswordField.test.ts (unit, edge, integration, behaviour): hidden to begin with, revealed
# and hidden again, the typed value surviving both, and a control that does not submit
npm test -- src/lib/components/PasswordField.test.ts

# PaymentForm.test.ts (unit, edge, integration, behaviour): one field only, the honeypot sent
# as it stands, and the challenge token carried
npm test -- src/lib/components/PaymentForm.test.ts

# CaptchaWidget.test.ts (edge, integration, behaviour): one token, the unanswered state, and
# no callback after unmount
npm test -- src/lib/components/CaptchaWidget.test.ts

# HoldCountdown.test.ts (edge, integration, behaviour): the label moving with the clock, a past
# or missing deadline reading as ended, zero reported once rather than on every tick, and a
# suspended tab correct on its first frame back
npm test -- src/lib/components/HoldCountdown.test.ts

# BookingActions.test.ts (unit, edge, integration, behaviour): buttons rather than links, only
# a hold cancellable, two presses to cancel, and a second press ignored
npm test -- src/lib/components/BookingActions.test.ts

# BookingStatus.test.ts (edge, integration, behaviour): all six statuses, the seat shown only
# when won, the class named as the heading, and the block left out when it could not be read
npm test -- src/lib/components/BookingStatus.test.ts

# ReadinessDot.test.ts (unit, edge, behaviour): the three states, the fourth for no answer,
# and the state readable without the colour
npm test -- src/lib/components/ReadinessDot.test.ts

# VersionFooter.test.ts (unit, edge): the version and the commit injected at build time, a long
# commit shortened to seven characters, and nothing else carried
npm test -- src/lib/components/VersionFooter.test.ts
```

### src/lib/session, src/lib/booking, src/lib/config, src/lib/telemetry

```sh
# session/sign_out.test.ts (unit, edge, integration): store cleared, cache cleared, storage
# cleared, stores reset, navigation requested
npm test -- src/lib/session/sign_out.test.ts

# session/sign_out_request.test.ts (edge, integration, behaviour): the api told first, nothing
# left behind, and a refused logout still clearing this device
npm test -- src/lib/session/sign_out_request.test.ts

# session/restore.test.ts (edge, integration, behaviour): a tab with cookies getting its parent
# back, and a first visit never being told a session ended
npm test -- src/lib/session/restore.test.ts

# booking/countdown.test.ts (unit, edge): minutes and seconds at a fixed width, a deadline
# exactly now already expired, and a past, absent, or unparseable one reading as expired
# rather than as a negative or as NaN
npm test -- src/lib/booking/countdown.test.ts

# booking/cancel_request.test.ts (edge, integration, behaviour): the delete the api registered,
# the seat count dropped, and a refusal reaching the caller
npm test -- src/lib/booking/cancel_request.test.ts

# booking/bot_signals.test.ts (unit, edge): a real measurement, a backwards clock, and a
# rounded fraction
npm test -- src/lib/booking/bot_signals.test.ts

# config/api_base_url.test.ts (unit, edge, integration, behaviour): the loopback pair aligned
# both ways, and a real address left alone
npm test -- src/lib/config/api_base_url.test.ts

# config/build_identity.test.ts (unit, edge, behaviour): the manifest version, the short commit,
# and what each answers where neither exists
npm test -- src/lib/config/build_identity.test.ts

# config/environment.test.ts (unit, edge, behaviour): the api base url and the build identity
# arriving from the build, an unset variable falling back to the local value, and an unnamed
# build still saying which code it is
npm test -- src/lib/config/environment.test.ts

# telemetry/emitter.test.ts (unit, edge, integration): batching, a swallowed failure, no retry,
# the cap, and an idle emitter scheduling nothing
npm test -- src/lib/telemetry/emitter.test.ts

# telemetry/page_load.test.ts (unit, edge, integration): the gap rather than the mount, one
# report per screen, and a pattern rather than a path
npm test -- src/lib/telemetry/page_load.test.ts
```

### src/routes

```sh
# layout.test.ts (unit, edge, integration): the header shell, the control shown only to
# somebody signed in, a second press ignored, and the session asked for once on mount
npm test -- src/routes/layout.test.ts

# page.test.ts (edge, integration): the class list renders, a full class offers no way in,
# and a failed read says so
npm test -- src/routes/page.test.ts

# sign-in/page.test.ts (edge, integration, behaviour): the screen, including the notice after
# a reused token
npm test -- src/routes/sign-in/page.test.ts

# book/[classId]/page.test.ts (edge, integration): a hold moves on, and a full class or a
# duplicate is answered on the screen
npm test -- 'src/routes/book/[classId]/page.test.ts'

# pay/[bookingId]/page.test.ts (edge, integration, behaviour): the booking read on arrival, a
# settled payment moving on, a decline offering a retry, a lost seat being terminal, an
# internal_error claiming nothing, and the countdown reaching zero closing the control
npm test -- 'src/routes/pay/[bookingId]/page.test.ts'

# booking/[bookingId]/page.test.ts (edge, integration, behaviour): the status read from the
# api, a hold given up, and the booking read again afterwards
npm test -- 'src/routes/booking/[bookingId]/page.test.ts'

# bookings/page.test.ts (edge, integration, behaviour): a pending payment and a completed one
# told apart, and the empty state never standing in for a failure
npm test -- src/routes/bookings/page.test.ts
```

<br>

## The Script Guards

The scripts have tests of their own. They read their subject or stop inside a
guard, so none of them starts or removes anything:

```sh
scripts/debug_test.sh
scripts/clean_test.sh
```

The repository-wide guards, for the scripts under the root `scripts/`, are
listed in the root `how-to.md` under Run Every Test.

<br>

## Where Else Tests Are Described

| document | what it holds |
| :- | :- |
| `how-to.md` | the whole-suite commands, and Writing A Test |
| `HLD.md` | Testing, why every test drives a fake transport |
| `LLD.md` | Tests, the same file list with what each one covers |
| `phase-track.md` | which phase each test landed in |
