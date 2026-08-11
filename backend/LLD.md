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

`ix_job_queue_claimable` on `(run_after, id)` is what keeps a busy queue from
scanning the table, because that pair is exactly what the claim orders by.

There is no foreign key on the payload, on purpose. See ADR-024.

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

## The Payment Attempt

`internal/payment`. The charge settles first and the seat is decided afterwards,
which is why this package never imports `internal/booking`. The two are wired
together by `internal/checkout`, which is the only package that imports both.

```
booking id, amount, idempotency key present?   -> ErrInvalidRequest
amount greater than zero, currency valid?      -> ErrInvalidAmount, ErrInvalidCurrency
key non-empty, bounded, printable?             -> ErrInvalidIdempotencyKey
open the attempt row, status initiated         -> replay if the key is taken
charge the provider                            -> settled, declined, or unreachable
settle the row with the answer
```

The row is written before the provider is called, on purpose. A process that
dies mid-charge leaves something for a replay to find, where the other order
would leave a charge nobody has a record of.

| provider answer | attempt row | what the caller gets |
| :- | :- | :- |
| settled | succeeded, provider reference kept | the attempt, no error |
| declined | failed, reason kept, no reference | the attempt and `ErrDeclined` |
| unreachable | left initiated | the attempt and `ErrProviderUnavailable` |

A replay never reaches the provider. It returns the stored answer, so two
identical calls produce one charge and the same response body. See ADR-021 for
the three replay cases, and ADR-020 for how the mock decides.

The amount check exists twice by design. `check (amount_cents > 0)` in the
schema is the backstop that holds even against a manual sql edit, and
`Amount.Validate` is the readable answer a caller can act on. A constraint
violation reaching a parent as a database error is not a message.

<br>

## The Job Queue

`internal/queue`. It knows about ids, kinds, opaque payloads, leases, and
attempts. It has never heard of a booking, a seat, or money, and that is what
lets the whole of it be tested without any of them.

Four rules, and every one of them is in the claim statement:

| rule | how |
| :- | :- |
| a job runs no earlier than it was scheduled for | `run_after <= now` |
| a claimed job is invisible to another worker | `locked_until is null or locked_until <= now` |
| a job that keeps failing stops rather than loops | `attempts < max attempts` |
| finding a job and owning it is one step | `for update skip locked`, inside one statement with the update |

```
claim   -> lease until now + lease, attempts + 1, oldest scheduled first
complete-> delete the row
release -> clear the lease, push run_after to the backoff, keep the attempt
depth   -> ready, claimed, and parked, counted in one scan of the same instant
```

The three depth numbers are separate rather than one total, because they mean
different things to whoever is looking. Ready rising means the worker is behind.
Claimed rising means jobs are slow. Parked rising means something is broken and
no amount of waiting will fix it. Counting them in three queries would let them
describe a table that never existed.

A payload is validated as a json object before either implementation is reached,
so the fake refuses exactly what `jsonb` refuses.

<br>

## The Worker

`internal/worker`. The seam where the queue meets the domain, and the only
package that imports both.

```
poll  -> claim a batch
        no handler for the kind?  -> release, report
        handler returned nil?     -> delete the row
        handler returned an error?-> release with a backoff, report
        at the attempt cap?       -> report that it is parked
wait  -> a full batch polls again at once, anything less waits
```

**expire_hold.** Reads the booking id from the payload and calls
`booking.Service.Expire`, which locks the row and judges the deadline against
the service clock rather than against the job. Three answers, three outcomes:

| answer | outcome | why |
| :- | :- | :- |
| nil | the job is done | the hold was released and the slot is free again |
| `ErrNotHolding` | the job is done | the booking already moved on, so arriving late was the job doing its work |
| `ErrHoldStillLive` | handed back | taking the slot now would take it from somebody still paying |

**reconcile_refund.** Reads the booking, and does nothing unless it is in
`refund_required`. That status read is the replay guard for the ordinary case.
Then: refund first, close second, never the other way round. A booking closed
before the money moved would look settled while the parent is still out of
pocket, and nothing would come back to fix it.

The awkward case is a refund that settled and a close that failed. The booking
still says it is owed, so the retry comes straight back to the refund, and the
idempotency key is what stops the money moving twice. See ADR-025.

**The metrics listener** serves `/healthz` and `/metrics` on 9002 and nothing
else, because the worker accepts no request that changes anything. The api
serves `/readyz` and `/version` as well, from `internal/operations`, and the
worker does not: readiness answers whether traffic should be sent somewhere, and
nothing sends traffic here.

