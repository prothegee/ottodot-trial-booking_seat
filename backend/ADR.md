# Backend Decision Records

Numbered records, each with the context that forced a choice, the choice itself,
and what living with it costs. A record is written when the decision is taken,
not afterwards, so the reasoning survives even when nobody remembers the
conversation.

Each record says which phase implements it. Records were written while the work
was being planned rather than afterwards, so some of them carried `planned` for
several phases. Every one of them is now implemented, and the second status is
kept in the table because that is what those records read as while they waited.

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

**Status:** accepted, phases 6 and 7

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

**Status:** accepted, phase 4

**Context.** Expiring holds and reconciling refunds are background work. They
must not be lost.

**Decision.** A `job_queue` table, claimed with `for update skip locked`.

The claim is one statement, not two:

```sql
with claimable as (
    select id
    from job_queue
    where run_after <= $1
      and (locked_until is null or locked_until <= $1)
      and attempts < $2
    order by run_after, id
    limit $3
    for update skip locked
)
update job_queue
    set locked_until = $4, attempts = attempts + 1
    where id in (select id from claimable)
returning id, kind, payload, run_after, attempts, locked_until, created_at;
```

There is no instant between finding a job and owning it. A second worker
arriving mid-flight sees locked rows, steps over them, and takes different ones.

**Consequences.** Lower throughput than Redis Streams, and a polling worker
rather than a pushed one. In exchange there is one datastore to back up, and a
job that was mid-flight when a worker died is picked up by the next one.

The proof has teeth rather than being assumed: eight parallel connections
against twenty four jobs hand every job to exactly one worker. Removing
`skip locked` makes that test hang rather than fail, which is itself the
signal.

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

**Status:** accepted, phase 5

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

**Status:** accepted, phase 9

**Context.** The proof tier needs Postgres. Running it only locally means the
one test that proves the headline claim is the one test nobody runs.

**Decision.** The proof job runs containers on every pull request.

**Consequences.** Slower pull requests. In exchange the claim this whole
exercise rests on is checked by the build rather than by a person remembering
to.

<br>

## ADR-019: The failure path is proven by making it fail

**Status:** accepted, phase 7

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
mock that fails at random would make a failing test a coin toss and a
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


<br>

## ADR-022: A claim is a lease, and a lease is never cleared by the worker that holds it

**Status:** accepted, phase 4

**Context.** A worker that dies mid-job clears nothing. Something has to make
that job runnable again, and the obvious answers are all worse than they look: a
heartbeat needs a second write path, and a flag needs somebody to unset it.

**Decision.** A claim writes `locked_until = now + lease` and nothing else. A
job is held while that instant is in the future and free once it passes. Nobody
ever writes the lease back to null on the recovery path, the value simply stops
being believed.

The boundary falls one way and it is worth naming: a lease ending on this exact
instant is already lapsed, so recovery happens as early as it honestly can.

**Consequences.** A crashed worker's job is recoverable without anybody noticing
it crashed, and without a second mechanism to go wrong. The cost is that the
lease has to be comfortably longer than the slowest handler. A lease that lapses
mid-job lets a second worker start work the first is still doing, so the default
is two minutes against handlers that take milliseconds.

<br>

## ADR-023: A job that keeps failing parks, and parking is an attempt count rather than a state

**Status:** accepted, phase 4

**Context.** A job that can never succeed will be retried forever. Two workers
spend the night handing it back and forth, the log fills with the same line, and
the failure that caused it is buried.

**Decision.** The claim refuses any row whose `attempts` has reached the cap. A
job at or past it is parked: it stays in the table, it is never handed out
again, and the operator queue view in phase 6 is what surfaces it.

Parking is deliberately not a status column. The alternatives were both worse:

| approach | verdict |
| :- | :- |
| refuse rows at the attempt cap | chosen. No new column, no state to get out of step with the count, and the same number the runner uses is the one the depth query reports |
| a `parked_at` column | rejected. A second fact about the same thing, which can disagree with the count that produced it |
| push `run_after` far into the future | rejected. It hides the job from the depth query, so a queue full of broken work reports as healthy |
| delete the job | rejected. Deleting the evidence is the one thing that guarantees nobody looks at it |

**Consequences.** Five attempts at thirty seconds apart spans two and a half
minutes of trouble before a job stops, which outlasts an ordinary restart and
not much else. A job parked for a transient reason needs a hand to release it,
and until phase 6 that hand is an `update` on the attempts column.

<br>

## ADR-024: job_queue hangs from no foreign key

**Status:** accepted, phase 4

**Context.** Every job so far names a booking. A foreign key would be the
ordinary choice and would keep the table honest.

**Decision.** No foreign key. The payload is opaque `jsonb`, and the queue
package never reads inside it.

**Consequences.** A job can outlive what it refers to, and that is deliberately
the handler's problem rather than a write failure at three in the morning. The
handler reports what it found, the job runs out of attempts, and it parks where
somebody can see it. The alternative, a cascade that silently deletes queued
work when a row goes, loses work without a word.

It also keeps the queue package free of the booking package, which is what lets
the whole queue be tested with no class, no student, and no seat in sight.

<br>

## ADR-025: A refund is guarded by an idempotency key, not by a row of its own

**Status:** accepted, phase 4

**Context.** Reconciliation refunds the settled charge and then closes the
booking. Those are two writes and the second one can fail. The booking still
says `refund_required`, so the retry comes straight back to the refund, and
without a guard it sends the money a second time.

**Decision.** The refund carries an idempotency key derived from the attempt it
reverses, `refund_<attempt id>`. It is a pure function of the attempt, so the
job produces the same key on every worker, after every restart, however many
times it runs. The provider recognises the key and moves nothing.

The booking status is still read first, so in the ordinary case the provider is
not asked twice at all. The key is what covers the case the status cannot: a
refund that settled and a close that did not.

