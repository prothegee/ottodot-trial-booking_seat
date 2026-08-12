# Frontend Decision Records

Numbered records, each with the context that forced a choice, the choice itself,
and what living with it costs. Cross-stack decisions are recorded in
`../backend/ADR.md` and not repeated here.

Every record here is now implemented. The second status is kept in the table
because that is what a record read as while the phase that implements it was
still ahead.

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

## ADR-F002: nginx in its own container

**Status:** accepted, phase 1. Server changed on 2026-08-11.

**Context.** A static bundle needs something to serve it, and client routes such
as `/book/[classId]` only exist at runtime.

**Decision.** nginx, with `try_files $uri $uri/ /index.html` handing the same
document to every unknown path. The whole server block is one file,
`containers/nginx/nginx.conf`, copied over the image's default.

**Alternatives rejected.** A Node process, which would mean a runtime to patch
for no benefit. A dev server in production, which is not what one is for.

**Consequences.** One more container, which the monitoring already covers. The
response still carries a `Server: nginx` header, without a version. Removing the
name needs a module the official image does not carry, and is not worth a custom
build here.

**History.** This container first ran a different server, picked without anyone
asking for it. It was replaced with nginx on the same terms: same port, same
fallback, same headers.

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
internal detail onto a shared screen, and an unknown code falls back to a generic
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

**Status:** accepted, phase 5

**Context.** A decline and an `internal_error` look similar on screen and are
opposite underneath. A decline means no money moved. An `internal_error` means
the client does not know.

**Decision.** A retry after `internal_error` resends the original idempotency
key. A retry after a decline gets a fresh one.

**Consequences.** The client has to tell two failures apart that look alike. In
exchange a parent is never charged twice for the same attempt.

<br>

## ADR-F015: Visual polish is the last phase

**Status:** accepted, phase 9

**Context.** The brief states plainly that a polished interface is not required
and that the weight is on the data model, the backend logic, and the
explanation.

**Decision.** Layout and responsiveness come last, and only if time remains.

**Consequences.** The screens read plainly until then. The one exception is the
pair that carries a demonstration, payment failure and duplicate booking, which
have to be legible on a shared screen.

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

<br>

## ADR-F026: The client measures and sends, and refuses nothing

**Status:** accepted, phase 6

**Context.** The payment form carries three bot prevention signals: a honeypot
field, how long the form was open, and a challenge token. Checking any of them
here is tempting, because a refusal that never leaves the browser is instant and
costs the backend nothing.

**Decision.** Nothing is checked in the client. `botSignals` measures, the form
sends what it measured, and the api decides. A honeypot with something in it is
sent with something in it, and the submission goes out.

**Consequences.** Two failure modes are avoided at once. A client that refused
its own submission would tell a script exactly which value to change, because
the refusal arrives before any request and can be seen in a debugger. And a
fill-time check running on a slow device would refuse a real parent for being
slow, with no way for anybody to see that it had happened.

The cost is one wasted round trip for a submission that was never going to be
accepted, which is a request the backend was going to have to refuse anyway.

<br>

## ADR-F027: The honeypot is a real field moved off screen

**Status:** accepted, phase 6

**Context.** A hidden field has to be invisible to a person, invisible to a
screen reader, and out of the keyboard order, while still looking fillable to
whatever is filling it in. `type="hidden"` satisfies the first three and fails
the fourth: it is the first thing a script skips.

**Decision.** A text input with an ordinary name, `website`, positioned off
screen with `tabindex="-1"`, `autocomplete="off"`, and inside an `aria-hidden`
wrapper. `display: none` was rejected for the same reason as `type="hidden"`.

**Consequences.** It is a real, laid out field that nobody can see, reach with
the keyboard, or hear read out, and `autocomplete="off"` is what stops a browser
or a password manager filling it in on the parent's behalf, which would refuse a
real person. Simulation F7 asserts all four properties, because each one is a
line somebody could delete without anything else breaking.

The field name is deliberately plausible. A field named `honeypot` is a field a
script skips, which would make the whole thing decorative.

<br>

## ADR-F028: The fill timer runs from mount, and a backwards clock reports nothing

**Status:** accepted, phase 6

**Context.** The elapsed time has to be a real measurement. A constant would
pass the backend's check while proving nothing about who filled the form in,
which is the same as not sending it.

