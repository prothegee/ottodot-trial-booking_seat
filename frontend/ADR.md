# Frontend Decision Records

Numbered records, each with the context that forced a choice, the choice itself,
and what living with it costs. Cross-stack decisions are recorded in
`../backend/ADR.md` and not repeated here.

| status | meaning |
| :- | :- |
| accepted | decided and implemented |
| planned | decided, implementation in a later phase |

<br>

## ADR-F001: SvelteKit with server rendering off, static output

**Status:** accepted, phase 1

**Context.** Every meaningful decision in this system happens inside a
transaction on the Go backend. A rendering server in front of it would be a
second moving part.

**Decision.** SvelteKit with `ssr = false` and `prerender = false`, built to a
static bundle with one fallback document.

**Consequences.** No server rendered first paint, and the client asks the api
for everything it shows. In exchange there is one backend rather than two, one
place where a seat count can be trusted, and a deployment that is a directory of
files.

Prerendering is off for the same reason: there is no page here whose content is
known before a parent signs in.

<br>

## ADR-F002: Caddy in its own container

**Status:** accepted, phase 1

**Context.** A static bundle needs something to serve it, and client routes such
as `/book/[classId]` only exist at runtime.

**Decision.** Caddy, with `try_files` handing the same document to every unknown
path.

**Alternatives rejected.** A Node process, which would mean a runtime to patch
for no benefit. A dev server in production, which is not what one is for.

**Consequences.** One more container, which the monitoring already covers.

<br>

## ADR-F003: Seat counts are advisory and never decide anything

**Status:** accepted, phase 1

**Context.** The client can show a seat count. The temptation is to use it, for
example by refusing to submit when it reads zero.

**Decision.** The count is a hint. The screen may grey out a full class to save
a wasted click, and every step still handles a rejection that contradicts what
it just showed.

**Consequences.** Every screen needs a path for an outcome it did not expect.
That is more work than trusting the number, and it is the only version that is
correct, because the number is stale the moment it is rendered.

<br>

## ADR-F004: The client never reads a token

**Status:** accepted, phase 2

**Context.** The access token is a JWT. Decoding it would give a display name
without a round trip.

**Decision.** Both tokens live in HttpOnly cookies. No code path reads, decodes,
or stores either one. Every request sends `credentials: "include"`. Whether a
session is valid is learned from a response.

**Alternatives rejected.** `localStorage`, which any script on the page can
read. Decoding the JWT for a display name, which would make the client depend on
a claim shape it does not own, for a name that one call to `/auth/me` returns.

**Consequences.** One extra round trip after signing in. In exchange there is no
token anywhere a script can reach, and the claim payload can stay free of
anything identifying, because nothing here needs to read it.

<br>

## ADR-F005: One refresh at a time, then exactly one retry

**Status:** accepted, phase 2

**Context.** Several calls can be in flight when the access token expires. The
naive fix retries each one after its own refresh.

**Decision.** A coordinator that collapses concurrent callers into a single
refresh call, then each caller retries its original request once. Never a loop.

**Consequences.** This is not an optimisation. The backend rotates the refresh
token on use and treats a second use of a rotated one as reuse, which revokes
the whole family. Five parallel refreshes would therefore end the session they
were trying to save.

A failed refresh reports the sign out once, not once per waiting caller, for the
same reason: three sign outs would mean three navigations away from whatever the
parent was looking at.

<br>

## ADR-F006: The api client reports a sign out, something else performs it

**Status:** accepted, phase 2

**Context.** The client is the only place a 401 can be noticed. It is also the
wrong place to know about routing.

**Decision.** `createApiClient` takes an `onSignOut` callback and does nothing
else about it. `lib/session/sign_out.ts` owns what a hard sign out does: clear
the store, clear session storage, route to sign in with a reason.
`lib/session/client.ts` is the one file that wires them together.

**Consequences.** One more indirection. In exchange the api client has no
opinion about navigation and is testable with no router, and the sign out is one
function rather than three lines repeated at every call site, which is how a
signed-out screen ends up still showing the previous parent's children.

<br>

## ADR-F007: Session storage is cleared wholesale on sign out

**Status:** accepted, phase 2

**Context.** The internal cache writes entries. Removing them key by key means
remembering to add each new key to the sign-out path.

**Decision.** `sessionStorage.clear()`.

**Consequences.** Anything else on the origin goes too, which is nothing, since
this client is the only writer. A key added in a later phase is covered without
anyone remembering, and a stale entry surviving a sign out is exactly what would
leak between two parents on a shared machine.

The in-memory half of the cache is emptied alongside it, in the same function.
Clearing storage on its own would leave the previous parent's class list in a
map that outlives the sign out, because nothing reloads the page.

<br>

## ADR-F008: The auth store is memory only

**Status:** accepted, phase 2