**Consequences.** This is a weaker guarantee than the one protecting a charge. A
charge is guarded by `uq_payment_idempotency`, an index this service owns. A
refund has no row and therefore no index, so the guard is the provider honouring
the key. That is what a real integration does, and it is written down here
rather than left to be discovered.

The alternative was a refund row, or two nullable columns on `payment_attempts`.
Both were rejected for the same reason: they hold a copy of the amount, the
currency, and the provider reference that the attempt row already carries, and
two places holding the same numbers is how they come to disagree. What the
refund genuinely adds, its own reference, is written to the audit trail and the
log instead.

<br>

## ADR-026: The worker refuses to expire a hold that is still standing

**Status:** accepted, phase 4

**Context.** An expiry job carries an instant chosen when it was written. By the
time it runs, the booking may have been confirmed, cancelled, or given a fresh
deadline. Acting on the job's own idea of the time would take a seat from a
parent who is still on the payment screen.

**Decision.** `Expire` locks the booking row, reads the deadline from it, and
refuses with `ErrHoldStillLive` when the deadline has not passed. The instant it
is judged against comes from the service clock, never from the job.

**Consequences.** The one mistake this job must never make is refused by the
same transaction that would have made it. A job that runs early is handed back
and tried again, which costs an attempt and nothing else.

The three answers the handler reads are deliberately distinct: `ErrNotHolding`
finishes the job, because a booking that already moved on means the job did its
work by arriving too late. `ErrHoldStillLive` hands it back. Anything else hands
it back too, because unexpected work is retried rather than thrown away.
<br>

## ADR-027: HS256, written against the standard library rather than pulled in

**Status:** accepted, phase 5

**Context.** One service signs access tokens and the same service verifies them.
A JWT library is the reflex here, and it brings a dependency whose whole job is
about a hundred lines of hmac, base64, and json, in the one place where a
dependency's defaults decide whether forged tokens are accepted.

**Decision.** HS256, implemented in `internal/auth/jwt.go` against
`crypto/hmac`, `crypto/sha256`, and `encoding/json`. The algorithm in the header
is read and then refused unless it is exactly HS256, and the signature is
compared with `hmac.Equal`.

**Consequences.** Two classic forgeries are refused by code a reviewer can see
rather than by a library setting they have to trust: `alg: none`, where the
signature segment is empty and a trusting verifier believes the payload, and a
token signed with a key the service publishes. The comparison is constant time,
because a comparison that returns early leaks the signature one byte at a time.

The cost is that this is a hand-written implementation of a specification with
known sharp edges. That is why the verification order is fixed and tested:
shape, then algorithm, then signature, then claims, then expiry. Nothing inside
the payload is believed before the signature has been checked, and expiry is
judged last so a forgery is never told that the clock is its only remaining
problem.

RS256 is the move the day a second service needs to verify without holding the
signing key. Until then an asymmetric key buys key handling and nothing else.

<br>

## ADR-028: The claim set is a closed struct with no room to grow

**Status:** accepted, phase 5

**Context.** A JWT payload is base64, not encryption. Anyone holding the token
reads it, including whoever picks it out of a shared screen. The usual way an
email ends up in a token is not a decision, it is a convenience: a
map of extra claims, and six months later somebody puts a name in it.

**Decision.** `Claims` is a struct with exactly six fields, `sub`, `role`,
`typ`, `jti`, `iat`, and `exp`, and there is no map, no pass-through, and no
`any`. `Validate` refuses a claim set missing any of them.

**Consequences.** Adding a claim is a change to a type, visible in a diff, in
the same file as the comment explaining why the list is closed. The test asserts
the exact key set of the encoded payload rather than the absence of one field,
so a seventh key fails whatever it is called.

The `jti` requirement is the one worth naming separately. A token nobody can
name cannot be denylisted, so accepting one would quietly remove logout for as
long as that token lives.

<br>

## ADR-029: Rotation is one transaction, and reuse is what the lock reveals

**Status:** accepted, phase 5

**Context.** Refresh rotation is read the token, check it is live, revoke it,
write its successor. Written that way, two requests carrying the same token both
read a live row, both rotate, and a stolen token quietly becomes two working
sessions. Reuse detection then never fires, because nothing was ever presented
twice from the store's point of view.

**Decision.** `Rotate` is one method on the store and one transaction inside it.
The presented row is taken with `select ... for update` before anything about it
is judged. The second caller waits on the lock rather than reading around it, so
by the time it looks, the row is revoked and it reports reuse.

**Consequences.** The invariant lives in the store, where both implementations
must satisfy it, and the contract suite runs against the fake and against
Postgres. The fake holds a mutex across the whole decision, which is the same
rule by a different mechanism, and the case that matters is proven separately on
real parallel connections in the containers tier.

Detecting reuse revokes the whole family, which signs the honest parent out as
well. That is the correct trade and it is stated rather than softened: two
parties hold the token, one of them stole it, and this service cannot tell
which. The parent signs in again in one click. The alternative leaves the thief
signed in.

<br>

## ADR-030: The refresh cookie is scoped to the auth group, not to the refresh route

**Status:** accepted, phase 5

**Context.** The plan scoped the refresh cookie to `/api/v1/auth/refresh`, so a
refresh token would never travel on an ordinary business call. Building sign out
found the hole in that: a cookie scoped to one path is not sent to any other
one, so `/api/v1/auth/logout` never sees the refresh token, and there is nothing
there to revoke. Sign out could withdraw the access token and leave the chain
behind it working.

**Decision.** The refresh cookie is scoped to `/api/v1/auth`. It travels on the
four auth routes and on nothing else.

**Consequences.** The reason the original scope existed is kept: the refresh
token is still never sent on a class list read, a booking, or a payment, which
is where the exposure would have mattered. What it costs is that the token also
travels on login, logout, and the session read, and the session read is called
on every application boot.