There is nothing to type on this form: a parent reads an amount and presses a
button. So there is no first keystroke to measure from.

**Decision.** The measurement runs from the component mounting to the submit,
using `performance.now` where it exists and `Date.now` otherwise. An elapsed
time of zero or below is sent as zero, which the api reads as "not measured"
rather than as evidence.

**Consequences.** `performance.now` is monotonic, so a system clock correction
mid-form cannot produce a negative. The fallback can, which is why the
arithmetic guards it anyway rather than trusting the clock it was handed.

Simulation F7 mounts the screen twice, holds each open for a different stretch,
and asserts the two numbers differ. A constant would pass every other test in
this repository.

<br>

## ADR-F029: The key lifecycle lives in the api layer, not in the store

**Status:** accepted, phase 6

**Context.** ADR-F021 put the rule for when an idempotency key is spent inside
the booking store, so no screen could forget it. That was right, and it left the
rule reachable only through a store: reading it meant reading `submit`, and
testing it meant driving a booking.

**Decision.** `lib/api/attempt.ts` owns the key and the rule. The store calls
`restart` when an attempt begins, `current` when it continues, and `settle` with
the failure kind, and holds no key of its own.

**Consequences.** ADR-F021 still holds: the store applies the rule and no screen
can forget it. What changed is where the rule can be read from, which is a file
with ten cases against it, including the one that matters most, that a failure
kind this build has never heard of keeps the key rather than minting one.

That default is the safe direction. A backend that grows a code this client does
not know must not be able to turn a retry into a second charge.

<br>

## ADR-F030: The challenge widget is a mock, and it takes a moment on purpose

**Status:** accepted, phase 6

**Context.** A real challenge widget mounts, takes a moment, and hands back one
opaque token. A mock that solved instantly would hide the one state worth
designing for: the parent submitting before the challenge has answered.

**Decision.** `CaptchaWidget.svelte` waits a short delay, then reports the token
the backend's mock verifier accepts. The token is passed through untouched: the
client never reads it, shortens it, or decides anything from it.

**Consequences.** The unanswered state is reachable and is tested. A submission
that goes out before the widget solves carries an empty token, which the api
accepts today and would refuse with the challenge required, and neither case is
a surprise.

Swapping in Turnstile or hCaptcha replaces this one file, because nothing
outside it knows what a token looks like. The shared token string is what makes
both halves testable end to end without an account anywhere, and it is the one
thing a real provider replaces on both sides at once.

<br>

## ADR-F031: Monitoring never breaks a booking, which decides everything about the emitter

**Status:** accepted, phase 7

**Context.** The client cannot be scraped, so it posts what it saw to the api.
That post is a network call on the same connection as a booking, made from the
same tab, competing for the same attention. Every design question about it has
the same answer.

**Decision.** Every failure is swallowed, including the reason. Nothing a screen
does awaits a post. A failed batch is thrown away rather than retried. The queue
is capped at the backend's own batch cap. The timer only runs while something is
queued.

**Consequences.** A telemetry post that fails leaves no trace anywhere the parent
can see, and not even a console line. That is uncomfortable to write and it is
correct: a monitoring failure a parent can see has already cost more than it was
ever going to save.

Not retrying is the part worth defending. A retry would turn an unreachable api
into a growing queue and then into repeated requests at exactly the moment the
api is least able to answer them. Losing a page load timing is not worth that.

The cap is what stops a tab left open overnight with the api down from being a
memory leak. Reaching it forces a post rather than dropping the oldest event,
because a batch that goes out and fails costs nothing and a batch that is
silently trimmed is a number nobody can explain.

<br>

## ADR-F032: The event vocabulary is closed, and it is closed on both sides

**Status:** accepted, phase 7

**Context.** Every field the client posts becomes a Prometheus label. A route
posted as a path would be one time series per booking, which is a leak of who
booked what into a system with no access control in front of it.

**Decision.** `TelemetryRoute` is a union of route patterns, not a string.
`FunnelStep` and `CacheResult` are unions too. The backend checks the same lists
again on the way in and drops anything it does not recognise.

**Consequences.** `/booking/[bookingId]` is one series. The type is what makes
the wrong value impossible to pass, and the backend's check is what makes it
impossible to send from a page somebody modified.

