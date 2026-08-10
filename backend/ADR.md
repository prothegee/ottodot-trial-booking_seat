# Backend Decision Records

Numbered records, each with the context that forced a choice, the choice itself,
and what living with it costs. A record is written when the decision is taken,
not afterwards, so the reasoning survives even when nobody remembers the
conversation.

Records for parts that are not built yet are still here, because the decision
was taken during planning and the code follows it rather than the other way
round. Each one says which phase implements it.

| status | meaning |
| :- | :- |
| accepted | decided and implemented |
| planned | decided, implementation in a later phase |

<br>

## ADR-001: Go with the standard library, no web framework

**Status:** accepted, phase 1

**Context.** The service is small, its routing is a dozen paths, and the
interesting part of it is a database transaction rather than an http layer.

**Decision.** Go with `net/http` from the standard library. No framework.

**Consequences.** More routing and middleware written by hand. In exchange,
nothing about the request path is hidden behind a framework's conventions,
which matters when the point of the exercise is to show exactly where each
check happens.

<br>

## ADR-002: UUIDv7 identifiers, minted in the application

**Status:** accepted, phase 2

**Context.** Every table needs a primary key. Serial keys leak volume and make
merging environments awkward. UUIDv4 keys scatter index writes across random
pages. Postgres 18 can generate UUIDv7 itself, which would tie the schema to
that major version.

**Decision.** UUIDv7 generated in Go, in `internal/identifier`, written in
about forty lines rather than pulled from a dependency.

**Consequences.** One more file to own and test. In exchange the ids sort
roughly by creation time, so the primary key index keeps appending at its right
hand edge, the schema needs no extension and no database side default, and the
package that mints every primary key in the system has no third party code in
it.

<br>

## ADR-003: The last seat is decided by a row lock, counted under that lock

**Status:** accepted, phase 2

**Context.** Two parents pay for the last seat at nearly the same instant. The
naive implementation counts the confirmed bookings, sees a seat free, and takes
it. Two callers can do that at once and both succeed.

**Decision.** The confirm transaction takes `select ... for update` on the class
row, then finds the lowest free seat under that lock, then writes. Two confirms
for the same class cannot overlap.

**Alternatives rejected.**

| alternative | why not |
| :- | :- |
| a denormalized `seats_taken` counter with a conditional update | one atomic statement is appealing, but the counter can drift from the bookings table, and then the roster and the counter disagree |
| check then insert, no lock | this is the bug the exercise is about |
| a Redis distributed lock | the database lock is already correct and durable, and a lease that expires mid-transaction lets two writers proceed |
| serializable isolation | correct, but it turns the failure into a retry loop the caller has to own, for a contention level four seats never reach |

**Consequences.** Confirms serialize per class. At four seats that costs
nothing, and the lock is held for one short transaction. It would need
revisiting for a class type with a much larger capacity.

<br>

## ADR-004: The unique seat index is a backstop, not the mechanism

**Status:** accepted, phase 1

**Context.** The lock above is what makes the system correct. Something should
still be true if that code is ever wrong.

**Decision.** A partial unique index on `(class_id, seat_no)` where `seat_no` is
not null, kept as a backstop.

**Consequences.** One more index to maintain. In exchange the invariant survives
a buggy code path, a manual sql edit, and a future writer nobody has thought of.
The upper bound on a seat number is still enforced in the transaction, because a
check constraint cannot read `trial_classes.capacity`. That gap is stated openly
rather than closed by hardcoding `check (seat_no between 1 and 4)`, which would
freeze capacity forever.

<br>

## ADR-005: Holding a place is allowed beyond capacity

**Status:** accepted, phase 2

**Context.** The brief requires this sequence: parent A picks the last seat and
moves to payment, then parent B picks the same seat. For B to pick a seat A is
holding, selecting must not block.

**Decision.** A class carries `hold_allowance`, defaulting to 2. The number of
parents who may sit on the payment screen at once is `capacity + hold_allowance`.

**Consequences.**

| allowance | effect |
| :- | :- |
| 0 | nobody is ever charged for a seat they cannot get, and a seat sits idle behind a parent who walked away |
| 2 | seats fill reliably, and a few parents pay and are refunded |

The strict behaviour is a data change rather than a code change, because the
allowance is a column. The confirm transaction is unaffected either way, it
remains the sole authority.

<br>

## ADR-006: Paid but seatless is its own status

**Status:** accepted, phase 1

**Context.** Two failures look similar on a screen and are opposite in the
ledger: a payment the provider declined, and a payment that settled for a seat
that was gone.

**Decision.** `payment_failed` and `refund_required` are separate statuses.

**Consequences.** One more enum value. In exchange an operator can tell at a
glance whether money moved, and the refund worklist is a status filter rather
than a join against payment attempts.

<br>

## ADR-007: The payment settles first, the seat is decided second

**Status:** accepted, phase 2

**Context.** Either the seat is taken before charging, or the charge happens
before the seat is decided. Both leave a bad case somewhere.