The alternative was revoking every family for that parent on sign out, which
would sign them out on their other devices as well, for a property nobody asked
for. The narrower cookie plus a wider revocation is a worse trade than a
slightly wider cookie plus an exact revocation.

<br>

## ADR-031: The access token denylist is an interface with a per-process fake for now

**Status:** accepted, phase 5

**Context.** A stateless access token cannot be withdrawn by deleting a row.
Sign out writes its `jti` to a denylist for the token's remaining life, and the
plan puts that denylist in Redis. Redis does not arrive in this stack until
phase 6, and pulling a client in early to serve one map would put a dependency
in the auth package before anything else could use it.

**Decision.** `Denylist` is an interface in `internal/auth`, with
`MemoryDenylist` behind it. The Redis implementation lands with the rest of the
Redis surface and plugs in without a caller changing.

**Consequences.** Stated plainly rather than buried: the memory denylist binds
the process that served the sign out and no other. With one api process, which
is what this stack runs, a sign out takes effect immediately and everywhere.
With two, a token withdrawn on one is still believed by the other until it
expires, at most one access token lifetime.

That bound is the same one the Redis version has when Redis is down, which is
why the fifteen minute access lifetime was chosen in the first place. The
entries expire themselves: a `jti` stops mattering at the exact instant its
signature stops verifying, so nothing here grows without bound.

<br>

## ADR-032: An unknown email gets the same answer as a malformed request

**Status:** accepted, phase 5

**Context.** An endpoint that answers "no such account" for one address and
"signed in" for another is an endpoint that tells anyone who asks which addresses
have accounts here.

**Decision.** `ErrNoSuchParent` maps to 400 `invalid_request`, the same answer a
malformed body gets, and the message repeats nothing the caller sent. The
service counts the refusal so the rate is visible on a dashboard, and the count
carries no label.

**Consequences.** A genuine typo gets a generic message, which is a slightly
worse experience for a real parent. The api's error envelope is a closed set the
client already maps, so the alternative would have meant adding a code to that
contract for the sole purpose of confirming who has an account.

The counter is where enumeration becomes visible: one refusal is a typo, a
thousand is somebody working through a list, and neither needs an identifier in
a metric label to be seen.

Extended by ADR-052, which adds a password. A wrong password joins an unknown
address under this same answer, and the timing was equalised so the two cannot be
told apart by how long they take.

<br>

## ADR-033: The client sends the amount, and the service refuses anything but its own price

**Status:** accepted, phase 6

**Context.** The payment body carries `amount_cents` and `currency`. A charge
whose size comes from the request body is a charge a caller sets for
themselves, and a trial that can be bought for one cent is worse than a trial
that cannot be bought at all.

Removing the fields was the obvious answer and it costs something real: the mock
provider decides its outcome from the last two digits of the amount, which is
what lets a reviewer demonstrate a decline without an account anywhere.

**Decision.** `checkout.AcceptAmount` compares the requested amount against
`TrialPriceCents`, exactly, and refuses anything else with
`payment.ErrInvalidAmount` before the provider is reached. Overpaying is refused
as firmly as underpaying. In development only, the two amounts the mock reads as
a decline and as an unreachable provider are also accepted.

**Consequences.** The client still states what it believes it is paying, so a
client that has drifted from the price is told rather than charged the right
amount for the wrong reason. The demonstration path exists locally and is
impossible anywhere else, because the relaxation is bound to `APP_ENV` in one
function with a test either side of it.

The day a class carries its own price, this function reads it from the class
instead of from a constant, and no handler changes.

<br>

## ADR-034: A declined payment ends the booking, and that transition is new

**Status:** accepted, phase 6

**Context.** The state machine has allowed `pending_payment -> payment_failed`
since phase 2 and nothing could write it. The repository had `Hold`, `Confirm`,
`Cancel`, and `Expire`, so a declined charge left a booking sitting in
`pending_payment` until the worker expired the hold. The enum value existed, the
frontend rendered it, and no path in the service could produce it.

**Decision.** `Repository.Fail` was added, with both implementations and a
contract case in the shared suite. `checkout.Pay` calls it when the provider
declines. It is separate from `Cancel` because the two mean different things to
whoever reads the row later: no money moved here, so nothing has to move back,
and the audit trail records the payment path as the cause rather than a person.

**Consequences.** A declined booking finishes immediately and its hold is
released, so the seat is free for the next parent rather than parked for the
rest of the countdown. That is the behaviour test 2 describes and the
first thing the frontend's declined screen needed to be true.

The alternative, leaving the booking to expire, would have been a seat held for
ten minutes by somebody who has already been told no.

<br>

## ADR-035: Caching is an optimisation, and every failure of it is a miss

**Status:** accepted, phase 6

**Context.** Redis holds the cached class list, the rate limit buckets, and the
access token denylist. Those three have different consequences when Redis cannot
be reached, and treating them the same way is how one outage becomes three.

**Decision.** Three separate rules, each stated where it is applied:

| surface | Redis unreachable |
| :- | :- |
| response cache | a miss. The read goes to the replica and the tag is dropped |
| rate limit, safe methods | fail open. A read costs a cached body |
| rate limit, writes | fail closed, 503 `dependency_unavailable` |
| access token denylist | refuse. A denylist that cannot say no must not say yes |

**Consequences.** The readiness route reports Redis as required, so a service
that cannot reach it stops taking traffic before most of the above matters. The
rules are what govern the window between Redis failing and readiness noticing,
and the split is deliberate: a flood during an outage is exactly when an
unlimited write path does damage, and a parent refused a class list because a
cache is down is an outage this design was built to avoid.

The denylist rule is the one that could surprise. It makes an unreachable Redis
into failed requests rather than into honoured tokens, and that is the correct
direction: the alternative is believing a token somebody has already signed out
of.

<br>

## ADR-036: The etag carries a version counter as well as a digest

**Status:** accepted, phase 6