The duplication is deliberate rather than an oversight. This is the one endpoint
where the api takes a label value from outside itself, and it is worth being
unable to get wrong from either direction.

Adding a screen means adding its pattern here and in the backend's list. That is
one more step than it would otherwise be, and it is the step that keeps the
series count bounded.

<br>

## ADR-F033: A page load is measured to usable, not to mounted

**Status:** accepted, phase 7

**Context.** Nothing on the server can see how long a screen took. A request
duration ends when the body is written, and what a parent experiences is that
plus the network, the parse, and the render.

A screen mounts long before it is usable. A class list that mounted instantly
and then waited two seconds for its data was not usable for two seconds.

**Decision.** `measurePageLoad` starts a monotonic timer and returns a function
the screen calls when it has something a parent can act on. Calling it twice
reports once.

**Consequences.** The number describes the wait rather than the render, which is
the only version of it anybody would act on.

Reporting once is what stops a screen whose data arrives in two parts from
recording the second part as a second page load. That would fill the histogram
with numbers nobody meant, and a histogram is exactly the shape that cannot be
cleaned up afterwards.

<br>

## ADR-F034: The roster is never cached, and its link is a courtesy

**Status:** accepted, phase 7

**Context.** Every cacheable read in this client is advisory: a stale seat count
costs a parent one wasted click. The roster is neither. It is the only shape in
the whole client that carries a child's name next to a seat.

**Decision.** The roster store reads through the api client, never through the
cached reader, and drops what it holds when the screen is destroyed. The link is
shown to an admin role and hidden from a parent.

**Consequences.** A stored roster would be the one place in the browser where
another family's name outlives the screen that showed it, and `sessionStorage`
survives a reload. Nothing about a roster is worth that.

The hidden link is a courtesy and is written down here as one. Anybody can type
the route, and what actually refuses them is the api answering `forbidden_role`.
A client that treated a hidden link as protection would be one developer tools
window away from handing over every other family's name, which is why simulation
F11 drives the refusal rather than only checking that the link is absent.

The refused case has a screen of its own rather than sharing the generic failure
message, because a parent who reached it by typing deserves a plain sentence
rather than a sentence about a roster.

<br>

## ADR-F035: The status route is the only thing that polls, and it stops

**Status:** accepted, phase 7

**Context.** Readiness is a question about right now. A cached answer to "is the
database up" is an answer about a moment that has passed, and a client that
polled it everywhere would turn a readiness probe into traffic.

**Decision.** `/status` polls `/readyz` and `/version` every fifteen seconds while
it is open, and nowhere else in this client polls anything. The store's `close`
stops the timer, and the screen calls it when it is destroyed.

**Consequences.** A timer that outlives its screen is a request every fifteen
seconds for a page nobody is looking at, and in a test it is a warning after the
case has finished. Simulation F15 asserts that the requests stop after the screen
unmounts, which is the only way to know the cleanup is real.

A 503 from readiness is treated as a successful read of a real answer. The api
answers unready with the report naming which dependency is down, and treating
the status as a failure would throw away the only information the screen exists
to show. That is why `ApiError` carries the parsed body.

<br>

## ADR-F036: Degraded is amber, and no answer is a fourth state

**Status:** accepted, phase 7

**Context.** The api reports three readiness states. Two of them are obvious. The
third, degraded, means every required dependency answered and an optional one did
not, which for this service means the replica is down.

**Decision.** Green, amber, red for the three, and grey for a backend that
answered nothing at all.

**Consequences.** Amber rather than red for degraded, because no seat is ever
decided from the replica. A degraded service is correct and a class list may be a
moment stale, and colouring that red would send somebody looking for a problem
that is not costing anybody a booking.

Grey is a fourth state and not a synonym for red. A service telling the truth
about being broken and a service that is not there are different facts, and a
dot that showed the same thing for both would be lying about one of them.

The label is the answer and the dot is the decoration. Three states carried by a
colour alone would say nothing at all to somebody who cannot see it, which is why
the dot is hidden from assistive technology and the word is not.

<br>

## ADR-F037: A build that is not named takes its name from the repository

**Status:** accepted, phase 9

**Context.** The footer exists so a reviewer can tell which build is on screen. It
read `version dev, commit unknown` on every run of the dev server, because both
fallbacks were literal strings and nothing sets those two values locally. A
footer that says the same thing for every build answers no question at all.

