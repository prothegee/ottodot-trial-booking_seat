# Backend Low Level Design

The detail. Tables, indexes, the confirm transaction line by line, the state
machine, and the error contract. Boundaries are in `HLD.md`, reasoning is in
`ADR.md`.

<br>

## Schema

Two enums and eight tables. The migration is `migrations/0001_schema.sql`, the
synthetic dataset is `migrations/0002_seed.sql`.

```sql
create type booking_status as enum (
    'pending_payment', 'confirmed', 'payment_failed',
    'refund_required', 'expired', 'cancelled'
);

create type payment_status as enum ('initiated', 'succeeded', 'failed');
```

Identifiers are UUIDv7, minted in the application. No extension, no database
side default. See ADR-002.

### parents

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| email | text | not null, unique |
| full_name | text | not null |
| role | text | not null, default parent, check in (parent, admin) |
| created_at | timestamptz | not null, default now() |

### students

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| parent_id | uuid | not null, references parents(id) on delete cascade |
| full_name | text | not null |
| grade_level | smallint | not null, check between 1 and 12 |
| created_at | timestamptz | not null, default now() |

### trial_classes

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| subject | text | not null, check in (science, math) |
| title | text | not null |
| starts_at | timestamptz | not null |
| duration_minutes | smallint | not null, default 60, check greater than 0 |
| capacity | smallint | not null, default 4, check greater than 0 |
| hold_allowance | smallint | not null, default 2, check at least 0 |
| created_at | timestamptz | not null, default now() |

`hold_allowance` is a column rather than a constant so the strict behaviour,
allowance 0, is a data change. See ADR-005.

### bookings

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| student_id | uuid | not null, references students(id) |
| class_id | uuid | not null, references trial_classes(id) |
| status | booking_status | not null, default pending_payment |
| seat_no | smallint | null until confirmed, check at least 1 |
| hold_expires_at | timestamptz | null once the booking stops holding |
| confirmed_at | timestamptz | null |
| created_at | timestamptz | not null, default now() |
| updated_at | timestamptz | not null, default now() |

Two check constraints make a half-written confirmation impossible:

```sql
constraint bookings_confirmed_holds_a_seat
    check (status <> 'confirmed' or seat_no is not null)

constraint bookings_confirmed_is_stamped
    check (status <> 'confirmed' or confirmed_at is not null)
```

### payment_attempts

Append only. A row is never mutated after settlement.

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| booking_id | uuid | not null, references bookings(id) |
| idempotency_key | text | not null |
| amount_cents | integer | not null, check greater than 0 |
| currency | char(3) | not null, default SGD |
| status | payment_status | not null |
| provider_ref | text | null |
| failure_reason | text | null |
| created_at | timestamptz | not null, default now() |
| settled_at | timestamptz | null |

### booking_events

Append only audit trail, written inside the transaction that changes the status.
See ADR-009.

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| booking_id | uuid | not null, references bookings(id) |
| from_status | booking_status | null on creation |
| to_status | booking_status | not null |
| actor | text | not null, check in (parent, system, admin, payment) |
| reason | text | null |
| created_at | timestamptz | not null, default now() |

### job_queue

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| kind | text | not null, check in (expire_hold, reconcile_refund) |
| payload | jsonb | not null |
| run_after | timestamptz | not null, default now() |
| attempts | smallint | not null, default 0 |
| locked_until | timestamptz | null |
| created_at | timestamptz | not null, default now() |

### refresh_tokens

The access token is a JWT and is stored nowhere. Only the refresh token has a
row, and only as a hash.

| column | type | constraint |
| :- | :- | :- |
| id | uuid | primary key |
| parent_id | uuid | not null, references parents(id) on delete cascade |
| token_hash | bytea | not null, unique, sha256 of the token |
| family_id | uuid | not null, groups one rotation chain |
| expires_at | timestamptz | not null |
| revoked_at | timestamptz | null |
| created_at | timestamptz | not null, default now() |

<br>

## The Four Invariants

Each one is a partial unique index. Partial matters: it is what makes the rule
apply to live rows only.

```sql
-- one live booking per child per class
create unique index uq_booking_active
    on bookings (student_id, class_id)
    where status in ('pending_payment', 'confirmed');

-- two bookings can never own the same seat in a class
create unique index uq_seat_taken
    on bookings (class_id, seat_no)
    where seat_no is not null;

-- a replayed payment submission cannot create a second charge
create unique index uq_payment_idempotency
    on payment_attempts (booking_id, idempotency_key);

-- one row per refresh token, so reuse is detectable
create unique index uq_refresh_token_hash
    on refresh_tokens (token_hash);
```