**Context.** A tag built from the payload alone repeats whenever a document
returns to something it held before. Cancelling a booking puts a seat straight
back, so the class list becomes byte for byte what it was a moment earlier, and
a client holding the old tag would be told nothing had changed.

**Decision.** The tag is `"<version>-<digest>"`. The version is a counter in
Redis, bumped by every mutation that can move a seat count. The digest is the
first 64 bits of the payload's sha256.

**Consequences.** A repeat needs both halves to collide at once, which is what
makes an invalidation visible even when the bytes are identical. The version
counter carries no expiry, because a counter that reset would hand out a tag a
client is still holding.

The counter is not bumped inside the transaction that changed the seat count.
Redis and Postgres cannot share one, so the bump is a separate call made after
the write commits, and a crash between the two leaves a stale body for at most
its thirty second lifetime. Everything cacheable here is advisory, so the worst
case is a wasted click.

<br>

## ADR-037: The worker's expiry is not the only thing that releases a hold

**Status:** accepted, phase 6

**Context.** Phase 4 built the expiry job and nothing scheduled it. The http
layer is where a hold is granted, so it is where the release has to be
scheduled.

**Decision.** `checkout.Hold` grants the hold first and writes the job second,
with the job's instant set past the deadline by a grace period. The reverse
order was rejected.

**Consequences.** A crash between the two leaves a hold with no scheduled
release. That is survivable: the deadline is on the row, so the class stops
counting the holder the moment it passes, and the only cost is a row sitting in
`pending_payment` until something looks at it. The operator worklist is where it
surfaces.

The other order is not survivable. Scheduling first would write a job for a
booking that may never exist, and a worker would be handed an id it can never
resolve, on every attempt, until it parks.

The grace period exists so the worker loses a race it should lose. A job that
ran at the exact instant a deadline passed would compete with the parent
pressing pay at that instant. The booking side refuses an early expiry anyway,
so this is the second of two defences rather than the only one.

<br>

## ADR-038: A not-found and a not-yours give the same answer

**Status:** accepted, phase 6

**Context.** A parent asking for a booking id can be told three things: here it
is, there is no such booking, or that one is not yours. The last two are
different facts, and telling them apart lets anyone with an account discover
which identifiers exist by asking for them.

**Decision.** `ErrBookingNotFound` and `ErrNotYourChild` both reach the client
as a refusal with no detail: the first as 400 `invalid_request`, the second as
403 `not_your_child`, and neither message repeats the identifier. The ownership
check runs after the read, so the two paths cost the same and are
indistinguishable from the outside by timing as well as by wording.

**Consequences.** A parent who mistypes a link gets a generic refusal, which is
slightly worse than a specific one. The identifiers are UUIDv7 and are not
guessable, so this is defence in depth rather than the only thing standing in
the way, but an endpoint that confirms existence is an endpoint that can be
walked.

The admin worklist is where an operator sees bookings across parents, and it is
role gated. That is the route for the question this one refuses to answer.

<br>

## ADR-039: The challenge is verified when offered and required only by configuration

**Status:** accepted, phase 6

**Context.** The bot prevention table lists a CAPTCHA as its last layer. Making
it mandatory means every test, every script, and every reviewer has to produce a
token, and a mock that accepts anything would make the layer decorative.

**Decision.** `internal/captcha` holds the interface and a deterministic mock in
the shape Turnstile has. The http layer verifies a token whenever one arrives
and does not hold its absence against a caller, unless `RequireCaptcha` is on,
which refuses a submission with no token. It is off by default.

**Consequences.** The layer is real and provable: a refused token is refused, a
passing token passes, and a provider that cannot be reached is a pass rather
than a refusal, so a third party's outage does not become ours. Turning it on is
a configuration change rather than a code change.

What it does not do is stop a determined caller, who simply omits the field
while the flag is off. That is stated here rather than implied, and it is why
the challenge is the last layer rather than a load bearing one. Everything above
it works on properties nobody can decline to send.

<br>

## ADR-040: The api does not trust a forwarded-for header

**Status:** accepted, phase 6

**Context.** The address rate limit needs a caller address. `X-Forwarded-For` is
written by whoever is dialling, so an api that reads it lets any caller choose
which bucket to spend.

**Decision.** `CallerAddress` reads `RemoteAddr` and nothing else.

**Consequences.** Behind a proxy this becomes the proxy's address, and the
address bucket stops distinguishing callers. That is the honest consequence and
it is acceptable, because the address bucket was always the weaker of the two:
the subject bucket is keyed on a parent id this service issued, and it carries
on unchanged.

The day this service runs behind a proxy it controls, the fix is to read the
header only from a known set of proxy addresses, which is a change to this one
function.

<br>

## ADR-041: Metrics are a library, and logs are the standard library

**Status:** accepted, phase 7

**Context.** Layer one of the monitoring plan is cpu, resident memory, file
descriptors, goroutines, heap, and garbage collection pauses, per process. Every
one of those is either a read of `/proc` or a read of the Go runtime, and the
Prometheus exposition format has rules about escaping, ordering, and histogram
buckets that are easy to get subtly wrong.

The worker already shipped a hand written exposition in phase 4, and its own
comment said this phase would replace it.

**Decision.** `github.com/prometheus/client_golang` for metrics.
`log/slog` from the standard library for logs.

**Consequences.** The split is not inconsistency. The metric library is paying
for something real: the process and runtime collectors, the histogram maths, and
a format nobody has to hand check. The logging library would be paying for
structured json and levels, which `log/slog` already gives, so it would be a
dependency bought for nothing.

The hand written exposition is gone, and the four metric names it published,
`queue_depth`, `worker_jobs_claimed_total`, `worker_jobs_completed_total`, and
`queue_job_failed_total`, carry over unchanged. That was the promise its comment
made, and it is kept.

One cost is stated rather than discovered: the registry is built per process
rather than taken from the library's global default. A global cannot be built
twice, so two test cases wanting independent counters would panic on the second.

<br>