**Decision.** When `BUILD_VERSION` and `BUILD_COMMIT` are unset, the version is
the one `package.json` already declares and the commit is the short hash git
already recorded. `src/lib/config/build_identity.ts` reads both, `vite.config.ts`
passes them to the same two constants, and an explicitly set value still takes
precedence.

**Consequences.** The fallback is decided in one place. `scripts/build.sh` used
to resolve the commit itself and default the version to `dev`, which meant the
script silently overrode anything better. It now sets neither, so a build made
through the script and a build made with `npm run build` name themselves
identically.

A container build has no git history in its context, so the commit there is still
`unknown` unless a pipeline hands it in, which both workflows do. The version
survives, because `package.json` is copied into the image. The build arguments
default to empty rather than to `dev`, and an empty value is read as unset.

The version is only as honest as the manifest. `0.1.0` stays `0.1.0` until
somebody raises it, which is a smaller lie than `dev` and a familiar one.

<br>

## ADR-F038: The api host follows the page host between loopback names

**Status:** accepted, phase 9

**Context.** A cookie belongs to a host, and a port is not part of that host. The
api is configured as `http://127.0.0.1:9000` and the client is served on 9001, so
a reviewer who opens `http://localhost:9001` is making cross-site calls: sign in
answers 204, the browser discards both session cookies as `SameSite=Lax` in a
cross-site context, and the very next call is a 401. Nothing on screen explains
it, and the api allows both origins on purpose so that either name may be typed.

**Decision.** When the page is served from a loopback name and the configured api
is on the other loopback name, the api host follows the page.
`src/lib/config/api_base_url.ts` decides that once, and `session/client.ts` is the
only caller.

**Consequences.** Same host, different port, which is same-site, so the cookies
are kept and sign in holds. The rule is deliberately narrow: it fires only when
both hosts are `localhost` or `127.0.0.1`, so a deployed client pointing at a
real api is never rewritten, and a page on a real host never has its api moved.

This does not make the client decide its own backend. The configured address
still names the port, the scheme, and any path. Only the host is aligned, and
only between two names for the same interface.

The alternative was to remove `http://localhost:9001` from the api's allowed
origins so the failure arrived as a blocked request instead of a silent 401. That
trades a confusing symptom for a blunt one, and it still costs a reviewer the
sign in.

<br>

## ADR-F039: The header carries the title and nothing else

**Status:** accepted, phase 9

**Context.** The header offered a `Status` link on every screen. That page reports
build identity and backend readiness, which is an operations question, and it sat
one click from a parent halfway through booking a seat.

**Decision.** The header carries the title, which returns to the class list.
`/status` stays exactly as it is and is reached by typing its address.

**Consequences.** One less thing on a screen whose job is a booking. The route
loses its only discoverable entry point, which is why `README.md` and `how-to.md`
both name the address. Simulation F15 is unaffected, because it mounts the screen
directly rather than navigating to it.

<br>

## ADR-F040: The sign out is a header control that tells the api first

**Status:** accepted, phase 9

**Context.** `hardSignOut` existed from phase 3 and was reached only by the api
reporting a session that could not be recovered. Nothing a parent could press
called it, and nothing anywhere called `api.logout()`. A parent on a shared
machine could close the tab and leave a live refresh token behind in a cookie.

**Decision.** The header carries a `Sign out` control, shown only while the auth
store holds a session. It calls the api first, then runs the same local cleanup a
forced sign out runs. The two halves are separate files, because `client.ts`
imports the local one and the half that reaches for the client cannot live beside
it: `sign_out.ts` clears this device, `sign_out_request.ts` is the parent asking.

A refused or unreachable api does not stop the local half. The reason recorded is
`requested`, which is the one reason the sign-in screen shows no notice for.

**Consequences.** The control is a button rather than a link, because ending a
session is a write and a link is followed by a prefetch, by a crawler, and by a
browser restoring a tab. It is disabled while the call is in flight, because two
logouts race the same refresh token and the api answers the second one as a
reuse, which would reach the parent as the notice meant for a stolen session.

This amends ADR-F039. The header carries the title and the session control, and
still nothing else.

<br>

## ADR-F041: A card's actions are one block the layout can push down