**Context.** Mirroring the session into storage would survive a reload.

**Decision.** Memory only. A reload asks the api again.

**Consequences.** One call after a refresh of the page. In exchange there is no
durable copy of a parent's details anywhere in the browser, which matters
because the session itself lives in cookies this code cannot read.

The store also copies across only the fields it is allowed to hold. If the api
ever starts sending an email alongside the session, it stops there rather than
reaching a component. A test asserts that on the serialized state, because the
risk is a field nobody thought to look for.

<br>

## ADR-F009: Wire field names are used unchanged

**Status:** accepted, phase 2

**Context.** The api speaks snake_case. TypeScript convention is camelCase. The
usual answer is a mapping layer.

**Decision.** The api's own names, unchanged. `parent_id`, not `parentId`. No
second vocabulary and no mapper.

**Consequences.** snake_case appears in components, which reads oddly for a
TypeScript codebase. In exchange there is one name per field, a reviewer can
search the backend for the same word, and there is no layer to keep in step
when the contract changes.

<br>

## ADR-F010: Error text is rendered from the typed code

**Status:** accepted, phase 2

**Context.** The api sends a message alongside every error code.

**Decision.** The client maps the code to its own wording. The server's prose is
never pasted into the page.

**Consequences.** One mapping table to keep in step with the backend envelope.
In exchange the wording on screen is owned here, no server text can leak an
internal detail onto a recording, and an unknown code falls back to a generic
error rather than a blank page.

`internal_error` gets particular care: it is the one code that says nothing
about the booking, so the wording never claims the seat was lost or the payment
failed. Neither is known from here.

<br>

## ADR-F011: The fake transport is its own file

**Status:** accepted, phase 2

**Context.** The interface, the real transport, and the fake could sit together.

**Decision.** `transport.ts` holds the interface and the fetch transport.
`transport_fake.ts` holds the fake.

**Consequences.** One more file. In exchange test-only code can never reach a
production bundle, and the two cannot quietly grow into each other.

<br>

## ADR-F012: Tests use an injected fake, not a browser

**Status:** accepted, phase 1

**Context.** The alternatives are a mock service worker, a real network, or a
browser runner.

**Decision.** The api client takes a transport at construction and every test
passes a fake one. No network, no browser, no containers.

**Consequences.** The fake has to stay faithful to the api contract, which is a
real risk and is mitigated by the error mapping table being written out by hand
in its own test rather than read from the implementation.

In exchange the whole suite runs in about two seconds, and a test can assert
that no request was sent at all, which is the only way to prove the fresh-cache
case in simulation F12.

<br>

## ADR-F013: A three tier client cache backed by session storage

**Status:** accepted, phase 3

**Context.** A repeat class-list view should not cost a database read.

**Decision.** Fresh for five seconds, served from memory with no request at all.
Stale to thirty seconds, rendered at once and revalidated in the background with
`If-None-Match`. Cold after that. Both boundaries are exclusive at the low end,
so exactly five seconds is stale and exactly thirty is cold.

Only the class list and a single class are held. Booking status, payment, the
roster, auth, and admin are never cached, because each of them either decides
something or is specific to one parent.

**Consequences.** Invalidation has to be wired into every mutation and into sign
out. This is safe only because of ADR-F003: everything cacheable is advisory. A
count that came out of this cache still cannot refuse a booking, and every screen
still handles a rejection that contradicts it.

<br>

## ADR-F014: An unknown outcome reuses the idempotency key

**Status:** planned, phase 5

**Context.** A decline and an `internal_error` look similar on screen and are
opposite underneath. A decline means no money moved. An `internal_error` means
the client does not know.

**Decision.** A retry after `internal_error` resends the original idempotency
key. A retry after a decline gets a fresh one.

**Consequences.** The client has to tell two failures apart that look alike. In
exchange a parent is never charged twice for the same attempt.

<br>

## ADR-F015: Visual polish is the last phase

**Status:** planned, phase 9

**Context.** The brief states plainly that a polished interface is not required
and that the weight is on the data model, the backend logic, and the
explanation.

**Decision.** Layout and responsiveness come last, and only if time remains.

**Consequences.** The screens read plainly until then. The one exception is the
pair that appears in the video, payment failure and duplicate booking, which
have to be legible on a recording.

<br>

## ADR-F016: A mutation ages a cache entry rather than deleting it

**Status:** accepted, phase 3

**Context.** A successful booking or payment makes every seat count on screen
untrue. The obvious answer is to delete the cached class list. Deleting it also
throws away the ETag, and the tag is the cheapest thing in the cache.

**Decision.** A successful mutation marks every entry cold, keeping its body and
its tag. A cold entry is never rendered before the api has answered, so nothing
stale can reach a parent, and the revalidation that follows still carries
`If-None-Match`.