## ADR-042: A metric label is a bounded enumeration, and the route label is a pattern

**Status:** accepted, phase 7

**Context.** The obvious way to label a request metric is by path, and the
obvious way to label a booking metric is by booking. Both are wrong in the same
way: an identifier in a label is one time series per booking. That is a leak of
who booked what to anybody who can reach the monitoring system, and it is a way
to run that system out of memory over a weekend.

**Decision.** Every label value comes from a fixed list written into the code.
The route label is the registered pattern, bound in when the middleware is
built, never `request.URL.Path`. A recording method handed a value it does not
recognise folds it into the nearest known one rather than opening a new series.

**Consequences.** `GET /api/v1/bookings/{bookingId}` is one series. A dashboard
cannot break a panel down by class or by parent, and that is the correct trade:
those questions belong to a query against the database, where there is an access
control decision in front of them.

Test 14 asserts the property rather than trusting it. It drives a whole
booking and then scans the exposition for anything shaped like a uuid.

The one place values arrive from outside the process is the telemetry endpoint.
There the rule is stricter: an unrecognised value is dropped rather than folded,
because a browser is somebody else's computer and folding would let a modified
page create series at will.

<br>

## ADR-043: Two metric labels differ from the plan's table, on purpose

**Status:** accepted, phase 7

**Context.** The monitoring plan's metric table proposed `booking_confirmed_total`
with a `subject` label. The class subject is a bounded enumeration, `science` or
`math`, enforced by a check constraint, so the label would not break ADR-042.

The problem is where the value would come from. The confirm path knows the
booking and the class identifier and does not read the class row. Populating the
label would mean adding a query inside the transaction that holds the lock every
other parent for that class is waiting behind.

**Decision.** No label. The counter is the count.

**Consequences.** A dashboard cannot split confirmations by subject. That is a
breakdown somebody might want and nobody needs, and the alternative was
lengthening the one transaction the whole design exists to keep short.

The plan's table is wrong on this row rather than the code being incomplete, and
this record is where that is said out loud rather than left as a silent
difference between a document and a service.

**The second row is `queue_depth`.** The table proposed a `kind` label, meaning
the job kind. The worker has published `queue_depth{state}` since phase 4, where
state is ready, claimed, or parked, and that is the breakdown the `QueueStalled`
alert needs: jobs waiting while nothing finishes. A kind breakdown would answer a
different and less urgent question, and carrying both would be two gauges over
the same rows.

The name carries over from phase 4 unchanged, which is what ADR-041 promised
when the hand written exposition was replaced.

<br>

## ADR-044: Redaction happens at the writer, and names have no field to live in

**Status:** accepted, phase 7

**Context.** The sensitive data rules say cookie and authorization values are
redacted in logs, and that ids are acceptable while names and emails are not.
Redaction at the call site is a convention, and a convention is what fails on
the one line nobody reviewed.

There is a second problem the first does not solve. A cookie value has no shape,
so only the field name identifies it. An email has a shape and can be matched. A
name has neither.

**Decision.** Three layers. A `slog.Handler` wraps the json writer and scrubs
every record, so no call site performs redaction and none can forget it. A field
name list catches values identified only by their key. A pattern catches emails
and headers written out inside free text, which is how a wrapped driver error
carries a whole request.

For names there is no fourth layer, and instead there is an absence: the log
field vocabulary has no field for a parent's name or a child's, so there is
nowhere to put one.

**Consequences.** A value bound with `With` is scrubbed too, and so is one
nested in a group, because both are ways a secret arrives without the top level
field name giving it away.

The header pattern is deliberately greedy to the end of the line or the next
delimiter. `Bearer <token>` is two words, so stopping at the first space would
replace the scheme and leave the token, which is the half that matters.

Test 14 drives a failing request with a session cookie on it and asserts
nothing from that cookie reached the output.

<br>

## ADR-045: The fault surface is a guarded runtime route, not a build tag

**Status:** accepted, phase 7

**Context.** `confirm_transaction_total{outcome="error"}` and the alert built on
it are only trustworthy if the confirm transaction can be made to fail on a
running stack and the failure watched arriving. A build tag would keep the call
sites out of a production binary entirely, which is the safer answer.

It also means a rebuild in the middle of a demonstration.

**Decision.** A runtime surface behind four guards. `APP_ENV` must be
development and unset counts as refusal. `FAULT_INJECTION_ENABLED` defaults to
false and the api refuses to start with it true anywhere else. When it is off
the routes are never registered, so the surface answers a plain not found rather
than a refusal that would confirm it exists. When it is on the routes carry the
admin role check and the write rate limit.

Two more things make it self cleaning. An arming carries a count, default one
and capped at ten, and a lifetime, default sixty seconds and capped at ten
minutes, so a forgotten fault cannot leave a stack broken. And
`fault_injection_enabled` is published as a gauge whether the surface is on or
off, so the dashboard's first row says plainly that this stack can be broken on
purpose.

**Consequences.** A handful of guarded call sites live in production code:
inside the confirm transaction, at the class lock, in the mock provider, in the
worker's dispatch, and at the Redis read. Each is one nil comparison against a
registry that is nil unless configuration turned it on.

The registry is per process and holds nothing durable. Restarting the api is the
last resort for undoing an arming, and it has to work.

Every fault point simulates a failure that can genuinely happen. That is a rule
rather than a preference: a fault with no real counterpart proves the metric
moves and proves nothing about the system.

<br>

## ADR-046: A lock wait timeout is its own failure, answered as retryable

**Status:** accepted, phase 7

**Context.** The `confirm.lock_wait` fault point needed something to return.
Reusing an existing sentinel would have made it a fault with no real
counterpart, which is exactly what ADR-045 rules out.

An overloaded Postgres genuinely does give up on a lock rather than waiting
forever, and that failure is unlike every other one the confirm can produce:
nothing was decided and nothing was written.