| metric | kind | reads |
| :- | :- | :- |
| `queue_depth{state="ready"}` | gauge | jobs due, unclaimed, with attempts left |
| `queue_depth{state="claimed"}` | gauge | jobs a worker is holding |
| `queue_depth{state="parked"}` | gauge | jobs that have stopped |
| `worker_jobs_claimed_total` | counter | jobs this process has taken |
| `worker_jobs_completed_total` | counter | jobs it finished and removed |
| `queue_job_failed_total` | counter | jobs it handed back |

A scrape that cannot read the queue answers 503 rather than publishing zeroes,
because zeroes read as a healthy empty queue and no alert would fire.

<br>

## Verifying An Access Token

`internal/auth/jwt.go`. The order is the design, and it is fixed by a test for
each step:

```
1. split on '.', refuse anything that is not three segments
2. decode the header, refuse any alg but HS256 and any typ but JWT
3. recompute the mac over "header.payload", compare with hmac.Equal
4. decode the payload, refuse a claim set this service would not have issued
5. compare exp with now, inclusive
```

Step 2 is what refuses the two classic forgeries: `alg: none` with an empty
signature segment, and a token signed with a key the service publishes.

Step 3 uses `hmac.Equal` rather than `==`. A string comparison returns as soon
as two bytes differ, and that timing is enough to recover a signature one byte
at a time given enough attempts.

Steps 4 and 5 are in that order and not the other way round. A forged token that
is also expired reports `ErrTokenInvalid`, because telling its holder that the
clock is the only remaining problem tells them what to fix next.

`ErrTokenExpired` is separate from `ErrTokenInvalid` for exactly one reason: it
is the only failure the client acts on rather than shows. It maps to
`token_expired`, and the frontend refreshes and retries once, invisibly.

<br>

## The Rotation Transaction

`internal/auth/store_postgres.go`. One transaction, and the lock is taken before
anything is judged.

```
begin
  select ... from refresh_tokens where token_hash = $1 for update
  -- no row            -> ErrTokenNotFound
  -- revoked_at is set -> revoke the whole family, commit, ErrTokenReused
  -- expires_at <= now -> ErrTokenExpired
  update refresh_tokens set revoked_at = now where id = <presented>
  insert the successor, same parent, same family
commit
```

Read it as a race and the lock's job is obvious. Two requests carry the same
token. The first locks the row. The second waits here rather than reading
around it. The first revokes and inserts, then commits. The second now reads a
revoked row and reports reuse.

Without the lock both read a live row, both rotate, and one stolen token quietly
becomes two working sessions with reuse detection that never fires.

The family revocation is committed even though the caller is refused. A rollback
there would detect the theft and then undo the response to it.

Three answers, three different things:

| answer | means | what it is not |
| :- | :- | :- |
| `ErrTokenNotFound` | this service never issued that token, or the row is long gone | not a theft |
| `ErrTokenExpired` | the parent was away longer than the refresh lifetime | not a theft, and deliberately not counted as one |
| `ErrTokenReused` | the token was already spent, and the chain is now revoked | the one worth alerting on |

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

internal/payment
|
|___attempt.go              (Attempt, Status)
|___amount.go               (Amount, the money rules, pure)
|___idempotency.go          (the key rules, pure)
|___errors.go               (the typed failures, sentinels)
|___provider.go             (the interface, its request and its three answers)
|___provider_mock.go        (the deterministic provider, see ADR-020)
|___repository.go           (the interface and its request shapes)
|___repository_postgres.go  (the writes, and uq_payment_idempotency)
|___repository_memory.go    (the fake, one attempt per key under a mutex)
|___refund.go               (sending a settled charge back, and the key that guards it)
|___service.go              (the order: validate, open, charge, settle)

internal/queue
|
|___job.go                  (Job, Kind, and the three questions asked of a row)
|___payload.go              (the booking payload and its encoding)
|___errors.go               (the typed failures, sentinels)
|___queue.go                (the interface and its request shapes)
|___queue_postgres.go       (the skip-locked claim, see ADR-011)
|___queue_memory.go         (the fake, the same rules under a mutex)