A hard sign out is the opposite case and deletes everything, in memory and in
session storage, because there the whole point is that nothing is left behind.

**Alternatives rejected.** Deleting on a mutation, which costs a full response
body on the next view even when the mutation moved a different class.
Invalidating one chosen key, which needs the mutation to know which class it
touched and drops the same set anyway, since this cache holds nothing but class
data.

**Consequences.** Two words that sound alike mean different things in this
codebase, so both are stated where they are used: `invalidate` ages an entry and
`clear` removes it. In exchange a mutation that did not move a given list costs
one 304 rather than one full body.


<br>

## ADR-F017: One idempotency key covers one attempt, both calls included

**Status:** accepted, phase 4

**Context.** A booking is two calls: hold a place, then pay for it. Either can
be retried by a parent on a slow connection or by a client whose response was
lost. Charging twice is the failure that matters most, because it takes money
and nobody notices until a statement arrives.

**Decision.** One key is minted when a parent submits a booking and it is sent
on both calls. The key belongs to the attempt, so it lives in the booking store
rather than in either call.

A new attempt mints a new key. A decline is a finished attempt, so paying again
is a new one, and reusing the key there would replay the decline for as long as
the parent kept trying. See ADR-F014 for the case that goes the other way: an
unknown outcome is the same attempt still unresolved, and it keeps its key.

**Consequences.** The key has to be unguessable rather than merely unique. The
backend honours a matching key as a promise that two calls are the same call, so
a predictable key would let one parent's retry collide with another's attempt.
`crypto.randomUUID` is the source, with a fallback over the same random source
rather than a timestamp or a counter, both of which are guessable.

Phase 6 moves the lifecycle into the api client, so a header is attached without
every caller remembering. The minting stays in one file either way.

<br>

## ADR-F018: A refusal that names a seat count invalidates the cached list

**Status:** accepted, phase 4

**Context.** ADR-F016 says a successful mutation ages every cache entry and a
failed one ages nothing, because a failure usually says nothing about seats.
Two failures are not like that. `class_full` is the api saying the class has no
room, and `seat_lost` is it saying the last seat went to somebody else. Both
arrive on the failure path and both are news about the count on screen.

Without acting on them, a parent refused for a full class goes back to a list
that still shows them a seat, because the entry is fresh and nothing asked
again. That is the exact contradiction this client exists to avoid.

**Decision.** The booking store ages every entry when a refusal is one of those
two kinds, and leaves the cache alone for every other failure. The set is a
named constant so a reader can see which two and check why.

**Consequences.** The rule lives in the store rather than in the mutation layer,
because the mutation layer is deliberately ignorant of what a failure means and
knowing would make it depend on the error vocabulary. The cost is one line in
one store, and the alternative was a screen remembering to refetch, which is the
kind of thing that gets forgotten on the fourth screen.

<br>

## ADR-F019: A count on screen never disables the way forward

**Status:** accepted, phase 4

**Context.** A class showing no seats could hide its booking button, grey it
out, or leave it alone. All three look reasonable and only one of them is
consistent with the rest of this client.

**Decision.** A full class shows no link and says plainly that no seats are
showing. A class with any seats left is bookable, including the last one.
Nothing on a screen refuses a request on the strength of a cached number, and
the api is what says no.

**Consequences.** A count that is a moment out of date costs a parent one
refusal with a sentence they can act on, which is the trade this whole design
already makes. A disabled button would be the worse failure: it looks like the
system deciding, and it is really a cached number deciding.

The boundary is asserted in both directions. One seat left is bookable, because
off by one there would hide the last seat from everybody, and that last seat is
what the whole exercise is about.

<br>

## ADR-F020: A store the parent's data lives in resets when the session ends

**Status:** accepted, phase 4

**Context.** A hard sign out drops the auth store, empties the cache, and clears
session storage. The booking store was not on that list, and it holds a booking
identifier and the child it is for. On a shared machine the next parent to sign
in on the same tab would find both still there.

**Decision.** The wired singletons watch the auth store and reset themselves
when the session becomes null. A store a test builds does not, so a test keeps
whatever it put in.

**Consequences.** The reset cannot be called from `session/sign_out.ts`, because
that would close a cycle: sign out would import the store, the store imports the
mutator, the mutator imports the api client, and the api client reports a hard
sign out. Watching the auth store is the same effect with the arrows pointing
one way, and it is why `session/cache.ts` was kept import-free in phase 3.

<br>

## ADR-F021: The idempotency rule lives in the store, not on the screen

**Status:** accepted, phase 5

**Context.** ADR-F014 and ADR-F017 set the rule: a decline is a finished attempt
and earns a fresh key, an unknown outcome keeps its key. Phase 4 left the store
with a `startNewAttempt` method and left it to whichever screen offered the
retry to call it at the right time.