**Decision.** `booking.ErrLockWaitTimeout`, mapped to 503
`dependency_unavailable` rather than to a refusal.

**Consequences.** The client already handles that code, and the honest answer to
a parent is "ask again" rather than a refusal they would read as final.

`booking.ErrTransactionBroken` is deliberately not mapped at all, so it falls
through to internal_error with a request id. A parent cannot act on it, the
detail belongs in the log line the id leads to, and what matters most is what it
is not: it is not `ErrSeatLost`, so no money is marked for refund and the hold is
left standing for a retry.

<br>

## ADR-047: The gauges are sampled on a timer, not inside the scrape

**Status:** accepted, phase 7

**Context.** Four numbers describe a state rather than a count: the two pool
sizes, the replication lag, and how many parents are owed a refund. Nothing
raises an event when any of them changes, so somebody has to go and look.

The obvious place to look is inside the `/metrics` handler, which is where
Prometheus already is.

**Decision.** A goroutine on a five second timer, matching the scrape interval.
`/metrics` has no database query on it at all.

**Consequences.** A published number is at most one scrape stale, which for a
gauge nobody alerts on to the second is not a difference anybody can act on.

The gain is what matters: `/metrics` answers when the database is the thing that
is broken. A scrape handler that queried Postgres would go silent at exactly the
moment somebody needs the numbers, and a silent scrape target reads as a process
that has died.

A value that cannot be read keeps its last one rather than being set to zero.
Zero replication lag reads as a replica that is perfectly caught up, which is the
opposite of what an unreachable replica means. The readiness probe is what says
a dependency is down.

The replication lag is read on the replica rather than on the primary, and that
is the load bearing half of this record. The primary's `pg_stat_replication` has
no row at all while a replica is disconnected, so a replica that has fallen over
reports no lag from that side, which reads as perfect health.

<br>

## ADR-048: The dashboards and the rules are compiled by a test

**Status:** accepted, phase 7

**Context.** A metric name lives in three places that cannot see each other: the
code that increments it, the Grafana panel that draws it, and the alert rule that
fires on it. A rename that updates only the first turns a panel blank and an
alert silent, and neither failure announces itself. A blank panel on a quiet
afternoon looks exactly like a healthy service.

**Decision.** A Go test reads every dashboard json and the rule file, pulls every
metric name out of every query, and checks it against what the registry actually
publishes.

Three specific things are checked by name as well. The transaction failure panel
queries `confirm_transaction_total{outcome="error"}`, which is the exact series
`TransactionErrorSpike` fires on. Cpu, memory, and drive still resolve from the
queries that do not mention cAdvisor. And every one of the thirteen alerts the plan
names is present.

**Consequences.** The names come from a registry that has been driven rather
than merely built, so a name counts as published only when a code path exists to
produce it. A list taken from the constants would pass even if half of them were
never registered.

`promtool test rules` covers the other half, replaying a synthetic series and
asserting that `TransactionErrorSpike` fires on a broken transaction, does not
fire on twenty lost races, and that `RefundBacklog` and `RefreshReuse` fire when
they should. The middle case is worth the most: those two outcomes look identical
in a rollback count, and telling them apart is the whole reason
`confirm_transaction_total` carries an outcome label.

<br>

## ADR-049: The telemetry endpoint is authenticated, bounded, and drops what it does not know

**Status:** accepted, phase 7

**Context.** The browser cannot be scraped. Everything else Prometheus knows
about is a process this project runs, and the client is the one part of the
system that has to report on itself, which means an endpoint that turns a posted
event into a metric.

That endpoint is the only place in the service where a label value arrives from
outside.

**Decision.** It sits behind the parent role, the origin check, and the write
rate limit, exactly like every other mutation. The body is read through a bounded
reader. The batch is capped at fifty events and refused whole past it. Every
label value is checked against a closed list, and anything unrecognised is
dropped rather than folded into a known value.

The lists are checked twice, once in the converter and once again in the metric
methods themselves. That duplication is deliberate.

**Consequences.** One unrecognised event does not throw away the nine good ones
posted alongside it. A client that has not been reloaded can send one stale
field, and refusing the whole batch for it would make every deployment blind for
as long as an old tab stayed open.

The answer is 204 with no body at all. The client swallows every failure from
this route by design, so there is nothing for it to read, and sending a tally
back would invite somebody to start acting on one.

A reported page load over two minutes or below zero is dropped. The number
arrives from a browser, so it is a claim rather than a measurement, and one
absurd sample drags every quantile with it until the panel stops describing
anybody.

<br>

## ADR-050: The composition root is split by what each file assembles

**Status:** accepted, phase 7

**Context.** `cmd/api/main.go` had grown to hold the configuration, both stores,
the session, the checkout, the advisory readers, every guard, the readiness
probes, the router, the listener, and the shutdown. Every one of those is a
different question, and the file answered all of them in the order they happened
to be needed.

**Decision.** One file per thing assembled. `dependencies.go` opens the two
stores, `session.go` builds authentication, `checkout.go` builds the seat and the
money and the queue, `reads.go` builds the two advisory readers, `guards.go`
builds what runs before a handler, `operations.go` builds the probes,
`faults.go` builds the development only surface, `routes.go` assembles them,
`listener.go` owns the socket, `sampler.go` owns the gauges, and `process.go` is
the order it all happens in. `main.go` is the entry point and nothing else.

The worker is split the same way.

**Consequences.** A reviewer looking for how the session is put together does not
read how the queue is. The read routing decision, which is the one thing in the
wiring worth being careful about, is now visible as two files: everything in
`reads.go` uses the replica and everything else uses the primary, and the names
say so.

What the two binaries genuinely share moved to `internal/bootstrap`: opening the
pools, opening Redis, building the registry and the logger, and the two lifecycle
contexts. Those were written twice before, which is how two processes end up
disagreeing about a connect timeout.

The cost is more files. That is the point of it.

<br>

## ADR-051: Redis runs with protected mode off inside a private network