internal/worker
|
|___errors.go               (the typed failures, sentinels)
|___handler.go              (the Handler interface and the kind registry)
|___runner.go               (claim, dispatch, complete or release)
|___expire_hold.go          (the expiry handler)
|___reconcile_refund.go     (refund, then close)
|___counters.go             (what this process has done)
|___exposition.go           (those numbers in the Prometheus text format)
|___listener.go             (healthz and metrics on 9002)

internal/auth
|
|___claims.go               (the six claims, and what a valid set is)
|___jwt.go                  (HS256 sign and verify, see ADR-027)
|___token.go                (the opaque refresh token, and its sha256)
|___errors.go               (the typed failures, sentinels)
|___failure.go              (what each failure looks like on the wire)
|___store.go                (the refresh token interface and its request shapes)
|___store_postgres.go       (the rotation transaction, see ADR-029)
|___store_memory.go         (the fake, the same rules under a mutex)
|___directory.go            (who exists: the interface)
|___directory_postgres.go   (lower(email) and the children read)
|___directory_memory.go     (the fake, the same normalisation)
|___denylist.go             (withdrawn access tokens: the interface)
|___denylist_memory.go      (the fake, per process, see ADR-031)
|___counters.go             (rotations, reuse, refused sign ins)
|___service.go              (sign in, sign out, the session read)
|___refresh.go              (rotation, and counting what the store reports)
|___cookie.go               (the two cookies, their flags and their paths)
|___middleware.go           (verify, denylist, role, origin)
|___handler.go              (the four routes)

cmd/worker
|
|___main.go                 (the composition root, and the only place that wires all of it)
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

| sentinel | means | maps to |
| :- | :- | :- |
| `ErrInvalidRequest` | refused before anything was read or written | 400 `invalid_request` |
| `ErrClassNotFound` | no class with that id | 400 `invalid_request`, see ADR-038 |
| `ErrStudentNotFound` | no student with that id | 400 `invalid_request`, see ADR-038 |
| `ErrBookingNotFound` | no booking with that id | 400 `invalid_request`, see ADR-038 |
| `ErrAlreadyBooked` | a live booking exists for this child and class | 409 `already_booked` |
| `ErrTooManyHolds` | this parent is at the hold cap | 409 `too_many_holds` |
| `ErrClassFull` | capacity plus allowance is taken | 409 `class_full` |
| `ErrSeatLost` | paid, but every seat was gone. Booking left in refund_required | 409 `seat_lost` |
| `ErrNotHolding` | the booking is not pending_payment, nothing to confirm | 409 `invalid_request` |
| `ErrHoldStillLive` | the deadline has not passed, so there is nothing to expire | never, the worker owns it |
| `ErrInvalidTransition` | the move is not in the state machine | 409 `invalid_request` |

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

`internal/payment` names its own failures for the same reason, including its own
`ErrBookingNotFound`, so a charge never depends on the package that owns seats.

| sentinel | means | maps to |
| :- | :- | :- |
| `ErrInvalidRequest` | refused before anything was read or written | 400 `invalid_request` |
| `ErrInvalidAmount` | the charge was zero or below | 400 `invalid_request` |
| `ErrInvalidCurrency` | the code is not three capital letters | 400 `invalid_request` |
| `ErrInvalidIdempotencyKey` | empty, too long, or not a header token | 400 `invalid_request` |
| `ErrBookingNotFound` | no booking with that id | 400 `invalid_request` |
| `ErrAttemptNotFound` | no attempt with that id | 400 `invalid_request` |
| `ErrDeclined` | the provider said no, no money moved | 402 `payment_declined` |
| `ErrProviderUnavailable` | the provider never answered, nobody knows | 503 `dependency_unavailable` |
| `ErrAttemptPending` | an earlier call with this key never settled | 503 `dependency_unavailable` |
| `ErrIdempotencyConflict` | this key was used for a different charge | 400 `invalid_request` |
| `ErrNothingToRefund` | no settled charge stands against this booking | 409 |
| `ErrAlreadySettled` | the row is append only from settlement | 409 |

| code | constraint | becomes |
| :- | :- | :- |
| 23505 | uq_payment_idempotency | a replay, or `ErrIdempotencyConflict` on a different amount |
| 23503 | the booking foreign key | `ErrBookingNotFound` |
| 23514 | payment_attempts_amount_positive | `ErrInvalidAmount` |
| 22P02 | text that is not a uuid | `ErrInvalidRequest` |

`internal/queue` and `internal/worker` name their own too. Neither is raised on
a request path, because the queue is scheduled from one and consumed from
somewhere else entirely. They reach a log, and the depth reaches the operator
queue view.