**Status:** accepted, phase 9

**Context.** The class list is a grid, and a grid stretches every card in a row to
the tallest one. What it does not decide is where the spare height goes. A class
whose date wrapped onto two lines pushed its button down and a class whose date
fit did not, so a row of `Book a place` buttons came out as a staircase.

**Decision.** Everything a reader can act on, the button or the note that says
there is no button, and the roster link when it is shown, sits in one block at the
end of the card. That block takes `margin-top: auto`, so all of the spare height
lands above it.

**Consequences.** Every button in a row is on the same line however long the words
above it ran, and a full card ends at the same height as a bookable one. The block
is why the roster link stays attached to the button rather than being pushed down
on its own. A test pins the structure, since the alignment itself is CSS and
nothing in this suite renders a layout.

<br>

## ADR-F042: The password can be shown, from a control that is a button

**Status:** accepted, phase 10

**Context.** The sign in field hid what was typed and offered no way to look. A
wrong password is then typed twice, and the api answers both attempts with the
same deliberately vague refusal, so nobody on that screen can tell a typo from a
wrong account. The four seeded accounts share one written down password, which
makes a reviewer mistyping it the most likely first experience of this client.

**Decision.** `PasswordField.svelte` owns the field and a control on its right
that shows and hides the characters. Three things are decided with it:

| decision | why |
| :- | :- |
| it opens hidden, every time | revealing once at a desk is not agreeing to reveal on the next screen, so the state is not remembered |
| the control is a `button` with `type="button"` | inside a form a button submits by default, so an icon bolted on without this would send a half filled sign in. A button is also reachable with a keyboard, which a bare icon is not |
| the input's type changes, and the element does not | Svelte refuses `bind:value` on an input whose type changes, so the value is written back from the input event by hand. Two elements swapped by a block would lose the caret and the focus on every press |

The icon is drawn inline and hidden from assistive technology, and the button
carries `Show password` or `Hide password` as its label and `aria-pressed` as its
state. The icon has no words in it, so the label is the whole answer for somebody
who cannot see it.

**Consequences.** A password on screen is readable by anyone else in the room,
which is the point of the control and the reason it is never on by default. The
component is separate from the sign in page, so a second password field anywhere
later behaves the same way rather than being rebuilt. Its styles are stated in
the component rather than inherited, because a page's styles are scoped to the
page and do not reach into a component, which is one repetition of the field
shape accepted in exchange for the component standing on its own.

<br>

## ADR-F043: The bookings a parent made are a screen, reached from the header

**Status:** accepted, phase 10

**Context.** Every link to a booking sat on the payment screen. The journey ran
class list, book, pay, booking, and each step knew the identifier because the
previous one handed it over. Nothing carried it across a closed tab. A parent who
paid and then closed the window could not get back to the booking to see whether
the payment completed, and the client had nothing to list because the api had no
route that answered "which bookings are mine".

**Decision.** `/bookings` lists the signed-in parent's own bookings, read from
`GET /api/v1/bookings` on every visit, and the header carries a link to it while
somebody is signed in.

The rows are the same `BookingStatus` card the booking screen shows. The wording
for a status is written once, so a list with its own shorter vocabulary cannot
drift into disagreeing with the screen it links to.

`stores/bookings.ts` is a separate store from `stores/booking.ts`. That one owns
the booking in flight: the attempt key, the hold counting down, the retry rules.
This one owns a list somebody is looking at, which has no attempt, no key, and
nothing to retry.

**Consequences.** This amends ADR-F039 and ADR-F040 again. The header carries the
title, this link, and the session control, and still nothing operational: there
is no `/status` link and there is not going to be one.

The list is never cached, for the reason `stores/booking.ts` gives about reading
one: a booking status is what decides, and a stored copy of it is how a parent is
shown a payment as still pending two minutes after it cleared.

A failed read leaves whatever was already on screen. The parent was looking at
something, and emptying it on a dropped connection would read as "your bookings
are gone", which is the exact fear this screen exists to answer.

The empty state waits for a read to come back. "You have not booked a trial class
yet" and "the list is still loading" hold the same zero rows, and showing the
first one on the way in tells a parent something untrue for as long as the call
takes.

A sign out empties the store, like the booking in flight. This is every booking a
family has, so it is the last thing that should outlive their session.