**Status:** accepted, phase 9

**Context.** Redis 8 ships `protected-mode yes`, which refuses every client that
is not on its own loopback interface when no password is set. In containers that
is every client there is: the api and the worker each have their own network
namespace, so both are answered `DENIED` and the api restarts in a loop. The
stack could not start.

Three ways out. Set `requirepass` and give both processes the password. Bind
Redis to a container address that the runtime decides. Or turn protected mode off
and rely on what is already holding.

**Decision.** Protected mode is off in `containers/redis/redis.conf`, and the
file says why at length rather than leaving a reader to wonder.

**Consequences.** What is holding is two layers below that file. The port is
published as `127.0.0.1:6379`, so nothing off the machine can reach it, and the
compose network is private to this stack. Adding a password here would move a
secret into the compose file and change nothing about who can connect.

A deployment that puts Redis anywhere but a private network sets
`REDIS_PASSWORD` and turns protected mode back on. The api already reads that
variable and holds it as a secret, so that is a configuration change rather than
a code change.

This was found by running the phase 9 integration path against real containers
for the first time, which is the sort of thing that only that tier can find.

<br>

## ADR-052: Sign in takes a password, hashed with argon2id

**Status:** accepted, phase 10

**Context.** Sign in took an email and nothing else. Anybody who knew a seeded
address was that parent, and `parents` had no column that could hold anything to
check. Calling that authentication in a document about security was the part that
did not survive a second reading.

**Decision.** `parents` gains `password_hash`, and sign in verifies the typed
password against it with argon2id at 64 MiB and one pass. The parameters travel
inside the stored string in the standard encoding, so raising them later leaves
every existing row verifiable.

Three details are the decision rather than the implementation:

The hash is read on one path only. `CredentialByEmail` is the single query that
selects `password_hash`, and it returns a `Credential` rather than a `Parent`, so
the hash sits on no struct a handler could put into a response by reflex.

The unknown-address path does the hashing work anyway and throws it away.
Skipping it would make an address nobody holds measurably faster to refuse than a
real address with a wrong password, and that difference is readable from outside,
which would give back exactly what ADR-032 hides.

A stored hash that cannot be read is its own error, `ErrPasswordUnusable`, and
never a wrong password. One is somebody mistyping and the other is a seed or a
migration that went wrong, and an account whose hash is unreadable can never be
signed in to.

**Consequences.** The four seeded accounts share one password, `otto123`, and it
is written down in `how-to.md`. That is the development convenience, not the
storage: no password exists anywhere in this system, in development or otherwise.

`backend/scripts/seed.sh` moves from the safe class to prompting. It can still
only insert into an empty database, so it destroys nothing, but what it creates
is four accounts whose password is public. Its flag is `--generate-demo-users`
rather than `--yes`, because a flag should name what happens rather than what is
being waived.

The schema carries the rule too. `parents_password_hashed` refuses any row whose
value does not begin `$argon2id$`, so the one mistake that could quietly sign
everybody in is refused by the database rather than found in a log.

<br>

## ADR-053: The api answers a browser's origin questions, and the answer is the same list as the csrf check

**Status:** accepted, phase 10

**Context.** The client is served on 9001 and the api answers on 9000, so every
call a browser makes is cross origin. The api sent no cors headers at all, which
meant a browser refused to hand any answer to the page while curl saw a perfectly
correct service. Every screen failed with a message that explained nothing, and
the whole suite passed, because a suite of curl calls cannot see this.

**Decision.** One cors layer, wrapped around the whole surface including the
operations routes, driven by the same `ALLOWED_ORIGINS` the csrf check reads.

`FRONTEND_ORIGIN` becomes `ALLOWED_ORIGINS` and becomes a list. A browser treats
`127.0.0.1:9001` and `localhost:9001` as different origins, both reach the same
development stack, and a reviewer types whichever they think of. One value meant
one of the two silently failed.

The origin is echoed rather than answered with a wildcard, because a wildcard is
not permitted once credentials are involved and the session here is cookies. The
layer is innermost of the three outer wrappers, so a preflight carries a request
id and is covered by the panic recovery, and it answers preflights itself: a
preflight carries no cookie, so passing it to the origin check would refuse the
very request that asks whether a write is permitted.

`Guard.OriginIsAllowed` is exported and is what both the cors layer and the
origin check call. Two copies of that rule could disagree, and a browser told a
write is permitted and then refused when it makes one is worse than either answer
alone.

**Consequences.** A refused origin gets an ordinary response with no cors headers
rather than an error status. The browser turns that into the failure, and a
status would confirm to a probing page that the origin was recognised and
rejected.

`Vary: Origin` is on every answer including refusals, so a cache in front of this
service cannot hand one origin's answer to a request from another.

The tests that would have caught this are in `internal/httpx/cors_test.go` for
the layer and in `router_test.go` for the wiring, because a cors layer that
exists and is not on the router is the same bug still there.

<br>

## ADR-054: A build identity is asked of four sources in order

**Status:** accepted, phase 10

**Context.** `/version` answered `{"version":"dev","commit":"unknown","built_at":
"unknown"}` on every running stack. The three values were link time variables
that defaulted to those words, and `compose.yml` passed the same words as build
arguments, so the stamp changed nothing. `config.BuildSettings` existed, was
loaded, and was read by nothing. A reviewer opening the status screen saw three
fields that could not tell them which build was in front of them.

**Decision.** Four sources, asked in order, first answer wins:

| order | source |
| :- | :- |
| 1 | the linker, `-X main.buildVersion` and the other two |
| 2 | `BUILD_VERSION` and `BUILD_COMMIT` from `config.json` or the environment |
| 3 | the record the Go toolchain embeds, and the running binary's own timestamp |
| 4 | the word `unknown` |