**Decision.** The payment settles, then the confirm transaction runs.

**Consequences.** A parent who loses the race has already been charged, which is
why ADR-006 exists and why a reconciliation job does. The alternative, taking
the seat first, would hold a confirmed seat against a payment that might never
settle, and that failure is worse: it seats a child nobody paid for and it needs
a timeout to unwind.

<br>

## ADR-008: The confirm transaction ignores the hold deadline

**Status:** accepted, phase 2

**Context.** A parent's ten minutes can run out while their payment is
settling. The transaction could refuse them.

**Decision.** It does not look at the deadline. If a seat is free, the parent
who paid gets it.

**Consequences.** Handing over a seat that is genuinely free beats refunding
somebody because a countdown ran out a moment earlier. A hold that the worker
has already expired fails the status check instead, which is the honest signal:
the booking left `pending_payment` and there is nothing to confirm.

There is a related asymmetry worth stating, because it looks like an
inconsistency and is not. A lapsed hold stops occupying an allowance slot, so a
parent who walked away does not keep a class looking full. The same lapsed hold
still blocks that child from booking that class twice, because that rule mirrors
the `uq_booking_active` index and the index does not know about time.

<br>

## ADR-009: The audit trail is written inside the transaction that changes the status

**Status:** accepted, phase 2

**Context.** Every status change needs a record an operator can read. It could
be an application log, a write after the fact, or part of the transaction.

**Decision.** A `booking_events` row is inserted in the same transaction as the
status change.

**Consequences.** One more insert per transition. In exchange the trail cannot
disagree with the booking it describes: either both are there or neither is. A
log line would be lost with the process, and a write after the fact would be
lost in exactly the failure worth recording.

The seat-lost outcome is committed rather than rolled back for the same reason.
That row is the only record telling an operator that money moved and has to move
back.

<br>

## ADR-010: Postgres holds all state, Redis is cache and rate limit only

**Status:** planned, phases 6 and 7

**Context.** Redis is faster and tempting for seat counts, locks, and the job
queue.

**Decision.** Postgres holds every piece of state that a decision depends on,
including the job queue. Redis holds cached responses, rate limit buckets, and
the token denylist, none of which are a source of truth.

**Consequences.** Lower queue throughput than a purpose-built broker. In
exchange Redis can be down without any invariant being at risk, and the queue
survives a restart because it is rows in a table.

<br>

## ADR-011: The job queue is Postgres with FOR UPDATE SKIP LOCKED

**Status:** planned, phase 4

**Context.** Expiring holds and reconciling refunds are background work. They
must not be lost.

**Decision.** A `job_queue` table, claimed with `for update skip locked`.

**Consequences.** Lower throughput than Redis Streams, and a polling worker
rather than a pushed one. In exchange there is one datastore to back up, and a
job that was mid-flight when a worker died is picked up by the next one.

<br>

## ADR-012: One asynchronous replica, promoted by hand

**Status:** accepted, phase 1

**Context.** A single database is a single point of failure, and synchronous
replication costs write latency on every confirm.

**Decision.** Streaming replication to one replica, asynchronous, with manual
promotion. Every deciding read goes to the primary. The replica serves advisory
reads only.

**Consequences.** A primary crash can lose the newest confirmed bookings, and
reconciliation against the payment provider is what would catch that. Automatic
failover was rejected because a correct one needs a quorum, and a single-machine
demo that claims to have failover without one would be a claim it cannot back.

<br>

## ADR-013: Authentication is a JWT access token plus a rotating opaque refresh token

**Status:** planned, phase 5

**Context.** The client must not be able to read a token, and a revoked session
must actually stop working.

**Decision.** A short-lived JWT for access, an opaque rotating token for
refresh, both in HttpOnly cookies. The refresh token is stored only as a hash,
one row per token, so presenting a rotated one is detectable as reuse and
revokes the whole family.

**Consequences.** Cross-site request forgery has to be handled, which the Origin
check on mutations does. Revocation lags by at most the access token lifetime.
In exchange no script in the page can read a token, and the payload carries no
email and no name, because a JWT payload is base64 rather than encryption.

<br>

## ADR-014: Each stack is self-contained, with no compose file at the root

**Status:** accepted, phase 1

**Context.** A single root compose file wiring both stacks is convenient and
makes either stack impossible to split out later without untangling it.

**Decision.** `backend/compose.yml` and `frontend/compose.yml`, each with its
own container network and paths relative to its own directory. Nothing shared at
the root. Root `scripts/` is the only exception, and it earns that by being
where cross-stack work belongs.

**Consequences.** Two files to start instead of one, which `scripts/stack_up.sh`
covers without becoming a third definition. In exchange either stack is
deployable on its own, and neither can quietly grow a dependency on the other.

<br>

## ADR-015: Container state is a bind mount whose parent is mounted

**Status:** accepted, phase 1