| sentinel | package | means |
| :- | :- | :- |
| `ErrInvalidRequest` | queue | refused before anything was read or written |
| `ErrUnknownKind` | queue | the kind is not one the check constraint allows |
| `ErrInvalidPayload` | queue | the payload is not a json object, or carries no booking |
| `ErrJobNotFound` | queue | no job with that id, which is also what a second completion gets |
| `ErrDuplicateJob` | queue | a job already carries that id |
| `ErrUnknownKind` | worker | a handler was registered for a kind this service does not run |
| `ErrHandlerMissing` | worker | a kind has nothing to run it, refused at construction |
| `ErrHandlerAlreadyRegistered` | worker | a second registration would silently replace the first |
| `ErrInvalidSettings` | worker | a policy value the runner cannot work with |

| code | constraint | becomes |
| :- | :- | :- |
| 23505 | the job_queue primary key | `ErrDuplicateJob` |
| 23514 | job_queue_kind_allowed | `ErrUnknownKind` |
| 22P02 | text that is not a uuid | `ErrJobNotFound`, or `ErrInvalidRequest` where an absent row is not an answer |

`internal/auth` names its own too, and it is the one package that already maps
them itself, in `failure.go`, because it serves routes before `internal/httpx`
exists.

| sentinel | means | maps to |
| :- | :- | :- |
| `ErrInvalidRequest` | refused before anything was read or written | 400 `invalid_request` |
| `ErrNoSuchParent` | that email is not a seeded account | 400 `invalid_request`, see ADR-032 |
| `ErrOriginRefused` | a write arrived from another site | 400 `invalid_request` |
| `ErrTokenExpired` | the access token aged out, refresh and retry | 401 `token_expired` |
| `ErrTokenInvalid` | bad shape, wrong algorithm, failed signature, or a missing claim | 401 `token_invalid` |
| `ErrTokenNotFound` | no stored refresh token carries that hash | 401 `token_invalid` |
| `ErrNotAuthenticated` | the route needs an identity and there is none | 401 `token_invalid` |
| `ErrTokenReused` | a spent refresh token came back, the family is now revoked | 401 `token_reused` |
| `ErrForbiddenRole` | a real identity on a route that is not for it | 403 `forbidden_role` |
| `ErrDuplicateToken` | a refresh token hash repeated, which means the randomness did | 500 `internal_error` |

The four token failures collapse into one code on purpose. Telling a caller
which part of a token failed tells an attacker which part to fix next. The one
exception is expiry, and it is an exception because the client acts on it.

| code | constraint | becomes |
| :- | :- | :- |
| 23505 | uq_refresh_token_hash | `ErrDuplicateToken` |
| 22P02 | text that is not a uuid | `ErrTokenNotFound`, or `ErrInvalidRequest` where an absent row is not an answer |

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

## The Http Surface

`internal/httpx`. It owns no rule. Every decision it makes was made somewhere
that can be tested without a socket, and its job is to ask the right thing in
the right order and turn the answer into a status and a code.

```
backend
|
|___/internal
    |___/httpx
    |   |___paths.go                       (the route constants, shared with the frontend)
    |   |___errors.go                      (every typed failure to a status and a code)
    |   |___response.go                    (the envelope, and the three cache policies)
    |   |___requestid.go                   (minted here, never read from the request)
    |   |___recover.go                     (a panic costs one request, not the process)
    |   |___botcheck.go                    (honeypot, fill timer, challenge)
    |   |___ratelimit.go                   (which bucket, and what an outage means)
    |   |___conditional.go                 (etag, 304, and invalidation)
    |   |___ownership.go                   (is this child, this booking, yours)
    |   |___counters.go                    (what phase 7 exports)
    |   |___middleware.go                  (the chain, in the order it is written)
    |   |___router.go                      (every route, and what guards it)
    |   |___handler_classes.go
    |   |___handler_students.go
    |   |___handler_bookings.go
    |   |___handler_payments.go
    |   |___handler_roster.go
    |   |___handler_admin.go
    |
    |___/operations
    |   |___health.go                      (liveness, touches nothing)
    |   |___readiness.go                   (the probes, and what each failure means)
    |   |___version.go                     (stamped at link time, never read from the environment)
    |   |___handler.go
    |
    |___/catalogue                         (the class list, from the replica)
    |___/checkout                          (the order a hold and a payment happen in)
    |___/roster                            (who owns a seat, from the replica, admin only)
    |___/cache                             (etag, stored body, version counter)
    |___/ratelimit                         (the token bucket, and two implementations)
    |___/captcha                           (the provider's shape, and a mock)
```

