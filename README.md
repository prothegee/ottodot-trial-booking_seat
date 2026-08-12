[![backend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)
[![frontend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)

# Trial Booking

A trial class booking slice for a service that runs live online science and math
classes for children. Trial classes seat four students. The whole exercise is
about one sentence: when two parents reach for the last seat at the same moment,
exactly one of them may end up confirmed.

**Walkthrough:** `TBA`

The workflow badges above report the two pull request workflows, one per stack,
on `main`. A merge into `main-stable` is a core engineer decision and is gated by
review rather than by these workflows.

<br>

## Current State

This repository is built in phases and this document is kept honest about which
of them exist. All nine phases are finished on each stack.

| stack | done |
| :- | :- |
| backend | phase 1 foundation, phase 2 booking core, phase 3 payment, phase 4 queue and worker, phase 5 authentication, phase 6 http surface, phase 7 monitoring and data hygiene, phase 8 documentation, phase 9 repository wide |
| frontend | phase 1 scaffold, phase 2 api client and auth, phase 3 internal cache, phase 4 booking flow, phase 5 payment and status, phase 6 bot prevention cooperation, phase 7 roster, status, telemetry, phase 8 documentation, phase 9 visual polish |

What that means in practice today:

| works now |
| :- |
| the schema, its four unique indexes, and seed data |
| the booking core: hold, confirm, cancel, fail, expire, and the last-seat transaction |
| the payment path: a deterministic provider, and one charge per idempotency key |
| the job queue and the worker that drains it, on port 9002 |
| the http api on port 9000, end to end from sign in to a seat number |
| ETag caching, a Redis token bucket, and the honeypot, fill timer, and challenge |
| authentication: HS256 access tokens, rotating refresh tokens, and the four auth routes |
| the last-seat race proven against real Postgres, and two workers proven never to share a job |
| one refresh token proven to be spendable exactly once, under real parallelism |
| the client api layer, its auth store, and the sign-in screen with a sign out beside it |
| the client cache: three tiers, with conditional revalidation |
| the class list and the booking screen, with one idempotency key per attempt |
| the payment screen, the hold countdown, and the booking status screen |
| the roster screen for an operator, and the status screen with backend readiness |
| every metric in the plan, on Prometheus, drawn by three provisioned dashboards |
| thirteen alert rules, five of them replayed and proven by `promtool` |
| fault injection, so the error metrics can be watched moving on a live stack |
| the last-seat race and the broken transaction, both scripted against a live stack |
| two pull request workflows, one filtered to each stack, and a deployment simulation that builds both |

Nothing in the code is missing. Two things are still blank, because only the
developer can supply them: the `Walkthrough` link at the top, and the
`Time Spent` section below.

Progress is tracked per stack in `backend/phase-track.md` and
`frontend/phase-track.md`.

<br>

## How This Was Built

Phase by phase, in order, committed as each piece landed. Nothing here was
written to completion in private and then dropped in as one large change.

- A phase closes before the next one opens: foundation, then the booking core,
  then payment, and so on.
- Each phase is its own branch and its own pull request.
- Inside a phase each file gets its own commit, and the message describes that
  file's change and nothing else.

This is aimed at whoever reviews it. A one-file diff in build order can be read,
questioned, and disagreed with in a minute. A single commit carrying a whole
slice can only be taken or left, which is not review.

It also means the plan and the history can be read against each other: the two
`phase-track.md` files list every phase with its checkboxes, in the same order
the commits arrive.

<br>

## What Is Here

Two stacks, each complete on its own. There is no compose file and no container
directory at the repository root, so either stack can be split out or deployed
separately without untangling anything. Root `scripts/` is the single exception,
and it exists precisely to do work that spans both.

```
ottodot-trial-booking_seat
|
|___/backend                               (Go api and worker, its own containers)
|
|___/frontend                              (SvelteKit client, its own container)
|
|___/scripts
|   |___/lib
|   |   |___api.sh                         (sign in, hold a seat, pay, in one place)
|   |   |___confirm.sh                     (the guard, manifest, and prompt)
|   |   |___confirm_test.sh                (every guard, never the confirming path)
|   |   |___settings.sh                    (makes a stack's settings file from its template)
|   |   |___settings_test.sh               (the copy, the file it leaves alone, the refusal)
|   |   |___stack.sh                       (which stacks exist, and where)
|   |   |___stack_test.sh                  (its socket discovery, and which runtime it picks)
|   |
|   |___cleanup_dev.sh                     (destructive, development only)
|   |___cleanup_dev_test.sh                (its guards, never the removing path)
|   |___race_last_seat.sh                  (test 6, over http, --fresh-class to repeat)
|   |___race_last_seat_test.sh             (its guards, and that its cleanup names one class)
|   |___seed_reset.sh                      (destructive, back to the seeded rows)
|   |___smoke_failure.sh                   (test 16, breaks a running api)
|   |___smoke_refund.sh                    (destructive, moves the refunds owed number)
|   |___smoke_refund_test.sh               (its flags, its account rule, and its floor)
|   |___stack_down.sh
|   |___stack_restart.sh                   (the two above, in one command)
|   |___stack_restart_test.sh              (that it delegates and removes nothing)
|   |___stack_status.sh                    (what is up, and what the tests still need)
|   |___stack_status_test.sh               (that it only reads, and reports every service)
|   |___stack_up.sh                        (all, backend, or frontend)
|   |___stack_up_test.sh                   (which services it waits on)
|   |___test_all.sh                        (every test in the repository, nothing left out)
|   |___test_integration.sh                (containers up, both tiers, tests 6 and 16, down)
|
|___.gitignore
|___AI_USAGE.md
|___README.md
|___explanation.md
|___how-to.md
|___work-rules.md                          (the ceiling on a delegated run)
```

<br>

## Why This Stack

One line each. Every choice serves the same goal: the seat is decided in a
single transaction, and nothing outside it is allowed to decide one.

| choice | why |
| :- | :- |
| Go with `net/http`, no framework | the hard part is a transaction, not routing, so no check is hidden behind a convention |
| Postgres holds every deciding read | a row lock is only worth something where the data lives |
| Redis is cache and rate limit only | it can be down without any invariant being at risk |
| the job queue is a Postgres table | background work survives a restart, and there is no second store to keep honest |
| login is two cookies, short-lived and long-lived | no database read to check who you are, and a stolen cookie shows up on first use |
| SvelteKit built static, served by nginx | no rendering server in front of the one place a seat count can be trusted |
| two self-contained stacks | either one deploys alone, and neither can grow a quiet dependency on the other |

`backend/README.md` and `frontend/README.md` each carry the same section with
the trade-off for every row. `backend/ADR.md` and `frontend/ADR.md` carry the
full decision, including what was rejected and what it cost.

<br>

## How To Run

Full commands, including what to do when something refuses to start, are in
`how-to.md`. The short version:

```sh
export APP_ENV=development

cd frontend && npm install && cd ..  # the client's dependencies, once

scripts/test_all.sh                  # every test, from nothing running

scripts/stack_up.sh backend          # the databases, redis, the api, the worker, the monitoring
backend/scripts/migrate.sh           # apply the schema
backend/scripts/seed.sh              # the synthetic dataset, asks before creating the accounts
scripts/stack_up.sh frontend         # the client on 9001
```

Tests first, on purpose. `scripts/test_all.sh` starts its own stack and takes it
down again, so it wants nothing running when it begins. Run it against a stack
that is already up and its last step refuses, because test 16 needs an api
started with the fault surface on and an already-running one has not got it.
`how-to.md` under Run Every Test has the two ways round that.

The brief's scenario, against the running system from the last four lines:

```sh
scripts/race_last_seat.sh                # the seeded seat, once
scripts/race_last_seat.sh --fresh-class  # a throwaway class, as often as you like
```

`scripts/stack_up.sh` with no argument starts both stacks. `scripts/stack_down.sh`
stops them and leaves their local state alone. `scripts/stack_status.sh` says
what is up without changing anything, and adds the three readings that decide
whether the test runs can work: the schema, the seed, and the fault surface.

Three numbered walkthroughs to follow by hand, debug the project, run every test,
and break it on purpose to watch the alert fire, are in `how-to.md` under Step By
Step. The last of them runs in development only, and the api refuses to start
with it switched on anywhere else.

To run a process from the source rather than from a built image, each stack has
a debug script: `backend/scripts/debug.sh` for the api, `backend/scripts/debug.sh
worker` for the worker, and `frontend/scripts/debug.sh` for the dev server. The
backend one starts the databases and the monitoring it needs, and on ctrl-c stops
only what it started, without removing anything. The frontend one starts no
container, so it stops none.

Every container runs on the loopback address only. Ports are listed in
`how-to.md`.

Sign in at `http://127.0.0.1:9001/sign-in` as `alice.tan@example.test`, or as any
of the four seeded accounts. They share the password `otto123`, which `how-to.md`
lists alongside the rest of them. Signing out is the control in the header, which
revokes the refresh token at the api before it clears this browser.

Neither stack's settings file is committed. `backend/config.json` and
`frontend/.env` are made from the committed templates beside them, by the scripts
above, on the first run. A value stated in one of those files wins over an
environment variable of the same name.

<br>

## Monitoring

It starts with the backend stack, because every scrape target is a backend
surface, and it starts with `backend/scripts/debug.sh` too, so the dashboards are
live whether a process is running in a container or from source. Nothing has to
be configured after that: the data source and the three dashboards are files
under `backend/containers/grafana/`, loaded on every start.

| open | what is there |
| :- | :- |
| `http://127.0.0.1:9004` | Grafana. Sign in as `admin` with `admin` |
| `http://127.0.0.1:9003` | Prometheus, its scrape targets and its alert rules |
| `http://127.0.0.1:9000/metrics` | what the api publishes, including the events the client posts to it |
| `http://127.0.0.1:9002/metrics` | what the worker publishes: queue depth and job outcomes |

The three dashboards are `backend`, `frontend`, and `resources`. Cpu, memory, and
drive usage come from node_exporter on 9005 and cAdvisor on 9006, and cAdvisor is
the one service allowed to fail, which is what `resources` is for. `how-to.md`
under Monitoring has the rest, `backend/how-to.md` has the queries and the
administrator sign in.

<br>

## The Last Seat

The scenario the brief asks about, and what this system does at each step.

```
1. Parent A picks the last seat and moves to payment.
2. Parent B picks the same seat.
3. Parent B pays first and is confirmed.
4. Parent A then tries to pay.
```

**The approach.** Selecting a seat grants a hold, not a seat. A hold is a place
on the payment screen and nothing more. The seat itself is decided in one
transaction that locks the class row, counts the seats already taken under that
lock, and takes the lowest free one. Two confirms for the same class cannot run
at the same time, because the second waits on the first one's lock.

```sql
begin;

select id, capacity from trial_classes where id = $1 for update;

select seat_no
    from generate_series(1, $capacity) as seat_no
    where seat_no not in (
        select seat_no from bookings where class_id = $1 and seat_no is not null
    )
    order by seat_no
    limit 1;

update bookings
    set status = 'confirmed', seat_no = $2, confirmed_at = now()
    where id = $3 and status = 'pending_payment';

commit;
```

Parent A therefore reaches step 4, finds no seat free under the lock, and lands
in `refund_required` rather than confirmed. Their money moved, so a status that
says exactly that is what an operator needs.

**Why this and not the alternatives.**

| approach | verdict |
| :- | :- |
| lock the class row, count under the lock | chosen. Simple to explain, simple to test, correct under read committed, and writes serialize per class, which at four seats costs nothing |
| a unique index on `(class_id, seat_no)` | kept as a backstop, not the mechanism. It makes the invariant impossible to violate even from a buggy path or a manual sql edit |
| a denormalized `seats_taken` counter | rejected. One atomic statement is appealing, but the counter can drift from the bookings table, and then the roster and the counter disagree |
| check, then insert, without a lock | rejected. This is the bug the exercise is about |
| a Redis distributed lock | rejected. The database lock is already correct and durable, and a lease that expires mid-transaction lets two writers proceed |

**Trade-offs accepted.**

| trade-off | why it is acceptable |
| :- | :- |
| confirms serialize per class | a class has four seats. The lock is held for one short transaction, and contention is bounded by the class, not the service |
| a parent can pay and then be refunded | that is the cost of letting parent B select a seat parent A is holding, which is the exact sequence the brief requires. `hold_allowance` is a column, so setting it to 0 removes the case without a code change |
| the upper seat bound is not a database constraint | a check constraint cannot read `trial_classes.capacity`. The database stops two bookings sharing a seat, and the transaction enforces the ceiling. Hardcoding `check (seat_no between 1 and 4)` would close it at the cost of freezing capacity forever |

<br>

## Backend Design

**Data model.** `parents`, `students`, `trial_classes`, `bookings`,
`payment_attempts`, plus `booking_events` for the audit trail, `job_queue` for
background work, and `refresh_tokens` for sessions. Full column list in
`backend/LLD.md`.

**The endpoints a booking travels through.** Everything under `/api/v1`, and the
five below are the whole flow. The full list, with who may call each one, is in
`backend/README.md`.

| endpoint | what it does |
| :- | :- |
| `GET /students` | the children on the signed-in account, to choose one |
| `GET /classes` | the catalogue, with advisory seat counts |
| `POST /bookings` | asks for a hold, which is a place on the payment screen and not a seat |
| `POST /bookings/{bookingId}/payments` | settles the charge, then runs the confirm transaction that decides the seat |
| `GET /bookings/{bookingId}` | the status the parent is shown afterwards |

An operator reads `GET /classes/{classId}/roster`, which is admin only and lists
confirmed bookings alone.

**Booking statuses.** `pending_payment`, `confirmed`, `payment_failed`,
`refund_required`, `expired`, `cancelled`. `payment_failed` and
`refund_required` are deliberately separate: one means no money moved, the other
means money moved and must move back. They call for different operator action.

**Where each check lives.**

| check | owner | why there |
| :- | :- | :- |
| the seat | the confirm transaction on the primary | it is the only place that can be right under concurrency |
| one live booking per child per class | the `uq_booking_active` partial index | the database enforces it even from a manual edit |
| two bookings never share a seat | the `uq_seat_taken` partial index | a backstop under the lock, not the mechanism |
| a replayed payment never charges twice | the `uq_payment_idempotency` index | the same request arriving twice must produce one charge |
| who may act on a booking | the api middleware | it needs the token, which the database does not have |
| seat counts on screen | the client, advisory only | it may be stale by the time a parent clicks, and every screen handles the rejection |
| expiring a lapsed hold | the worker | nothing on a request path should depend on a timer |

**Duplicate bookings** are prevented by a partial unique index over
`(student_id, class_id)` covering only the live statuses. A cancelled booking
therefore does not block a parent from starting again, and no application check
can be forgotten.

**Payment failure** never reaches the roster because the payment settles first
and the confirm transaction runs second. A decline leaves the booking in
`payment_failed`, which carries no seat number and cannot appear on a roster
that reads confirmed bookings only.

<br>

## How Correctness Is Proven

Four test tiers run against fakes and need nothing running. A fifth proves the
invariant against real Postgres.

| tier | what it can prove | what it cannot |
| :- | :- | :- |
| unit and edge | the seat picker, the transition table, the hold boundary | anything involving two callers |
| integration and behaviour | the service calls the right things in the right order | that a lock actually serializes |
| real database proof | twenty parallel connections produce exactly four confirmed seats | nothing about the fast tiers, which is why both run |

The fake and the real repository are held to one shared contract suite, so a
fake that quietly disagrees with the sql fails the build.

The proof was checked to have teeth rather than assumed: removing `for update`
from the confirm transaction makes the twenty-goroutine test fail.

Two more run against a live stack over http, cookies and all, and neither
reaches past a guard:

| script | proves |
| :- | :- |
| `scripts/race_last_seat.sh` | the brief's scenario, then reads four tables back to check they agree: one seat handed out, two charges settled, an audit line each, and a refund queued for the parent who lost. `--fresh-class` races a throwaway class so it can be run again |
| `scripts/smoke_failure.sh` | the confirm transaction broken on purpose, and the error arriving in the api's own counter, in Prometheus, in the alert, and in the panel bound to the same series |

The second one exists because of a specific kind of self-deception. A metric
nobody has watched move is a decoration, and an alert nobody has watched fire is
worse, because it is mistaken for coverage.

`scripts/test_integration.sh` puts real containers around the proof tier and both
of those, in one command, and continuous integration calls the same file. It
gives the race script `--fresh-class`, so the race happens on a throwaway class
made and then deleted for the run. Without that it would be a once-only command,
because the seeded seat is gone the moment somebody has it.

`scripts/test_all.sh` is that file plus every other test in the repository.

<br>

## Assumptions

| assumption | reason |
| :- | :- |
| the four seeded accounts share one password, written down in `how-to.md` | a reviewer has to be able to sign in as each of them without being handed anything out of band. The storage is real, argon2id, so only the sharing is the shortcut |
| the payment provider is mocked, with deterministic outcomes | a test and a demonstration have to reproduce exactly |
| a hold lasts ten minutes and a parent may hold three at once | long enough to find a card, short enough that a seat behind someone who walked away comes back |
| `hold_allowance` defaults to 2 | the brief requires parent B to select a seat parent A is holding, and an allowance of 0 makes that impossible |
| one asynchronous replica, promoted by hand | deciding reads all go to the primary, so replica lag is a display concern, not a correctness one |

<br>

## What Was Deliberately Cut

| cut | why |
| :- | :- |
| regular enrollment | out of scope, stated in the brief |
| a real payment provider | the interface is the same shape, and a real one adds credentials and a sandbox without adding judgment |
| automatic failover | promotion is one documented command, and an automatic one needs a quorum that a single-machine demo cannot honestly show |
| a browser end-to-end runner | the real proof is the api-level race, and a browser runner would be slower and prove less |
| visual polish beyond a layout and spacing pass | the brief weights correctness over appearance, so the client got tokens, readable failures, and a phone width, and nothing past that |

<br>

## What Would Be Watched After Release

The alert that matters most is not about uptime. It is `RefundBacklog`: bookings
sitting in `refund_required` whose refund job has not settled. Every row in that
state is a parent who has been charged for a seat they did not get. A service
that is fully up while that number climbs is failing in the way that costs the
most trust.

Beside it, `TransactionErrorSpike` on the confirm transaction, because that is
the one path where a failure means a seat may have been sold twice.

`scripts/smoke_refund.sh` moves that first number on demand, so the panel and the
alert can both be watched arriving and clearing rather than taken on trust.
`--increase` writes bookings that say a parent paid and lost the seat,
`--decrease` closes that many again, and it asks which demo parent first.

<br>

## What Would Be Next With More Time

| next | why it matters |
| :- | :- |
| a browser end-to-end run in continuous integration | the api-level race is the real proof, and a browser run would catch the one class of break neither tier sees: a client that stops sending what the api reads |
| a roster export an operator can hand to a teacher | the brief asks for a roster, and a screen is not the form a teacher wants it in |
| alert thresholds from real history | every threshold here is written for a stack demonstrated for an afternoon, and a quarter of data would move most of them |
| load behaviour beyond four seats | the lock is per class, which is fine here, and worth measuring before a class type with a larger capacity exists |

<br>

## Time Spent

To be filled in before submission.

<br>

## Further Reading

| document | contents |
| :- | :- |
| `how-to.md` | running both stacks, ports, and what to do when something refuses |
| `work-rules.md` | the ceiling on a delegated run, and the report written when it is hit |
| `explanation.md` | one explanation written for a non-technical reader and a reviewer at once |
| `AI_USAGE.md` | the tooling used to build this and how the result was verified |
| `backend/README.md` | the Go stack, its ports, and its own documents |
| `frontend/README.md` | the client, its port, and its own documents |