**Context.** The same compose file has to work under rootful Docker and rootless
Podman. In one the container user is the host user, in the other it maps to a
subordinate id. Chowning on the host is right for one and wrong for the other.

**Decision.** Mount the parent of the data directory at mode 0777 and let the
container create its own data directory inside, as its own user. Postgres 18
already expects that shape, since its declared volume is `/var/lib/postgresql`
with a version stamped directory inside.

**Consequences.** One open directory holding local development state. In
exchange one compose file works under both runtimes with no runtime specific
flag, and a wipe is one visible path rather than a volume name to look up.

<br>

## ADR-016: Configuration secrets are a type that masks itself

**Status:** accepted, phase 1

**Context.** A connection url carries a password. Logging a config struct is the
most ordinary thing in the world.

**Decision.** A `Secret` type that renders a mask through `%v`, `%s`, `%#v`, and
json. Reading the real value takes an explicit `Reveal()` call.

**Consequences.** One call at each point of use. In exchange every read is
visible in a review, and the default behaviour of printing a struct is safe
rather than dangerous.

<br>

## ADR-017: The repository is an interface with two implementations, held to one contract

**Status:** accepted, phase 2

**Context.** The four fast tiers need to run with nothing installed. The
last-seat rule needs a real database. Both are true at once.

**Decision.** `booking.Repository` is an interface. `repository_postgres.go` is
the real one, `repository_memory.go` is a fake with the same invariants under a
mutex, and one shared contract suite runs against both.

**Consequences.** Two implementations to keep honest. The contract suite is what
makes that safe: a fake that enforces the invariant correctly while the sql does
not would otherwise pass every fast test, and that is exactly the failure this
guards against.

<br>

## ADR-018: Containers are allowed in continuous integration

**Status:** planned, phase 9

**Context.** The proof tier needs Postgres. Running it only locally means the
one test that proves the headline claim is the one test nobody runs.

**Decision.** The proof job runs containers on every pull request.

**Consequences.** Slower pull requests. In exchange the claim this whole
exercise rests on is checked by the build rather than by a person remembering
to.

<br>

## ADR-019: The failure path is proven by making it fail

**Status:** planned, phase 7

**Context.** An alert nobody has watched fire is a query somebody wrote once.

**Decision.** A runtime fault surface with named injection points, guarded four
ways: development environment, an off-by-default flag, an admin role, and routes
that are not registered otherwise. The api refuses to start if the flag is on
outside development. Alert rules are proven with `promtool test rules` against a
synthetic series.

**Consequences.** A handful of guarded call sites in production code. In
exchange the demo needs one command rather than a rebuild, and the alert is
shown arriving rather than described.

The startup guard is already enforced and tested in the configuration loader.

<br>

## ADR-020: The payment provider is a deterministic mock behind one interface

**Status:** accepted, phase 3

**Context.** The brief needs a payment step and no real provider is in scope. A
mock that fails at random would make a failing test a coin toss and a recorded
demonstration a matter of luck.

**Decision.** One `payment.Provider` interface with the shape a real provider
has, and one mock behind it that decides from the request alone. The rule is the
last two digits of the amount: ending in 01 is declined, ending in 02 is a
provider that cannot be reached, everything else settles. A reference can also
be pinned to an outcome when a seeded price is fixed.

The three answers are kept apart on purpose:

| answer | money | what the booking does |
| :- | :- | :- |
| settled | moved | the confirm transaction runs next |
| declined | did not move | the booking may go to payment_failed, nothing to refund |
| provider error | nobody knows | the booking keeps the status it had |

**Consequences.** A reviewer forces any of the three with a number instead of an
account, and the same input gives the same answer every time. Swapping in a real
provider touches one file. The provider error path stays reachable and named,
which is the seam phase 7 arms as `payment.provider_error`.

<br>

## ADR-021: A replayed idempotency key returns the first answer, and never charges again

**Status:** accepted, phase 3

**Context.** A parent on a slow connection presses pay twice, or a client
retries a request whose response was lost. Charging twice is the failure that
matters most here, because it takes money and nobody notices until a statement
arrives.

**Decision.** The attempt row is written before the provider is called, under
the `uq_payment_idempotency` index on `(booking_id, idempotency_key)`. A second
call with the same key finds that row and returns its stored answer, so both
calls read identically and the provider is asked once.

Three cases follow from it, and each is answered rather than assumed:

| the stored attempt is | the replay returns |
| :- | :- |
| succeeded | the same settled attempt, no error |
| failed | the same failed attempt and `ErrDeclined` |
| still initiated | `ErrAttemptPending`, because nobody knows whether money moved |

A key already used for a different amount is refused with
`ErrIdempotencyConflict` rather than replayed, since replaying an answer that
belongs to another charge would be a lie.

**Consequences.** An attempt whose first call died mid-flight needs something
outside the request path to resolve it, and that is reconciliation work the
worker already exists for. The alternative, charging again on a replay, is the
one outcome this whole design refuses.