Building the payment screen showed what that costs. A screen that forgets the
call after a decline replays the decline. A screen that makes the call after an
`internal_error` risks charging twice. Both are one missing line, in a
component, where nothing about the surrounding code says the line is load
bearing.

**Decision.** The store decides. A named set, `kindsThatEndTheAttempt`, holds
the failures that finish an attempt, and the failure path mints a fresh key when
the refusal is one of them. `startNewAttempt` is gone. What remains is
`dismissFailure`, which clears the message on screen and deliberately touches no
key.

**Consequences.** A screen retries by calling `pay` again, and the right key
goes out whichever failure it is recovering from. Two edge tests pin the two
directions, and simulations F3 and F17 assert them end to end, one on a fresh
key and one on the same key.

The set has exactly two members and both are there for a reason a reader can
check. `PaymentDeclined` is an answer, so the attempt is over. `InvalidRequest`
is the request being rejected before it became an attempt at all. `SeatLost` and
`ClassFull` are in neither camp, because there is no retry to make.

<br>

## ADR-F022: The countdown is arithmetic in its own file, not state in a component

**Status:** accepted, phase 5

**Context.** A countdown looks like the sort of thing a component owns: a timer,
a number, a label. Written that way it holds the remainder in a variable and
subtracts a second per tick, which fails quietly in three ways. A suspended tab
comes back showing five minutes that have already gone. A deadline in the past
renders as a negative. A deadline the browser cannot parse renders as `NaN`.

**Decision.** `lib/booking/countdown.ts` is a pure function of a deadline and an
instant. The component holds a tick counter only to force a re-render, and every
render works the remainder out from the deadline and the clock. A deadline that
is absent, unparseable, or past all read as expired with `00:00`.

**Consequences.** All three failures above are single values, so all three are
asserted in a unit test rather than through a rendered component. A tab that was
hidden for five minutes shows the right number on its first frame back, because
nothing is accumulated.

The seconds round up, so a hold with 400 milliseconds left reads as one second
rather than zero. Reading `00:00` beside a control that still works is the one
way this display can contradict itself.

<br>

## ADR-F023: Reaching zero closes the control and then asks the api

**Status:** accepted, phase 5

**Context.** The hold countdown reaching zero is the client noticing something,
not the client deciding it. The two obvious implementations are both wrong. Wait
for the api before disabling, and a live payment button sits on a hold that has
gone. Declare the booking expired on the strength of the countdown, and this
client has decided something only the backend decides.

**Decision.** Both, in that order. The control closes at once, with no round
trip, so nothing can be submitted into a lapsed hold. Then the booking is read
back, once, and whatever the api says is what the screen shows.

**Consequences.** The read is fired once rather than on every tick, because what
it does is ask a question, and asking it every second is a poll nobody asked
for. A parent opening the link an hour later gets the same behaviour as one who
watched it run out: the control is dead on arrival and the api is asked.

The clock is never trusted for a claim. If the api says the booking is still
pending, the screen says so, and the countdown having reached zero is treated as
what it is, a hint that the hold is probably gone.

<br>

## ADR-F024: A booking status is never served from the cache

**Status:** accepted, phase 5

**Context.** The client cache exists so a repeat view of the class list costs
nothing. Adding a booking to it would be the same three lines and would look
like a consistency win.

**Decision.** The booking read goes straight through the api client. Only the
class list and a single class are ever cached, which is what ADR-F013 already
said, and this is the first screen that could have quietly widened it.

**Consequences.** Every visit to a payment or status screen costs one request.
That is the correct cost: a booking status is what decides. Whether the hold is
still standing, whether the seat was won, whether a refund is on the way, all of
them change without this client doing anything, and a cached answer is a screen
telling a parent something that stopped being true two minutes ago.

The seam is a separate `reader` option on the store rather than a flag on the
existing mutator, so nothing can accidentally route a status read through the
cache-aware path and invalidate the class list by reading.

<br>

## ADR-F025: A lost seat gets no retry control at all

**Status:** accepted, phase 5

**Context.** A declined payment and a lost seat are the same refusal of the same
call, and they are opposite in the ledger. One took no money. The other took
money and is giving it back. Rendered similarly, a parent who lost a seat sees a
retry button and presses it.

**Decision.** `SeatLost` renders a terminal message, and the payment control is
closed rather than merely unstyled. The status screen says the refund is
automatic and that there is nothing to do. `PaymentDeclined` says plainly that
no money was taken and offers the retry.

**Consequences.** The difference is stated in words rather than in colour, so it
survives a screenshot, a screen reader, and a parent who is not looking
carefully. A test renders both and asserts they read differently, which is the
one thing a shared component would have made easy to lose.

This is the parent's side of the case the whole exercise is about, and this
client's only job in it is to not make it worse.