**The middleware chain, in order.** Chained the other way round it would still
compile and would still answer, which is exactly why the order is written down:

| position | what | why there |
| :- | :- | :- |
| 1 | request id | everything below can fail, and a failure with no id cannot be found |
| 2 | panic recovery | a handler falling over costs one request rather than the process |
| 3 | origin check, writes only | a cookie session means the browser attaches the token itself |
| 4 | authenticate | the rate limit needs to know whose bucket to spend |
| 5 | role, admin routes only | wraps authenticate, so the role cannot be checked without the token |
| 6 | rate limit | last, so the bucket it spends is the right one |

**The failure envelope.** One shape, every route, so the client switches on a
code and never parses prose.

```json
{
    "error": {
        "code": "class_full",
        "message": "this class filled while you were choosing",
        "retry_after_seconds": 0
    }
}
```

Three fields are optional and are omitted rather than sent as null:

| field | on | why |
| :- | :- | :- |
| `retry_after_seconds` | `rate_limited` only | a client told to wait zero seconds retries into the same wall |
| `request_id` | `internal_error` only | it is the whole of what that code tells anybody |
| `booking_id` | `already_booked` only | it turns a duplicate notice into a link |

**Checkout, step by step.** `internal/checkout` is the only package that imports
booking, payment, and queue. Everything in this list is a decision that belongs
to nobody else:

```
the amount is compared to the price this service owns   -> ErrInvalidAmount
the provider settles                                    -> declined, unreachable, or settled
a decline ends the booking                              -> payment_failed, hold released
the confirm transaction runs                            -> the seat, or ErrSeatLost
a lost seat queues the refund                           -> refund_required, job written
```

An unreachable provider changes nothing about the booking, because nobody knows
whether money moved and any status written would be a guess.

<br>

## Test Tiers

| command | runs |
| :- | :- |
| `go test ./...` | unit, edge, integration, behaviour, all against fakes |
| `go test -tags=containers ./...` | the same contract against real Postgres, plus the race simulations |

| simulation | tier | asserts |
| :- | :- | :- |
| 1, duplicate booking | behaviour, fake | one row for that child and class, the second attempt leaves nothing behind |
| 2, payment failure | behaviour, fake | one failed attempt, no provider reference, no seat, the roster stays empty |
| 3, capacity boundary | behaviour, fake | 4 confirmed with seats 1 to 4, the fifth parent in refund_required |
| 4, one free seat | proof, real | 10 parallel confirms, exactly 1 confirmed, 9 in refund_required |
| 5, empty four seat class | proof, real | 20 parallel confirms, exactly 4 confirmed, seats 1 to 4, no gaps and no repeats |
| 7, hold expiry | behaviour, fake | the booking becomes expired, the slot frees up, one worker gets the job and a second gets nothing |
| 8, refund reconciliation | behaviour, fake | the booking becomes cancelled, the refund reference is recorded, a replay refunds nothing more |
| 9, idempotent replay | behaviour, fake | one payment_attempts row, one charge, both calls answer identically |
| one key under load | proof, real | 10 parallel calls with one key, one row, nine replays, uq_payment_idempotency holding |
| two workers, one queue | proof, real | 8 parallel workers over 24 jobs, every job claimed exactly once, skip locked holding |
| 13, refresh rotation and reuse | behaviour, fake | one token works once, reuse revokes the family, the honest holder is signed out too, and the reuse is counted |
| one refresh token under load | proof, real | 8 parallel rotations of one token, one successor, seven reported as reuse |
| 10, bot prevention layers | behaviour, fake | one case per branch, each with its own typed code, and a flood that empties the bucket without writing a booking |
| 11, conditional request | behaviour, fake | a matching tag answers 304 with no body and no reader call, and an invalidation changes the tag even when the body is identical |
| 12, readiness reflects reality | behaviour, fake | a downed replica stays in rotation, a downed primary or Redis does not, liveness stays 200, and no body names a host |
| read routing | proof, real | the primary is not in recovery, the replica is, and the replica refuses a write |

Simulation 5 also proves that `uq_seat_taken` never fired: every loser ends in
`refund_required`, and a unique violation would have rolled its transaction back
and left that booking in `pending_payment` instead.
