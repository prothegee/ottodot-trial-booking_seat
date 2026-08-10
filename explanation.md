# Explanation

One document, two readers. The first half assumes no technical background. The
second half is for a reviewer who wants the reasoning and the trade-offs.

Nothing here is written twice: the decision records live in `backend/ADR.md` and
`frontend/ADR.md`, and this document points at them rather than repeating them.

<br>

## What This Is

Parents book a free trial class for their child. A trial class seats four
students. Between choosing a class and paying for it, a few seconds pass, and in
those seconds somebody else may take the seat.

The whole exercise is one sentence: when two parents reach for the last seat at
the same moment, exactly one of them may end up confirmed, and the other must
not be left having paid for a seat that no longer exists.

What was built:

- A database that holds the rules itself, so a seat cannot be sold twice even if
  the code above it has a bug.
- A booking flow with three steps: hold a place, pay, then confirm. Only the
  last step decides who gets a seat.
- A payment step that never puts a child on the roster unless the money settled
  and a seat was still free.
- A record for the case in between: money moved, the seat did not. Somebody has
  to give that money back, and the system says so plainly.
- Tests that prove all of the above, including one that runs twenty real
  attempts at once and checks that exactly four seats came out.

<br>

## The One Hard Part

The sequence the brief asks about, told plainly.

Four seats. Three are taken. One left.

1. **Parent A picks the last seat and goes to the payment screen.** They do not
   have the seat. They have a place in the queue for it, which runs out after
   ten minutes.

2. **Parent B picks the same seat.** This is allowed on purpose. If holding a
   place blocked everyone else, a parent who wandered off would freeze the last
   seat for ten minutes, and the class would run with an empty chair.

3. **Parent B pays first.** Their payment settles, and only then does the system
   ask the real question: is a seat free right now. It is. Parent B is confirmed
   and gets seat four.

4. **Parent A then pays.** Their payment settles too. The system asks the same
   question. This time the answer is no. Parent A is not confirmed. Their
   booking is marked as needing a refund, and a background job sends the money
   back.

The part that makes this correct rather than lucky is step 3 and step 4 asking
the question in a way that cannot overlap. While one booking is being decided,
every other booking for that same class waits its turn. Not for long, and only
per class, so a busy class never slows down a quiet one.

The alternative most systems reach for first is to count the seats, see that one
is free, and then take it. That works until two people count at the same instant,
both see one free seat, and both take it. That is the bug this exercise is
about, and it is why the counting and the taking happen together here, as one
step nobody can interrupt.

<br>

## How Correctness Is Proven

Five tiers, and the split between them is the point.

| tier | runs against | proves |
| :- | :- | :- |
| unit | one function, nothing else | the seat picker takes the lowest free seat, the state machine refuses a confirmed booking going back to unpaid |
| edge | the same functions at their boundaries | a hold expiring exactly now counts as expired, a full class returns nothing rather than seat zero |
| integration | components wired together over a fake | the service calls the right things in the right order |
| behaviour | a whole user-visible outcome over a fake | a duplicate booking is refused and leaves no row behind |
| real database proof | real Postgres, real parallel connections | twenty simultaneous attempts on a four seat class produce exactly four confirmed seats |

The first four are fast and need nothing running. **They still cannot prove the
last-seat rule.** A fake proves the code asked the right questions. It cannot
prove that the database serialized two real transactions, because there is no
transaction in a fake. That is what the fifth tier is for.

The two implementations, the fake and the real one, are held to a single shared
contract suite. A fake that enforces the rule correctly while the sql does not
would otherwise pass every fast test, and that is the failure this guards
against.

One more check, because a test that cannot fail proves nothing: the row lock was
removed on purpose and the twenty-attempt simulation was re-run. It failed, with
three confirmed seats instead of four. The lock was then restored and the suite
re-run green.

<br>

## Design Decisions And Trade-Offs

The six worth defending here. The full records, with what was rejected and what
it cost, are in `backend/ADR.md` and `frontend/ADR.md`.

| decision | alternative rejected | cost accepted |
| :- | :- | :- |
| the seat is decided by locking the class row and counting under that lock | a running counter column, or checking then inserting | confirms serialize per class, which at four seats costs nothing |
| a unique index on class and seat number is kept as a backstop, not the mechanism | trusting the lock alone | one more index, and the ceiling still lives in the transaction |
| holding a place is allowed beyond capacity, by a configurable allowance | blocking everyone once capacity is held | a few parents pay and are refunded, which is the exact sequence the brief requires |
| paid but seatless is its own status, separate from a declined payment | one generic failure | one more status, in exchange for an operator knowing whether money moved |
| the payment settles first, the seat is decided second | decide the seat, then charge | a loser gets charged, and the refund job is what makes that acceptable |
| seat counts shown on screen are advisory and never decide anything | trusting the number the parent can see | every screen has to handle a rejection that contradicts what it just showed |

<br>

## What Was Deliberately Cut

| cut | why it was the right cut |
| :- | :- |
| regular enrollment | out of scope, stated in the brief |
| a real payment provider | the interface is the shape a real one would have, and a real one adds credentials and a sandbox without adding judgment |
| a real sign-in with passwords | authentication is not what is being assessed, and a mock keeps the demo reproducible |
| automatic failover for the database replica | promotion is one documented command, and an automatic one needs a quorum a single machine cannot honestly demonstrate |
| a browser end-to-end runner | the real proof is the race at the api level, which is faster to run and proves more |
| visual polish | the brief weights correctness over appearance, so it is last and only if time remains |

<br>

## What Would Be Watched After Release

The alert that matters most is not about uptime.

`RefundBacklog` counts bookings sitting in the paid-but-seatless state whose
refund has not settled. Every one of those is a parent who has been charged for
a seat they did not get. A service that is fully up while that number climbs is
failing in the way that costs the most trust, and no availability graph would
show it.

Beside it, an error spike on the confirm transaction, because that is the single
path where a failure could mean a seat was sold twice.

<br>

## Proving The Failure Path

An alert nobody has watched fire is not coverage. It is a query somebody wrote
once.

The plan for this build includes a fault surface: named injection points that
can be armed at runtime, so the confirm transaction can be made to fail on
purpose while the dashboard is on screen. It is guarded four ways, and any one
of them being wrong means the surface does not exist at all: the environment
must be development, an off-by-default flag must be on, the caller must hold an
admin role, and the routes are not registered otherwise. The api refuses to
start if the flag is on outside development.

That guard is already enforced and tested in the configuration loader. The
injection points themselves arrive with the monitoring phase.

<br>

## Where To Go Next

| document | contents |
| :- | :- |
| `README.md` | what is here, how to run it, and the state of each phase |
| `how-to.md` | commands, ports, and what to do when something refuses to start |
| `backend/ADR.md` | every backend decision, with what was rejected |
| `backend/LLD.md` | the schema table by table, and the confirm transaction step by step |
| `frontend/ADR.md` | every client decision, including why it never reads a token |