`uq_booking_active` is why cancelling frees a child to book again: a cancelled
row is not in the index. `uq_seat_taken` is why cancelling clears `seat_no`
rather than leaving it: a cancelled booking holding a seat number would keep
that seat locked out forever.

**Known gap, stated openly.** A check constraint cannot read
`trial_classes.capacity`, so the database stops two bookings sharing a seat but
does not stop a bug assigning seat 9 in a four seat class. The upper bound is
enforced inside the confirm transaction while the class row is locked.
Hardcoding `check (seat_no between 1 and 4)` would close it at the cost of
freezing capacity forever.

<br>

## The Confirm Transaction

The heart of the service. `internal/booking/repository_postgres.go`.

```sql
begin;

-- 1. which class this booking belongs to. The class is always locked before
--    the booking, so two confirms cannot take the locks in opposite order.
select class_id from bookings where id = $1;

-- 2. serialize every confirm for this class behind one row lock
select id, capacity from trial_classes where id = $2 for update;

-- 3. the booking, now that the class is held
select ... from bookings where id = $1 for update;

-- 4. the lowest free seat under the lock, no row when the class is full
select seat_no
    from generate_series(1, $capacity) as seat_no
    where seat_no not in (
        select seat_no from bookings
            where class_id = $2 and seat_no is not null
    )
    order by seat_no
    limit 1;

-- 5a. a seat was free
update bookings
    set status = 'confirmed', seat_no = $3, confirmed_at = $4,
        hold_expires_at = null, updated_at = $4
    where id = $1 and status = 'pending_payment';

-- 5b. no seat was free. This path commits, because money already moved
update bookings
    set status = 'refund_required', hold_expires_at = null, updated_at = $4
    where id = $1 and status = 'pending_payment';

-- 6. either way, one line of audit trail
insert into booking_events (...) values (...);

commit;
```

| step | why it is there |
| :- | :- |
| 1 before 2 | lock ordering. Class first, booking second, always |
| 2 | the whole mechanism. Two confirms for one class cannot overlap |
| 3 | the status check has to happen under the lock, or a cancel could slip between |
| 4 | mirrored exactly by `LowestFreeSeat` in Go, which is what the fake uses |
| 5b commits | the `refund_required` row is the only record that money has to move back. Rolling it back would lose it |
| 6 | in the same transaction, so the trail cannot disagree with the booking |

The hold deadline is deliberately not consulted. See ADR-008.

<br>

## The Hold Transaction

```
lock the class row
resolve the parent from the student            -> ErrStudentNotFound
a live booking for this child and class?       -> ErrAlreadyBooked
this parent's standing holds >= the cap?       -> ErrTooManyHolds
holders >= capacity + hold_allowance?          -> ErrClassFull
insert pending_payment with a deadline
write the audit line
```

The order is the order a parent would want to hear it: the most specific answer
first. Already booked says exactly what happened, the cap is about this parent,
and a full class is about everyone.

Two counting rules that differ on purpose:

| rule | counts | does not count |
| :- | :- | :- |
| duplicate, mirroring `uq_booking_active` | any `pending_payment` or `confirmed` row | nothing. Time is not part of it |
| holders, the allowance rule | `confirmed`, plus `pending_payment` whose deadline has not passed | a lapsed hold |

A lapsed hold therefore stops holding a class open, while still blocking a
second booking for the same child until the worker expires it. See ADR-008.

<br>

## The State Machine

`internal/booking/status.go`. One table, tested pair by pair, including every
pair that must be refused.

| from | may become |
| :- | :- |
| pending_payment | confirmed, payment_failed, refund_required, expired, cancelled |
| confirmed | cancelled |
| refund_required | cancelled |
| payment_failed | terminal |
| expired | terminal |
| cancelled | terminal |

`Status.IsLive()` returns true for `pending_payment` and `confirmed`, which is
exactly the set in `uq_booking_active`. The two are paired so a change to one
without the other fails a test rather than surfacing as a driver error.

<br>

## Package Boundaries

