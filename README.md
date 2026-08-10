[![backend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)
[![backend main-stable](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml/badge.svg?branch=main-stable)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-backend.yml)
[![frontend main](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)
[![frontend main-stable](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml/badge.svg?branch=main-stable)](https://github.com/prothegee/ottodot-trial-booking_seat/actions/workflows/pull-request-frontend.yml)

# Trial Booking

A trial class booking slice for a service that runs live online science and math
classes for children. Trial classes seat four students. The whole exercise is
about one sentence: when two parents reach for the last seat at the same moment,
exactly one of them may end up confirmed.

The workflow badges above go green once the workflows are added, which is the
last phase of this build.

<br>

## Current State

This repository is built in phases and this document is kept honest about which
of them exist. Two of nine phases are finished on each stack.

| stack | done | next |
| :- | :- | :- |
| backend | phase 1 foundation, phase 2 booking core | phase 3 payment |
| frontend | phase 1 scaffold, phase 2 api client and auth | phase 3 internal cache |

What that means in practice today:

| works now | not built yet |
| :- | :- |
| the schema, its four unique indexes, and seed data | the http api on port 9000 |
| the booking core: hold, confirm, cancel, and the last-seat transaction | the payment provider and the worker |
| the last-seat race proven against real Postgres | authentication endpoints |
| the client api layer, its auth store, and the sign-in screen | the class list, booking, and payment screens |
| both stacks starting, and both test suites | monitoring, and the video walkthrough |

Progress is tracked per stack in `backend/phase-track.md` and
`frontend/phase-track.md`.

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
|   |   |___confirm.sh                     (the guard, manifest, and prompt)
|   |   |___confirm_test.sh                (every guard, never the confirming path)
|   |   |___stack.sh                       (which stacks exist, and where)
|   |
|   |___stack_up.sh                        (all, backend, or frontend)
|   |___stack_down.sh
|
|___.gitignore
|___AI_USAGE.md
|___README.md
|___explanation.md
|___how-to.md
```

<br>

## How To Run

Full commands, including what to do when something refuses to start, are in
`how-to.md`. The short version:

```sh
export APP_ENV=development

scripts/stack_up.sh backend        # postgres primary, postgres replica, redis
backend/scripts/migrate.sh         # apply the schema
backend/scripts/seed.sh            # the synthetic dataset

cd backend && go test ./...        # the four fast tiers, nothing needs to run
cd backend && go test -tags=containers ./...   # the real database proof

scripts/stack_up.sh frontend       # the client on 9001
cd frontend && npm install && npm test
```

`scripts/stack_up.sh` with no argument starts both stacks. `scripts/stack_down.sh`
stops them and leaves their local state alone.

Every container runs on the loopback address only. Ports are listed in
`how-to.md`.

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
from the confirm transaction makes the twenty-goroutine simulation fail.

<br>

## Assumptions

| assumption | reason |
| :- | :- |
| sign in is a seeded email with no password | authentication is not what the brief is about, and a real password flow adds surface without adding an answer |
| the payment provider is mocked, with deterministic outcomes | a test and a recorded walkthrough have to reproduce exactly |
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
| visual polish | the brief weights correctness over appearance, so it is the last phase and only if time remains |

<br>

## What Would Be Watched After Release

The alert that matters most is not about uptime. It is `RefundBacklog`: bookings
sitting in `refund_required` whose refund job has not settled. Every row in that
state is a parent who has been charged for a seat they did not get. A service
that is fully up while that number climbs is failing in the way that costs the
most trust.

Beside it, `TransactionErrorSpike` on the confirm transaction, because that is
the one path where a failure means a seat may have been sold twice.

<br>

## What Would Be Next With More Time

| next | why it matters |
| :- | :- |
| finish phases 3 to 9 | payment, the worker, authentication, the http surface, monitoring, and the documentation that goes with them |
| a roster export an operator can hand to a teacher | the brief asks for a roster, and a screen is not the form a teacher wants it in |
| replication lag as a first class signal | the client already treats advisory reads as advisory, but nothing yet shows how far behind the replica is |
| load behaviour beyond four seats | the lock is per class, which is fine here, and worth measuring before a class type with a larger capacity exists |

<br>

## Time Spent

To be filled in before submission.

<br>

## Further Reading

| document | contents |
| :- | :- |
| `how-to.md` | running both stacks, ports, and what to do when something refuses |
| `explanation.md` | one walkthrough written for a non-technical reader and a reviewer at once |
| `AI_USAGE.md` | the tooling used to build this and how the result was verified |
| `backend/README.md` | the Go stack, its ports, and its own documents |
| `frontend/README.md` | the client, its port, and its own documents |