The defaults become empty strings, because empty is how a source says it has no
answer and a word like `dev` ends the search before anything that does know is
asked. The linker is first, because a released image describes itself and no
setting on the host it happens to run on may rename it. `config.json.template`
states `0.1.0`, which is the one committed place the backend version number is
written down.

The third source is what removes the last two `unknown`s without anything being
written down anywhere: the Go toolchain records the revision in every binary it
builds from a checkout, and the binary's file timestamp is the moment the linker
produced it. Copying a file into an image keeps its time, so that holds for a
container too.

**Consequences.** A container build has no repository in its context, so the
toolchain records no revision there. `scripts/stack_up.sh` stamps the images it
builds with this checkout's commit and the moment it built them, so a stack
started the documented way reports a real commit rather than a container-shaped
blank.

The third source has one condition that is easy to miss: `go build` records the
revision on its own and `go run` records it only when passed `-buildvcs=true`.
The from-source path was answering `unknown` for that reason alone, with a
checkout right there to read. `scripts/debug.sh` passes the flag, the documented
commands carry it, and `internal/config/build_identity_test.go` fails if it is
ever dropped from the script. The alternative, reading `.git` at startup, was
refused: it reports the checkout the process was started in rather than the
source it was built from, so it can state a commit that is not the running code,
which is worse than saying `unknown`.

The api and the worker resolve the same way and log the resolved values, so a
stack whose two halves disagree is visible rather than hidden.

The container does not read `config.json`, which is ADR-024 and stays true, so
`compose.yml` states the version a second time. `internal/config/build_identity_test.go`
reads both files and fails when they drift apart, because that drift is otherwise
silent: the api would answer one version and the image would be tagged another.

`stack_up.sh` gained `--build` for the backend, matching the frontend. Without it
a source change is invisible: the image already exists, compose starts it, and the
stack comes up healthy running the code from before the edit.

<br>

## ADR-055: A request header this api reads is a header the preflight names

**Status:** accepted, phase 10

**Context.** ADR-053 added the cors layer and permitted `Content-Type` and
`If-None-Match`. Two handlers read `Idempotency-Key`, which was never added. A
browser sends only the headers a preflight permitted, so every booking and every
payment was refused before it left the page: the preflight answered without the
header named, the browser blocked the request, and the screen showed the generic
failure. curl was unaffected, the api was correct, and the whole suite passed.

**Decision.** `allowedRequestHeaders` is built from `IdempotencyKeyHeader` rather
than repeating the string, so the constant the handlers read and the list the
preflight answers cannot drift apart.

**Consequences.** Adding a request header to a handler means adding it to that
list in the same change. `cors_test.go` walks every header this api reads and
fails when one of them is not permitted, which is the case that would have caught
this. It also checks the match case insensitively, because a browser lowercases
what it asks for and this client sends `idempotency-key`.

The general lesson is ADR-053's, stated again: no number of curl calls can see
this class of bug, because curl never sends a preflight.

<br>

## ADR-056: A parent can list their own bookings, and the token is the whole scope

**Status:** accepted, phase 10

**Context.** A booking could be read one at a time, at
`GET /api/v1/bookings/{bookingId}`, and nothing listed them. The identifier is
minted by the api and reaches a parent only as part of the address of the screen
they were on. Closing that tab lost it. There is no email in this service and no
receipt, so a parent who paid and then closed the tab had no way at all to find
out whether the payment landed, and "no way to find out" reads exactly like
"the money is gone".

The operator worklist already lists bookings, and it is not an answer here. It
crosses every parent, which is why it sits behind the admin role.

**Decision.** `GET /api/v1/bookings` answers the bookings of whoever is signed
in, newest first, capped at 50.

The parent comes from the authenticated identity and from nowhere else. The
route reads nothing from the request: no query string, no body, no path value.
`ParentBookings` applies the parent inside the statement, as a subquery on
`students`, so there is no shape of the call that returns another family's row
to be filtered out afterwards.

The cap is fixed rather than a page size a client can ask for. A family has a
handful of bookings, so a parameter would be a knob with nothing behind it and
one more value to validate. The cap exists so the read cannot grow with an
account that has been used for years.

Every status is listed, finished ones included. A parent opens this to find out
what happened, and what happened is often that it did not go through.

**Consequences.** The route shares its address with `POST /api/v1/bookings` and
differs only in the method, which the mux matches on, so the two cannot be
confused for one another.

It is `no-store`, like the single booking read and for the same reason: a stale
`pending_payment` after a card cleared looks exactly like a payment that was
lost.

`ParentBookings` is a repository method rather than a `WorklistRequest` with a
parent field. Folding it in would have put the one read that must never cross
parents into the same method as the one that always does, and a mistake there is
one family reading another's bookings.

The read goes to the primary. This is the screen a parent opens straight after
paying, and a replica a second behind would show the booking they just paid for
as still waiting for money.

<br>

## ADR-057: Each service owns its data directory

**Status:** accepted, phase 10

**Context.** Every service's state sat in one tree, `backend/.data/`, deleted as
a single path. `scripts/cleanup_dev.sh` failed to remove one container and said
nothing, then deleted that tree while the container was still running. Postgres
kept serving from deleted files: it answered `pg_isready` and failed every query,
so the next run reported a duplicate catalogue entry rather than the truth.

**Decision.** State moves to `containers/<service>/.data`, next to that service's
own configuration. One volume per service, independent of the others.

Two supporting changes. The cleanup reports a removal it could not do and refuses
to delete any state while a container is still there. The readiness gates in
`debug.sh` and `stack_up.sh` ask for `select 1` over tcp rather than
`pg_isready`, which also answers yes for the temporary server the postgres image
runs while it builds a new database.

**Consequences.** This amends ADR-015: a wipe is now five visible paths rather
than one, and `cleanup_dev.sh` names them all from the same list compose reads.

The directory is `.data` and not `data` because this tree is inside the Go
module. `go build ./...` walks every directory that does not start with a dot or
an underscore, and a container written one is not readable by the user running
it.