```
internal/booking
|
|___model.go                (Class, Booking, Event, Actor)
|___status.go               (the transition table, pure)
|___seat.go                 (LowestFreeSeat, pure, mirrors step 4 above)
|___hold.go                 (HoldIsLive, MaxHolders, HoldDeadline, pure)
|___errors.go               (the typed failures, sentinels)
|___repository.go           (the interface and its request shapes)
|___repository_postgres.go  (the transactions)
|___repository_memory.go    (the fake, same invariants under a mutex)
|___service.go              (policy: hold lifetime, cap, clock, identifiers)
```

The four pure files hold every rule that can be read without a database in front
of it. That is deliberate: a reviewer can check the seat rule and the hold
boundary by reading forty lines.

`Settings` carries the clock and the identifier source, defaulting to the real
ones. That is what lets a hold deadline be asserted exactly rather than
approximately, with no test that sleeps.

<br>

## Nullable Columns In Go

Three columns are nullable and are carried as zero values rather than pointers,
because each has a zero that cannot be a real value.

| column | zero | why it is unambiguous |
| :- | :- | :- |
| seat_no | 0 | seats are numbered from 1, and a check constraint enforces it |
| hold_expires_at | the zero time | a booking that is not holding has no deadline |
| confirmed_at | the zero time | a booking that never confirmed has no instant |

Every caller is therefore free of a nil check.

<br>

## Errors

The package reports sentinel errors. The http layer maps them to a status and a
code, so nothing above this package matches on wording.

| sentinel | means | maps to, phase 6 |
| :- | :- | :- |
| `ErrInvalidRequest` | refused before anything was read or written | 400 `invalid_request` |
| `ErrClassNotFound` | no class with that id | 404 |
| `ErrStudentNotFound` | no student with that id | 404 |
| `ErrBookingNotFound` | no booking with that id | 404 |
| `ErrAlreadyBooked` | a live booking exists for this child and class | 409 `already_booked` |
| `ErrTooManyHolds` | this parent is at the hold cap | 409 `too_many_holds` |
| `ErrClassFull` | capacity plus allowance is taken | 409 `class_full` |
| `ErrSeatLost` | paid, but every seat was gone. Booking left in refund_required | 409 `seat_lost` |
| `ErrNotHolding` | the booking is not pending_payment, nothing to confirm | 409 |
| `ErrInvalidTransition` | the move is not in the state machine | 409 |

No message carries an identifier or a name, and a test asserts that, because
these strings reach a log and a log gets pasted into a chat window.

Driver errors are translated rather than passed through, so nothing above this
package depends on Postgres error codes:

| code | constraint | becomes |
| :- | :- | :- |
| 23505 | uq_booking_active | `ErrAlreadyBooked` |
| 23505 | uq_seat_taken | `ErrSeatLost`, a backstop that should never fire |
| 23503 | any class foreign key | `ErrClassNotFound` |
| 23503 | otherwise | `ErrStudentNotFound` |
| 22P02 | text that is not a uuid | `ErrInvalidRequest` |

<br>

## Seed Data

`migrations/0002_seed.sql`. Fixed ids under the prefix
`0192a000-0000-7000-8000-0000000000XX`, obviously fake names, and `example.test`
addresses.

| class | state | proves |
| :- | :- | :- |
| ...021 open | 4 seats, 0 confirmed | a class with available seats |
| ...022 nearly full | 4 seats, 3 confirmed | the capacity boundary |
| ...023 duplicate target | 1 confirmed for a known child | a duplicate attempt |
| ...024 failure target | seeded to decline payment | payment failure never reaching the roster |
| ...025 race class | 1 seat, 0 confirmed | the last-seat race script |

Start times are relative to the seed run, so a seeded class is always in the
future no matter when the repository is cloned.

<br>

## Test Tiers

| command | runs |
| :- | :- |
| `go test ./...` | unit, edge, integration, behaviour, all against fakes |
| `go test -tags=containers ./...` | the same contract against real Postgres, plus the race simulations |

| simulation | tier | asserts |
| :- | :- | :- |
| 1, duplicate booking | behaviour, fake | one row for that child and class, the second attempt leaves nothing behind |
| 3, capacity boundary | behaviour, fake | 4 confirmed with seats 1 to 4, the fifth parent in refund_required |
| 4, one free seat | proof, real | 10 parallel confirms, exactly 1 confirmed, 9 in refund_required |
| 5, empty four seat class | proof, real | 20 parallel confirms, exactly 4 confirmed, seats 1 to 4, no gaps and no repeats |

Simulation 5 also proves that `uq_seat_taken` never fired: every loser ends in
`refund_required`, and a unique violation would have rolled its transaction back
and left that booking in `pending_payment` instead.
